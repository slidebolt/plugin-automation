package app

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	contract "github.com/slidebolt/sb-contract"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

const PluginID = "plugin-automation"

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

type GroupState struct {
	MemberCount int    `json:"member_count"`
	Status      string `json:"status,omitempty"`
}

type App struct {
	msg      messenger.Messenger
	store    storage.Storage
	cmds     *messenger.Commands
	subs     []messenger.Subscription
	ticker   *time.Ticker
	stopChan chan bool
}

type positionedEntity struct {
	entity   domain.Entity
	position int
}

func init() {
	domain.Register("group", GroupState{})
}

func New() *App { return &App{} }

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

	domain.Register("group", GroupState{})

	a.cmds = messenger.NewCommands(msg, domain.LookupCommand)
	sub, err := a.cmds.Receive(PluginID+".>", a.handleCommand)
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
	if a.store != nil {
		a.store.Close()
	}
	if a.msg != nil {
		a.msg.Close()
	}
	return nil
}

func (a *App) initializeGroupDevice() error { return a.discoverGroups() }

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
	allEntities, err := a.store.Query(storage.Query{Pattern: ">"})
	if err != nil {
		return fmt.Errorf("query all entities: %w", err)
	}

	groupConfigs := make(map[string][]positionedEntity)
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
						Position int    `json:"position"`
						Entity   string `json:"entity"`
					}
					if err := json.Unmarshal(metaRaw, &meta); err != nil {
						log.Printf("plugin-automation: failed to unmarshal meta for %s: %v", entity.Key(), err)
						continue
					}
					if meta.Entity != "" {
						deviceKey := groupName + ":" + meta.Entity
						groupConfigs[deviceKey] = append(groupConfigs[deviceKey], positionedEntity{entity: entity, position: meta.Position})
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
			groupEntity = createLightEntity(groupID, groupName, targets, targetJSON)
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
		if err := a.store.Save(groupEntity); err != nil {
			log.Printf("plugin-automation: failed to save %s group %s: %v", entityType, groupName, err)
			continue
		}
		log.Printf("plugin-automation: %s %s updated with %d members", entityType, groupName, len(members))
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
			group.State = GroupState{MemberCount: 0, Status: "inactive"}
			a.store.Save(group)
		}
	}
	return nil
}

func createLightStripEntity(groupID, groupName string, targets []string, targetJSON json.RawMessage) domain.Entity {
	return domain.Entity{
		ID: groupID, Plugin: PluginID, DeviceID: "group", Type: "light_strip", Name: groupName,
		Commands: []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_rgb", "light_set_color_temp", "lightstrip_set_segments"},
		State:    domain.LightStrip{Power: false, Targets: targets},
		Target:   targetJSON,
		Labels:   map[string][]string{"group_type": {"light_strip"}},
	}
}

func createLightEntity(groupID, groupName string, targets []string, targetJSON json.RawMessage) domain.Entity {
	return domain.Entity{
		ID: groupID, Plugin: PluginID, DeviceID: "group", Type: "light", Name: groupName,
		Commands: []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_rgb", "light_set_color_temp"},
		State:    domain.Light{Power: false},
		Target:   targetJSON,
		Labels:   map[string][]string{"group_type": {"light"}},
	}
}

func createSwitchEntity(groupID, groupName string, targets []string, targetJSON json.RawMessage) domain.Entity {
	return domain.Entity{
		ID: groupID, Plugin: PluginID, DeviceID: "group", Type: "switch", Name: groupName,
		Commands: []string{"switch_turn_on", "switch_turn_off", "switch_toggle"},
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

func (a *App) handleCommand(addr messenger.Address, cmd any) {
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
	default:
		log.Printf("plugin-automation: unknown command %T for %s", cmd, addr.Key())
	}
}
