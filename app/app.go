package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	contract "github.com/slidebolt/sb-contract"
	domain "github.com/slidebolt/sb-domain"
	logging "github.com/slidebolt/sb-logging-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

const PluginID = "plugin-automation"

// AggregateLightState computes the aggregate state of a group from its
// member light states: power = any-on, brightness/rgb/temperature =
// average of on-members.
func AggregateLightState(members []domain.Light) domain.Light {
	var result domain.Light
	if len(members) == 0 {
		return result
	}

	var onCount int
	var totalBrightness int
	var rgbCount int
	var totalR, totalG, totalB int
	var tempCount int
	var totalTemp int

	for _, m := range members {
		if !m.Power {
			continue
		}
		result.Power = true
		onCount++
		totalBrightness += m.Brightness

		if m.RGB != nil && len(m.RGB) == 3 {
			rgbCount++
			totalR += m.RGB[0]
			totalG += m.RGB[1]
			totalB += m.RGB[2]
		}
		if m.Temperature > 0 {
			tempCount++
			totalTemp += m.Temperature
		}
	}

	if onCount > 0 {
		result.Brightness = totalBrightness / onCount
	}
	if rgbCount > 0 {
		result.RGB = []int{totalR / rgbCount, totalG / rgbCount, totalB / rgbCount}
	}
	if tempCount > 0 {
		result.Temperature = totalTemp / tempCount
	}

	return result
}

type AutomationRule struct {
	RuleID    string `json:"rule_id"`
	Enabled   bool   `json:"enabled"`
	Condition string `json:"condition"`
	Action    string `json:"action"`
}

type AutomationRuleEnable struct {
	RuleID string `json:"rule_id"`
}

func (AutomationRuleEnable) ActionName() string { return "automation_rule_enable" }

type AutomationRuleDisable struct {
	RuleID string `json:"rule_id"`
}

func (AutomationRuleDisable) ActionName() string { return "automation_rule_disable" }

type ScriptRun struct {
	Name string `json:"name"`
}

func (ScriptRun) ActionName() string { return "script_run" }

type ScriptStopAll struct{}

func (ScriptStopAll) ActionName() string { return "script_stop_all" }

type GroupState struct {
	MemberCount int      `json:"member_count"`
	Status      string   `json:"status,omitempty"`
	Control     []string `json:"control,omitempty"`
}

type App struct {
	msg      messenger.Messenger
	store    storage.Storage
	logger   logging.Store
	cmds     *messenger.Commands
	subs     []messenger.Subscription
	ticker   *time.Ticker
	stopChan chan bool

	// controlSubs tracks subscriptions for control entity state changes.
	// Keyed by control entity key so re-discovery can skip existing ones.
	controlSubs map[string]messenger.Subscription

	// groupWatchers tracks storage.Watch instances per group ID.
	// Each watcher monitors the group's member query and aggregates
	// member states into the group entity on every change.
	groupMu       sync.RWMutex
	groupWatchers map[string]*storage.Watcher
}

var logSequence uint64

type scriptAPIResponse struct {
	OK    bool   `json:"ok"`
	Hash  string `json:"hash,omitempty"`
	Error string `json:"error,omitempty"`
}

type positionedEntity struct {
	entity   domain.Entity
	position int
}

func init() {
	domain.Register("group", GroupState{})
	domain.RegisterCommand("script_run", ScriptRun{})
	domain.RegisterCommand("script_stop_all", ScriptStopAll{})
}

func New() *App {
	return NewWithLogger(nil)
}

func NewWithLogger(logger logging.Store) *App {
	return &App{
		logger:        logger,
		controlSubs:   make(map[string]messenger.Subscription),
		groupWatchers: make(map[string]*storage.Watcher),
	}
}

func (a *App) Hello() contract.HelloResponse {
	return contract.HelloResponse{
		ID:              PluginID,
		Kind:            contract.KindPlugin,
		ContractVersion: contract.ContractVersion,
		DependsOn:       []string{"messenger", "storage"},
	}
}

func (a *App) OnStart(deps map[string]json.RawMessage) (json.RawMessage, error) {
	msg, err := messenger.Connect(deps)
	if err != nil {
		return nil, fmt.Errorf("connect messenger: %w", err)
	}
	a.msg = msg

	store, err := storage.Connect(deps)
	if err != nil {
		return nil, fmt.Errorf("connect storage: %w", err)
	}
	a.store = store
	if a.logger == nil {
		if logger, err := logging.Connect(deps); err == nil {
			a.logger = logger
		} else {
			log.Printf("plugin-automation: logging connect failed: %v", err)
		}
	}

	domain.Register("group", GroupState{})

	a.cmds = messenger.NewCommands(msg, domain.LookupCommand)
	sub, err := a.cmds.ReceiveMessage(PluginID+".>", a.handleCommandMessage)
	if err != nil {
		return nil, fmt.Errorf("subscribe commands: %w", err)
	}
	a.subs = append(a.subs, sub)

	if err := a.initializeGroupDevice(); err != nil {
		return nil, fmt.Errorf("initialize group device: %w", err)
	}

	a.stopChan = make(chan bool)
	a.ticker = time.NewTicker(30 * time.Second)
	go a.discoveryLoop()

	log.Println("plugin-automation: started")
	return nil, nil
}

func (a *App) OnShutdown() error {
	if a.ticker != nil {
		a.ticker.Stop()
	}
	if a.stopChan != nil {
		close(a.stopChan)
	}
	for _, sub := range a.subs {
		sub.Unsubscribe()
	}
	for _, sub := range a.controlSubs {
		sub.Unsubscribe()
	}
	a.groupMu.Lock()
	watchers := a.groupWatchers
	a.groupWatchers = make(map[string]*storage.Watcher)
	a.groupMu.Unlock()
	for _, w := range watchers {
		w.Stop()
	}
	if a.store != nil {
		a.store.Close()
	}
	if a.msg != nil {
		a.msg.Close()
	}
	return nil
}

func (a *App) initializeGroupDevice() error { return a.discoverGroups() }

// Discover runs a group discovery cycle. Exported for testing.
func (a *App) Discover() error { return a.discoverGroups() }

func (a *App) discoveryLoop() {
	for {
		select {
		case <-a.ticker.C:
			if err := a.discoverGroups(); err != nil {
				log.Printf("plugin-automation: group discovery error: %v", err)
			}
		case <-a.stopChan:
			return
		}
	}
}

func (a *App) discoverGroups() error {
	// Ensure the synthetic "group" device record exists. All automation
	// group entities are children of this single device — it's the logical
	// bucket that holds every user-defined group (basement, kitchen, …).
	// Written idempotently on every discovery pass so a fresh install or
	// a wiped data dir always gets it.
	groupDevice := domain.Device{ID: "group", Plugin: PluginID, Name: "Automation Groups"}
	if err := a.store.Save(groupDevice); err != nil {
		return fmt.Errorf("save group device: %w", err)
	}

	allEntities, err := a.store.Query(storage.Query{
		Pattern: ">",
		Where: []storage.Filter{
			{Field: "labels.PluginAutomation", Op: storage.Exists},
		},
	})
	if err != nil {
		return fmt.Errorf("query all entities: %w", err)
	}

	groupConfigs := make(map[string][]positionedEntity)
	groupControls := make(map[string][]string) // groupName -> control entity keys
	for _, entry := range allEntities {
		var entity domain.Entity
		if err := json.Unmarshal(entry.Data, &entity); err != nil {
			continue
		}
		if entity.Plugin == PluginID && entity.DeviceID == "group" {
			continue
		}
		if labels, ok := entity.Labels["PluginAutomation"]; ok {
			for _, groupName := range labels {
				metaKey := "PluginAutomation:" + groupName
				if metaRaw, ok := entity.Meta[metaKey]; ok {
					var meta struct {
						Position int             `json:"position"`
						Entity   string          `json:"entity"`
						Control  json.RawMessage `json:"control"`
					}
					if err := json.Unmarshal(metaRaw, &meta); err != nil {
						log.Printf("plugin-automation: failed to unmarshal meta for %s: %v", entity.Key(), err)
						continue
					}
					if meta.Entity != "" {
						deviceKey := groupName + ":" + meta.Entity
						groupConfigs[deviceKey] = append(groupConfigs[deviceKey], positionedEntity{entity: entity, position: meta.Position})
					}
					if isControlMeta(meta.Control) {
						groupControls[groupName] = append(groupControls[groupName], entity.Key())
					}
				}
			}
		}
	}

	for deviceKey, members := range groupConfigs {
		parts := strings.SplitN(deviceKey, ":", 2)
		if len(parts) != 2 {
			continue
		}
		groupName := parts[0]
		entityType := parts[1]
		sort.Slice(members, func(i, j int) bool { return members[i].position < members[j].position })
		targets := make([]string, len(members))
		for i, m := range members {
			targets[i] = m.entity.Key()
		}
		groupID := NormalizeGroupID(groupName)
		targetQuery := storage.Query{
			Where: []storage.Filter{{Field: "labels.PluginAutomation", Op: storage.Eq, Value: groupName}},
		}
		targetJSON, _ := json.Marshal(targetQuery)

		var groupEntity domain.Entity
		switch entityType {
		case "light_strip":
			groupEntity = createLightStripEntity(groupID, groupName, targets, targetJSON)
		case "light":
			groupEntity = createLightEntity(groupID, groupName, members, targets, targetJSON)
		case "switch":
			groupEntity = createSwitchEntity(groupID, groupName, targets, targetJSON)
		default:
			groupEntity = domain.Entity{
				ID: groupID, Plugin: PluginID, DeviceID: "group", Type: entityType, Name: groupName,
				Target: targetJSON,
				State:  GroupState{MemberCount: len(members), Status: "active"},
				Labels: map[string][]string{"group_type": {entityType}},
			}
		}

		// Preserve existing state from storage so rediscovery doesn't
		// reset state that was set by commands (e.g. power, brightness, rgb).
		existingKey := domain.EntityKey{Plugin: PluginID, DeviceID: "group", ID: groupID}
		if raw, err := a.store.Get(existingKey); err == nil {
			var existing domain.Entity
			if err := json.Unmarshal(raw, &existing); err == nil && existing.Type == groupEntity.Type {
				groupEntity.State = existing.State
			}
		}
		// Store control entity keys in labels so they're discoverable
		// without clobbering the domain-specific state (e.g. domain.Light).
		controlKeys := groupControls[groupName]
		if len(controlKeys) > 0 {
			if groupEntity.Labels == nil {
				groupEntity.Labels = map[string][]string{}
			}
			groupEntity.Labels["group_control"] = controlKeys
			if gs, ok := groupEntity.State.(GroupState); ok {
				gs.Control = controlKeys
				groupEntity.State = gs
			}
		}

		if err := a.store.Save(groupEntity); err != nil {
			log.Printf("plugin-automation: failed to save %s group %s: %v", entityType, groupName, err)
			continue
		}
		// Persist group labels in sidecar, merging with any existing
		// sidecar labels (e.g. PluginHomeassistant) so we don't clobber
		// user-set labels on every discovery cycle.
		if len(groupEntity.Labels) > 0 {
			mergedLabels := map[string][]string{}
			if raw, err := a.store.Get(domain.EntityKey{Plugin: PluginID, DeviceID: "group", ID: groupID}); err == nil {
				var existing domain.Entity
				if err := json.Unmarshal(raw, &existing); err == nil {
					for k, v := range existing.Labels {
						mergedLabels[k] = v
					}
				}
			}
			for k, v := range groupEntity.Labels {
				mergedLabels[k] = v
			}
			profileData, _ := json.Marshal(map[string]any{"labels": mergedLabels})
			if err := a.store.SetProfile(groupEntity, json.RawMessage(profileData)); err != nil {
				log.Printf("plugin-automation: failed to setprofile %s group %s: %v", entityType, groupName, err)
			}
		}
		log.Printf("plugin-automation: %s %s updated with %d members", entityType, groupName, len(members))

		// Subscribe to control entity state changes.
		for _, ck := range controlKeys {
			a.subscribeControl(ck, groupEntity)
		}

		// Watch member state changes and aggregate into the group.
		a.watchGroup(groupEntity)
	}

	existingGroups, err := a.store.Query(storage.Query{Pattern: PluginID + ".group.>"})
	if err != nil {
		return fmt.Errorf("query existing groups: %w", err)
	}
	for _, entry := range existingGroups {
		var group domain.Entity
		if err := json.Unmarshal(entry.Data, &group); err != nil {
			continue
		}
		found := false
		for deviceKey := range groupConfigs {
			parts := strings.SplitN(deviceKey, ":", 2)
			if len(parts) == 2 && NormalizeGroupID(parts[0]) == group.ID {
				found = true
				break
			}
		}
		if !found {
			// Group exists in storage but wasn't discovered from meta.
			// Set up Watch aggregation so its state reflects members.
			a.watchGroup(group)
		}
	}
	return nil
}

// SHIM: TODO: Investigate the removal of this legacy string-to-bool translation.
// It currently supports a legacy format where a non-empty string is truthy.
func isControlMeta(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s != ""
	}
	return false
}

// subscribeControl subscribes to state changes for a control entity and
// forwards on/off commands to the group's light members.
func (a *App) subscribeControl(controlKey string, group domain.Entity) {
	// Skip if already subscribed to this control entity.
	if _, ok := a.controlSubs[controlKey]; ok {
		return
	}

	subject := "state.changed." + controlKey
	sub, err := a.msg.Subscribe(subject, func(m *messenger.Message) {
		var ent domain.Entity
		if err := json.Unmarshal(m.Data, &ent); err != nil {
			return
		}
		on, valid := controlSignal(ent)
		if !valid {
			log.Printf("plugin-automation: control entity %s has unrecognized type %s", controlKey, ent.Type)
			return
		}
		a.handleControlChange(group, on, m.Headers)
	})
	if err != nil {
		log.Printf("plugin-automation: failed to subscribe to control entity %s: %v", controlKey, err)
		return
	}
	a.controlSubs[controlKey] = sub
	log.Printf("plugin-automation: group %s controlled by %s", group.Name, controlKey)
}

// handleControlChange stops running scripts and forwards on/off to light members.
func (a *App) handleControlChange(group domain.Entity, on bool, headers messenger.Headers) {
	traceID := messenger.TraceID(headers)
	a.appendLog("control.triggered", "info", "group control triggered", messenger.Address{
		Plugin:   group.Plugin,
		DeviceID: group.DeviceID,
		EntityID: group.ID,
	}, nil, traceID, map[string]any{"power": on})

	// Stop any running scripts targeting this group's members.
	if err := a.stopAllScriptsMatchingGroup(group, traceID); err != nil {
		log.Printf("plugin-automation: control stop scripts for %s: %v", group.Key(), err)
	}

	// Resolve light members and forward the command.
	memberKeys, err := a.resolveGroupMemberKeys(group)
	if err != nil {
		log.Printf("plugin-automation: control resolve members for %s: %v", group.Key(), err)
		return
	}

	cmd := "light_turn_off"
	if on {
		cmd = "light_turn_on"
	}

	for key := range memberKeys {
		// Only send to light entities — resolve and check type.
		raw, err := a.store.Get(entityKeyFromString(key))
		if err != nil {
			continue
		}
		var member domain.Entity
		if err := json.Unmarshal(raw, &member); err != nil {
			continue
		}
		if member.Type != "light" {
			continue
		}
		subject := key + ".command." + cmd
		outHeaders := messenger.WithOrigin(headers, PluginID, group.Key(), cmd)
		if err := a.msg.PublishWithHeaders(subject, []byte("{}"), outHeaders); err != nil {
			log.Printf("plugin-automation: control forward %s to %s: %v", cmd, key, err)
			continue
		}
		a.appendLog("control.forwarded", "info", "group control forwarded command", messenger.Address{
			Plugin:   group.Plugin,
			DeviceID: group.DeviceID,
			EntityID: group.ID,
		}, nil, traceID, map[string]any{"recipient": key, "command": cmd})
	}
}

// watchGroup sets up a storage.Watch for a light group's target query.
// On initial setup and on every member add/update/remove, it aggregates
// member light states into the group entity and saves it.
func (a *App) watchGroup(group domain.Entity) {
	if group.Type != "light" {
		return
	}
	a.groupMu.RLock()
	_, alreadyWatching := a.groupWatchers[group.ID]
	a.groupMu.RUnlock()
	if alreadyWatching {
		return
	}

	log.Printf("plugin-automation: watchGroup %s", group.Name)

	var targetQuery storage.Query
	if err := json.Unmarshal(group.Target, &targetQuery); err != nil {
		log.Printf("plugin-automation: watchGroup %s: decode target: %v", group.Name, err)
		return
	}

	// Initial aggregation from current member states in storage.
	a.aggregateAndSave(group, nil)

	onChange := func(_ string, _ json.RawMessage) {
		a.groupMu.RLock()
		w := a.groupWatchers[group.ID]
		a.groupMu.RUnlock()
		a.aggregateAndSave(group, w)
	}

	w, err := storage.Watch(a.msg, targetQuery, storage.WatchHandlers{
		OnAdd:    onChange,
		OnUpdate: onChange,
		OnRemove: onChange,
	})
	if err != nil {
		log.Printf("plugin-automation: watchGroup %s: watch: %v", group.Name, err)
		return
	}

	a.groupMu.Lock()
	a.groupWatchers[group.ID] = w
	a.groupMu.Unlock()
}

// aggregateAndSave reads all member light states and saves the aggregate
// to the group entity. Uses the watcher's tracked set if available,
// otherwise falls back to a storage query.
func (a *App) aggregateAndSave(group domain.Entity, w *storage.Watcher) {
	var memberLights []domain.Light

	if w != nil {
		// Use watcher's tracked set — already filtered and up to date.
		for _, raw := range w.Tracked() {
			if light, skip := extractLight(raw, PluginID); !skip {
				memberLights = append(memberLights, light)
			}
		}
	} else {
		// Initial call — query storage directly.
		var targetQuery storage.Query
		if err := json.Unmarshal(group.Target, &targetQuery); err != nil {
			return
		}
		entries, err := a.store.Query(targetQuery)
		if err != nil {
			log.Printf("plugin-automation: aggregateAndSave %s: query error: %v", group.Name, err)
			return
		}
		for _, entry := range entries {
			if light, skip := extractLight(entry.Data, PluginID); !skip {
				memberLights = append(memberLights, light)
			}
		}
	}

	agg := AggregateLightState(memberLights)

	// Load existing group to preserve non-state fields.
	key := domain.EntityKey{Plugin: PluginID, DeviceID: "group", ID: group.ID}
	raw, err := a.store.Get(key)
	if err != nil {
		return
	}
	var current domain.Entity
	if err := json.Unmarshal(raw, &current); err != nil {
		return
	}
	current.State = agg
	a.store.Save(current)
}

// extractLight tries to extract a domain.Light from raw entity JSON.
// Returns the light and skip=false on success, or zero light and skip=true
// if the entity should be skipped (group entity, parse error, or no light state).
func extractLight(data json.RawMessage, pluginID string) (domain.Light, bool) {
	var ent domain.Entity
	if err := json.Unmarshal(data, &ent); err != nil {
		return domain.Light{}, true
	}
	if ent.Plugin == pluginID && ent.DeviceID == "group" {
		return domain.Light{}, true
	}
	if light, ok := ent.State.(domain.Light); ok {
		return light, false
	}
	// Entity type not in registry (e.g. wiz_light) — try raw state unmarshal.
	var envelope struct {
		State json.RawMessage `json:"state"`
	}
	if json.Unmarshal(data, &envelope) != nil || len(envelope.State) == 0 {
		return domain.Light{}, true
	}
	var light domain.Light
	if json.Unmarshal(envelope.State, &light) != nil {
		return domain.Light{}, true
	}
	return light, false
}

// entityKeyFromString parses "plugin.device.id" into a domain.EntityKey.
func entityKeyFromString(key string) domain.EntityKey {
	parts := strings.SplitN(key, ".", 3)
	if len(parts) != 3 {
		return domain.EntityKey{}
	}
	return domain.EntityKey{Plugin: parts[0], DeviceID: parts[1], ID: parts[2]}
}

func createLightStripEntity(groupID, groupName string, targets []string, targetJSON json.RawMessage) domain.Entity {
	return domain.Entity{
		ID: groupID, Plugin: PluginID, DeviceID: "group", Type: "light_strip", Name: groupName,
		Commands: []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_rgb", "light_set_color_temp", "lightstrip_set_segments", "script_run", "script_stop_all"},
		State:    domain.LightStrip{Power: false, Targets: targets},
		Target:   targetJSON,
		Labels:   map[string][]string{"group_type": {"light_strip"}},
	}
}

func createLightEntity(groupID, groupName string, members []positionedEntity, targets []string, targetJSON json.RawMessage) domain.Entity {
	return domain.Entity{
		ID: groupID, Plugin: PluginID, DeviceID: "group", Type: "light", Name: groupName,
		Commands: groupLightCommands(members),
		State:    domain.Light{Power: false},
		Target:   targetJSON,
		Labels:   map[string][]string{"group_type": {"light"}},
	}
}

func groupLightCommands(members []positionedEntity) []string {
	cmds := []string{"light_turn_on", "light_turn_off", "light_set_brightness"}
	if groupSupportsColorTemperature(members) {
		cmds = append(cmds, "light_set_color_temp")
	}
	if groupSupportsRGB(members) {
		cmds = append(cmds, "light_set_rgb")
	}
	cmds = append(cmds, "script_run", "script_stop_all")
	return cmds
}

func groupSupportsColorTemperature(members []positionedEntity) bool {
	for _, member := range members {
		for _, cmd := range member.entity.Commands {
			if cmd == "light_set_color_temp" {
				return true
			}
		}
		if light, ok := member.entity.State.(domain.Light); ok {
			if light.Temperature > 0 || light.ColorMode == "color_temp" || light.ColorMode == "cold_warm_white" {
				return true
			}
		}
	}
	return false
}

func groupSupportsRGB(members []positionedEntity) bool {
	for _, member := range members {
		light, ok := member.entity.State.(domain.Light)
		hasStateRGB := ok && (len(light.RGB) == 3 || len(light.RGBW) == 4 || len(light.RGBWW) == 5 || strings.HasPrefix(light.ColorMode, "rgb"))
		if hasStateRGB {
			return true
		}
		if ok && (light.ColorMode == "color_temp" || light.ColorMode == "cold_warm_white") && light.Temperature > 0 {
			continue
		}
		for _, cmd := range member.entity.Commands {
			switch cmd {
			case "light_set_rgb", "light_set_rgbw", "light_set_rgbww":
				return true
			}
		}
	}
	return false
}

func createSwitchEntity(groupID, groupName string, targets []string, targetJSON json.RawMessage) domain.Entity {
	return domain.Entity{
		ID: groupID, Plugin: PluginID, DeviceID: "group", Type: "switch", Name: groupName,
		Commands: []string{"switch_turn_on", "switch_turn_off", "switch_toggle", "script_run", "script_stop_all"},
		State:    domain.Switch{Power: false},
		Target:   targetJSON,
		Labels:   map[string][]string{"group_type": {"switch"}},
	}
}

func NormalizeGroupID(name string) string {
	var result strings.Builder
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			result.WriteRune(r + ('a' - 'A'))
		case r == ' ':
			result.WriteRune('-')
		default:
			result.WriteRune(r)
		}
	}
	return result.String()
}

// handleCommand processes incoming commands for entities managed by plugin-automation.
func (a *App) handleCommand(addr messenger.Address, cmd any) {
	a.handleCommandWithTrace(addr, cmd, "")
}

func (a *App) handleCommandMessage(addr messenger.Address, cmd any, msg *messenger.Message) {
	traceID := ""
	if msg != nil {
		traceID = messenger.TraceID(msg.Headers)
	}
	a.handleCommandWithTrace(addr, cmd, traceID)
}

func (a *App) handleCommandWithTrace(addr messenger.Address, cmd any, traceID string) {
	a.appendLog("command.received", "info", "received command", addr, cmd, traceID, nil)
	switch c := cmd.(type) {
	case domain.LightTurnOn:
		log.Printf("plugin-automation: light %s turn_on", addr.Key())
	case domain.LightTurnOff:
		log.Printf("plugin-automation: light %s turn_off transition=%v", addr.Key(), c.Transition)
	case domain.LightSetBrightness:
		log.Printf("plugin-automation: light %s set_brightness brightness=%d", addr.Key(), c.Brightness)
	case domain.LightSetColorTemp:
		log.Printf("plugin-automation: light %s set_color_temp mireds=%d", addr.Key(), c.Mireds)
	case domain.LightSetRGB:
		log.Printf("plugin-automation: light %s set_rgb r=%d g=%d b=%d", addr.Key(), c.R, c.G, c.B)
	case domain.LightSetRGBW:
		log.Printf("plugin-automation: light %s set_rgbw r=%d g=%d b=%d w=%d", addr.Key(), c.R, c.G, c.B, c.W)
	case domain.LightSetRGBWW:
		log.Printf("plugin-automation: light %s set_rgbww r=%d g=%d b=%d cw=%d ww=%d", addr.Key(), c.R, c.G, c.B, c.CW, c.WW)
	case domain.LightSetHS:
		log.Printf("plugin-automation: light %s set_hs hue=%.1f sat=%.1f", addr.Key(), c.Hue, c.Saturation)
	case domain.LightSetXY:
		log.Printf("plugin-automation: light %s set_xy x=%.4f y=%.4f", addr.Key(), c.X, c.Y)
	case domain.LightSetWhite:
		log.Printf("plugin-automation: light %s set_white white=%d", addr.Key(), c.White)
	case domain.LightSetEffect:
		log.Printf("plugin-automation: light %s set_effect effect=%s", addr.Key(), c.Effect)
	case domain.SwitchTurnOn:
		log.Printf("plugin-automation: switch %s turn_on", addr.Key())
	case domain.SwitchTurnOff:
		log.Printf("plugin-automation: switch %s turn_off", addr.Key())
	case domain.SwitchToggle:
		log.Printf("plugin-automation: switch %s toggle", addr.Key())
	case domain.FanTurnOn:
		log.Printf("plugin-automation: fan %s turn_on", addr.Key())
	case domain.FanTurnOff:
		log.Printf("plugin-automation: fan %s turn_off", addr.Key())
	case domain.FanSetSpeed:
		log.Printf("plugin-automation: fan %s set_speed percentage=%d", addr.Key(), c.Percentage)
	case domain.CoverOpen:
		log.Printf("plugin-automation: cover %s open", addr.Key())
	case domain.CoverClose:
		log.Printf("plugin-automation: cover %s close", addr.Key())
	case domain.CoverSetPosition:
		log.Printf("plugin-automation: cover %s set_position pos=%d", addr.Key(), c.Position)
	case domain.LockLock:
		log.Printf("plugin-automation: lock %s lock", addr.Key())
	case domain.LockUnlock:
		log.Printf("plugin-automation: lock %s unlock", addr.Key())
	case domain.ButtonPress:
		log.Printf("plugin-automation: button %s press", addr.Key())
	case domain.NumberSetValue:
		log.Printf("plugin-automation: number %s set_value value=%v", addr.Key(), c.Value)
	case domain.SelectOption:
		log.Printf("plugin-automation: select %s set_option option=%s", addr.Key(), c.Option)
	case domain.TextSetValue:
		log.Printf("plugin-automation: text %s set_value value=%s", addr.Key(), c.Value)
	case domain.ClimateSetMode:
		log.Printf("plugin-automation: climate %s set_mode mode=%s", addr.Key(), c.HVACMode)
	case domain.ClimateSetTemperature:
		log.Printf("plugin-automation: climate %s set_temperature temp=%v", addr.Key(), c.Temperature)
	case ScriptRun:
		log.Printf("plugin-automation: group %s script_run name=%s", addr.Key(), c.Name)
		if err := a.runScriptForGroup(addr, c.Name, traceID); err != nil {
			log.Printf("plugin-automation: group %s script_run failed: %v", addr.Key(), err)
			a.appendLog("script.run.failed", "error", fmt.Sprintf("script_run failed: %v", err), addr, cmd, traceID, map[string]any{"script_name": c.Name})
		}
	case ScriptStopAll:
		log.Printf("plugin-automation: group %s script_stop_all", addr.Key())
		if err := a.stopAllScriptsForGroup(addr, traceID); err != nil {
			log.Printf("plugin-automation: group %s script_stop_all failed: %v", addr.Key(), err)
			a.appendLog("script.stop_all.failed", "error", fmt.Sprintf("script_stop_all failed: %v", err), addr, cmd, traceID, nil)
		}
	default:
		log.Printf("plugin-automation: unknown command %T for %s", cmd, addr.Key())
	}
}

func (a *App) appendLog(kind, level, message string, addr messenger.Address, cmd any, traceID string, data map[string]any) {
	if a == nil || a.logger == nil {
		return
	}
	action := commandActionName(cmd)
	event := logging.Event{
		ID:      fmt.Sprintf("%s-%d", PluginID, atomic.AddUint64(&logSequence, 1)),
		TS:      time.Now().UTC(),
		Source:  PluginID,
		Kind:    kind,
		Level:   level,
		Message: message,
		Plugin:  addr.Plugin,
		Device:  addr.DeviceID,
		Entity:  addr.Key(),
		Action:  action,
		TraceID: traceID,
		Data:    data,
	}
	if err := a.logger.Append(context.Background(), event); err != nil {
		log.Printf("plugin-automation: append log failed: %v", err)
	}
}

func commandActionName(cmd any) string {
	if action, ok := cmd.(interface{ ActionName() string }); ok {
		return action.ActionName()
	}
	return ""
}

func (a *App) runScriptForGroup(addr messenger.Address, name, traceID string) error {
	group, err := a.loadGroupEntity(addr)
	if err != nil {
		return err
	}
	if err := a.stopAllScriptsMatchingGroup(group, traceID); err != nil {
		return err
	}
	queryRef, err := a.ensureGroupQueryRef(group)
	if err != nil {
		return err
	}
	_, err = a.requestScriptAPI("script.start", map[string]any{
		"name":     name,
		"queryRef": queryRef,
	}, traceID)
	return err
}

func (a *App) stopAllScriptsForGroup(addr messenger.Address, traceID string) error {
	group, err := a.loadGroupEntity(addr)
	if err != nil {
		return err
	}
	queryRef, err := a.ensureGroupQueryRef(group)
	if err != nil {
		return err
	}
	_, err = a.requestScriptAPI("script.stop_all", map[string]any{
		"queryRef": queryRef,
	}, traceID)
	return err
}

func (a *App) stopAllScriptsMatchingGroup(group domain.Entity, traceID string) error {
	queryRef, err := a.ensureGroupQueryRef(group)
	if err != nil {
		return err
	}
	_, err = a.requestScriptAPI("script.stop_all", map[string]any{
		"queryRef": queryRef,
	}, traceID)
	return err
}

func (a *App) loadGroupEntity(addr messenger.Address) (domain.Entity, error) {
	raw, err := a.store.Get(domain.EntityKey{
		Plugin:   addr.Plugin,
		DeviceID: addr.DeviceID,
		ID:       addr.EntityID,
	})
	if err != nil {
		return domain.Entity{}, fmt.Errorf("load group entity %s: %w", addr.Key(), err)
	}
	var group domain.Entity
	if err := json.Unmarshal(raw, &group); err != nil {
		return domain.Entity{}, fmt.Errorf("decode group entity %s: %w", addr.Key(), err)
	}
	return group, nil
}

func (a *App) resolveGroupMemberKeys(group domain.Entity) (map[string]struct{}, error) {
	var q storage.Query
	if err := json.Unmarshal(group.Target, &q); err != nil {
		return nil, fmt.Errorf("decode group target for %s: %w", group.Key(), err)
	}
	entries, err := a.store.Query(q)
	if err != nil {
		return nil, fmt.Errorf("query group members for %s: %w", group.Key(), err)
	}
	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		out[entry.Key] = struct{}{}
	}
	return out, nil
}

func (a *App) ensureGroupQueryRef(group domain.Entity) (string, error) {
	var q storage.Query
	if err := json.Unmarshal(group.Target, &q); err != nil {
		return "", fmt.Errorf("decode group target for %s: %w", group.Key(), err)
	}
	if err := storage.EnsureQueryLayout(a.store); err != nil {
		return "", fmt.Errorf("ensure query layout: %w", err)
	}
	ref := groupQueryRef(group)
	if err := storage.SaveQueryDefinition(a.store, ref, q); err != nil {
		return "", fmt.Errorf("save query definition %s: %w", ref, err)
	}
	return ref, nil
}

func groupQueryRef(group domain.Entity) string {
	return fmt.Sprintf("plugin_automation_group_%s_%s", group.DeviceID, group.ID)
}

// controlSignal extracts an on/off signal from a control entity.
// Returns (on, true) for recognized types, (false, false) for unknown.
func controlSignal(ent domain.Entity) (on bool, valid bool) {
	switch ent.Type {
	case "switch":
		if sw, ok := ent.State.(domain.Switch); ok {
			return sw.Power, true
		}
		// Handle map form from JSON unmarshal without type registration.
		if m, ok := ent.State.(map[string]any); ok {
			if p, ok := m["power"].(bool); ok {
				return p, true
			}
		}
	case "binary_sensor":
		if bs, ok := ent.State.(domain.BinarySensor); ok {
			return bs.On, true
		}
		if m, ok := ent.State.(map[string]any); ok {
			if o, ok := m["on"].(bool); ok {
				return o, true
			}
		}
	case "button":
		// Buttons are fire-once: any state change means "on".
		return true, true
	}
	return false, false
}

func (a *App) requestScriptAPI(subject string, body any, traceID string) (*scriptAPIResponse, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", subject, err)
	}
	headers := messenger.WithOrigin(messenger.WithTraceID(nil, traceID), PluginID, subject, subject)
	respMsg, err := a.msg.RequestWithHeaders(subject, data, headers, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%s request: %w", subject, err)
	}
	var resp scriptAPIResponse
	if err := json.Unmarshal(respMsg.Data, &resp); err != nil {
		return nil, fmt.Errorf("parse %s response: %w", subject, err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s error: %s", subject, resp.Error)
	}
	return &resp, nil
}
