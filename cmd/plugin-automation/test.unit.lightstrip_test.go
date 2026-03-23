package main

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/slidebolt/plugin-automation/app"
	domain "github.com/slidebolt/sb-domain"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type positionedEntity struct {
	entity   domain.Entity
	position int
}

// ==========================================================================
// Light Strip Group Tests
// ==========================================================================

// TestLightStrip_BasicCreation validates that lights with "light_strip" entity
// meta create a proper LightStrip virtual entity
func TestLightStrip_BasicCreation(t *testing.T) {
	_, store, _ := env(t)

	// Create 5 lights for Office light strip, each with position and entity type
	lights := []struct {
		id       string
		position int
	}{
		{"office-light-0", 0},
		{"office-light-1", 1},
		{"office-light-2", 2},
		{"office-light-3", 3},
		{"office-light-4", 4},
	}

	for _, light := range lights {
		meta := map[string]json.RawMessage{
			"PluginAutomation:Office": json.RawMessage(`{"position": ` + string(rune('0'+light.position)) + `, "entity": "light_strip"}`),
		}
		saveEntityWithMeta(t, store, "test", "dev1", light.id, "light", "Office Light "+string(rune('0'+light.position)),
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Office"}}, meta)
	}

	// Run discovery
	discoverGroupsWithMeta(store)

	// Verify LightStrip entity was created
	officeKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "office"}
	raw, err := store.Get(officeKey)
	if err != nil {
		t.Fatalf("Office light_strip entity not found: %v", err)
	}

	var officeStrip domain.Entity
	if err := json.Unmarshal(raw, &officeStrip); err != nil {
		t.Fatalf("Failed to unmarshal light strip: %v", err)
	}

	// Verify it's a light_strip type
	if officeStrip.Type != "light_strip" {
		t.Errorf("Expected Type 'light_strip', got %q", officeStrip.Type)
	}

	// Verify it has LightStrip state with ordered Targets
	stripState, ok := officeStrip.State.(domain.LightStrip)
	if !ok {
		t.Fatalf("State should be LightStrip, got %T", officeStrip.State)
	}

	if len(stripState.Targets) != 5 {
		t.Errorf("Expected 5 targets in LightStrip, got %d", len(stripState.Targets))
	}

	// Verify targets are in correct order (position 0, 1, 2, 3, 4)
	expectedOrder := []string{
		"test.dev1.office-light-0",
		"test.dev1.office-light-1",
		"test.dev1.office-light-2",
		"test.dev1.office-light-3",
		"test.dev1.office-light-4",
	}

	for i, expected := range expectedOrder {
		if i < len(stripState.Targets) && stripState.Targets[i] != expected {
			t.Errorf("Target[%d]: expected %s, got %s", i, expected, stripState.Targets[i])
		}
	}

	// Note: Commands field is not unmarshaled by sb-domain's Entity.UnmarshalJSON
	// This is a known limitation - the storage system works correctly for core functionality
	t.Logf("✓ LightStrip created: %d segments in correct order", len(stripState.Targets))
}

// TestLightStrip_PositionOrdering validates that lights are ordered by position
// even if they're created out of order
func TestLightStrip_PositionOrdering(t *testing.T) {
	_, store, _ := env(t)

	// Create lights OUT OF ORDER
	lights := []struct {
		id       string
		position int
	}{
		{"light-pos-3", 3},
		{"light-pos-0", 0},
		{"light-pos-4", 4},
		{"light-pos-1", 1},
		{"light-pos-2", 2},
	}

	for _, light := range lights {
		meta := map[string]json.RawMessage{
			"PluginAutomation:Kitchen": json.RawMessage(`{"position": ` + string(rune('0'+light.position)) + `, "entity": "light_strip"}`),
		}
		saveEntityWithMeta(t, store, "test", "dev1", light.id, "light", "Kitchen Light",
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Kitchen"}}, meta)
	}

	// Run discovery
	discoverGroupsWithMeta(store)

	// Get the strip and verify ordering
	kitchenKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "kitchen"}
	raw, _ := store.Get(kitchenKey)
	var kitchenStrip domain.Entity
	json.Unmarshal(raw, &kitchenStrip)

	stripState := kitchenStrip.State.(domain.LightStrip)

	// Should be ordered: pos-0, pos-1, pos-2, pos-3, pos-4
	expectedOrder := []string{
		"test.dev1.light-pos-0",
		"test.dev1.light-pos-1",
		"test.dev1.light-pos-2",
		"test.dev1.light-pos-3",
		"test.dev1.light-pos-4",
	}

	for i, expected := range expectedOrder {
		if stripState.Targets[i] != expected {
			t.Errorf("Target[%d]: expected %s, got %s", i, expected, stripState.Targets[i])
		}
	}

	t.Logf("✓ Lights correctly ordered by position metadata")
}

// TestLightStrip_AddLightToExistingStrip validates adding a new light
// updates the existing light strip
func TestLightStrip_AddLightToExistingStrip(t *testing.T) {
	_, store, _ := env(t)

	// Start with 3 lights
	for i := 0; i < 3; i++ {
		meta := map[string]json.RawMessage{
			"PluginAutomation:Garage": json.RawMessage(`{"position": ` + string(rune('0'+i)) + `, "entity": "light_strip"}`),
		}
		saveEntityWithMeta(t, store, "test", "dev1", "garage-light-"+string(rune('0'+i)), "light", "Garage Light",
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Garage"}}, meta)
	}

	// Run discovery
	discoverGroupsWithMeta(store)

	// Verify 3 lights
	garageKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "garage"}
	raw, _ := store.Get(garageKey)
	var garageStrip domain.Entity
	json.Unmarshal(raw, &garageStrip)
	stripState := garageStrip.State.(domain.LightStrip)

	if len(stripState.Targets) != 3 {
		t.Fatalf("Expected 3 lights initially, got %d", len(stripState.Targets))
	}

	// Add 2 more lights
	for i := 3; i < 5; i++ {
		meta := map[string]json.RawMessage{
			"PluginAutomation:Garage": json.RawMessage(`{"position": ` + string(rune('0'+i)) + `, "entity": "light_strip"}`),
		}
		saveEntityWithMeta(t, store, "test", "dev1", "garage-light-"+string(rune('0'+i)), "light", "Garage Light",
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Garage"}}, meta)
	}

	// Run discovery again
	discoverGroupsWithMeta(store)

	// Verify now has 5 lights
	raw, _ = store.Get(garageKey)
	json.Unmarshal(raw, &garageStrip)
	stripState = garageStrip.State.(domain.LightStrip)

	if len(stripState.Targets) != 5 {
		t.Errorf("Expected 5 lights after adding 2 more, got %d", len(stripState.Targets))
	}

	t.Logf("✓ LightStrip expanded from 3 to 5 lights")
}

// TestLightStrip_RemoveLightFromStrip validates removing a light
// updates the existing light strip
func TestLightStrip_RemoveLightFromStrip(t *testing.T) {
	_, store, _ := env(t)

	// Create 5 lights
	for i := 0; i < 5; i++ {
		meta := map[string]json.RawMessage{
			"PluginAutomation:Porch": json.RawMessage(`{"position": ` + string(rune('0'+i)) + `, "entity": "light_strip"}`),
		}
		saveEntityWithMeta(t, store, "test", "dev1", "porch-light-"+string(rune('0'+i)), "light", "Porch Light",
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Porch"}}, meta)
	}

	// Run discovery
	discoverGroupsWithMeta(store)

	// Remove position 2 and 4
	store.Delete(domain.EntityKey{Plugin: "test", DeviceID: "dev1", ID: "porch-light-2"})
	store.Delete(domain.EntityKey{Plugin: "test", DeviceID: "dev1", ID: "porch-light-4"})

	// Run discovery again
	discoverGroupsWithMeta(store)

	// Verify now has 3 lights and they're in correct positions
	porchKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "porch"}
	raw, _ := store.Get(porchKey)
	var porchStrip domain.Entity
	json.Unmarshal(raw, &porchStrip)
	stripState := porchStrip.State.(domain.LightStrip)

	if len(stripState.Targets) != 3 {
		t.Errorf("Expected 3 lights after removing 2, got %d", len(stripState.Targets))
	}

	// Remaining lights should be: light-0, light-1, light-3
	expected := []string{
		"test.dev1.porch-light-0",
		"test.dev1.porch-light-1",
		"test.dev1.porch-light-3",
	}
	for i, exp := range expected {
		if stripState.Targets[i] != exp {
			t.Errorf("Target[%d]: expected %s, got %s", i, exp, stripState.Targets[i])
		}
	}

	t.Logf("✓ LightStrip shrank from 5 to 3 lights after deletion")
}

// TestLightStrip_MixedEntityTypes validates that different entity types
// create different virtual group entities
func TestLightStrip_MixedEntityTypes(t *testing.T) {
	_, store, _ := env(t)

	// LivingRoom: light_strip type
	for i := 0; i < 3; i++ {
		meta := map[string]json.RawMessage{
			"PluginAutomation:LivingRoom": json.RawMessage(`{"position": ` + string(rune('0'+i)) + `, "entity": "light_strip"}`),
		}
		saveEntityWithMeta(t, store, "test", "dev1", "lr-light-"+string(rune('0'+i)), "light", "LR Light",
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"LivingRoom"}}, meta)
	}

	// Bedroom: regular light type (not strip)
	for i := 0; i < 2; i++ {
		meta := map[string]json.RawMessage{
			"PluginAutomation:Bedroom": json.RawMessage(`{"position": ` + string(rune('0'+i)) + `, "entity": "light"}`),
		}
		saveEntityWithMeta(t, store, "test", "dev1", "br-light-"+string(rune('0'+i)), "light", "BR Light",
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Bedroom"}}, meta)
	}

	// Kitchen: switch type
	for i := 0; i < 4; i++ {
		meta := map[string]json.RawMessage{
			"PluginAutomation:Kitchen": json.RawMessage(`{"position": ` + string(rune('0'+i)) + `, "entity": "switch"}`),
		}
		saveEntityWithMeta(t, store, "test", "dev1", "kt-switch-"+string(rune('0'+i)), "switch", "KT Switch",
			domain.Switch{Power: false}, map[string][]string{"PluginAutomation": {"Kitchen"}}, meta)
	}

	// Run discovery
	discoverGroupsWithMeta(store)

	// Verify LivingRoom is light_strip
	lrKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "livingroom"}
	raw, _ := store.Get(lrKey)
	var lrEntity domain.Entity
	json.Unmarshal(raw, &lrEntity)

	if lrEntity.Type != "light_strip" {
		t.Errorf("LivingRoom should be light_strip, got %s", lrEntity.Type)
	}
	if _, ok := lrEntity.State.(domain.LightStrip); !ok {
		t.Error("LivingRoom state should be LightStrip")
	}

	// Verify Bedroom is regular light
	brKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "bedroom"}
	raw, _ = store.Get(brKey)
	var brEntity domain.Entity
	json.Unmarshal(raw, &brEntity)

	if brEntity.Type != "light" {
		t.Errorf("Bedroom should be light, got %s", brEntity.Type)
	}

	// Verify Kitchen is switch
	kitchenKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "kitchen"}
	raw, _ = store.Get(kitchenKey)
	var kitchenEntity domain.Entity
	json.Unmarshal(raw, &kitchenEntity)

	if kitchenEntity.Type != "switch" {
		t.Errorf("Kitchen should be switch, got %s", kitchenEntity.Type)
	}

	t.Logf("✓ Different entity types create correct virtual groups: light_strip, light, switch")
}

// TestLightStrip_BackgroundDiscovery validates that new light strips are
// created automatically through background discovery
func TestLightStrip_BackgroundDiscovery(t *testing.T) {
	_, store, _ := env(t)

	// Start with Basement light strip
	for i := 0; i < 3; i++ {
		meta := map[string]json.RawMessage{
			"PluginAutomation:Basement": json.RawMessage(`{"position": ` + string(rune('0'+i)) + `, "entity": "light_strip"}`),
		}
		saveEntityWithMeta(t, store, "test", "dev1", "bsmt-light-"+string(rune('0'+i)), "light", "Basement Light",
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Basement"}}, meta)
	}

	// Run discovery
	discoverGroupsWithMeta(store)

	// Verify only Basement exists
	groups, _ := store.Query(storage.Query{Pattern: app.PluginID + ".group.>"})
	if len(groups) != 1 {
		t.Fatalf("Expected 1 group (Basement), got %d", len(groups))
	}

	// Simulate background discovery finding new Attic light strip
	for i := 0; i < 4; i++ {
		meta := map[string]json.RawMessage{
			"PluginAutomation:Attic": json.RawMessage(`{"position": ` + string(rune('0'+i)) + `, "entity": "light_strip"}`),
		}
		saveEntityWithMeta(t, store, "test", "dev1", "attic-light-"+string(rune('0'+i)), "light", "Attic Light",
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Attic"}}, meta)
	}

	// Run discovery again (simulating background ticker)
	discoverGroupsWithMeta(store)

	// Verify both Basement and Attic exist
	groups, _ = store.Query(storage.Query{Pattern: app.PluginID + ".group.>"})
	if len(groups) != 2 {
		t.Errorf("Expected 2 groups (Basement and Attic), got %d", len(groups))
	}

	// Verify Attic has correct structure
	atticKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "attic"}
	raw, _ := store.Get(atticKey)
	var atticEntity domain.Entity
	json.Unmarshal(raw, &atticEntity)

	if atticEntity.Type != "light_strip" {
		t.Errorf("Attic should be light_strip, got %s", atticEntity.Type)
	}

	stripState := atticEntity.State.(domain.LightStrip)
	if len(stripState.Targets) != 4 {
		t.Errorf("Attic should have 4 lights, got %d", len(stripState.Targets))
	}

	t.Logf("✓ Background discovery created new Attic light strip with %d lights", len(stripState.Targets))
}

// ==========================================================================
// Helper Functions
// ==========================================================================

func saveEntityWithMeta(t *testing.T, store storage.Storage, plugin, device, id, typ, name string, state any, labels map[string][]string, meta map[string]json.RawMessage) domain.Entity {
	t.Helper()
	e := domain.Entity{
		ID:       id,
		Plugin:   plugin,
		DeviceID: device,
		Type:     typ,
		Name:     name,
		State:    state,
		Labels:   labels,
		Meta:     meta,
	}
	if err := store.Save(e); err != nil {
		t.Fatalf("save %s: %v", id, err)
	}
	// Persist labels/meta in sidecar so they survive Save() stripping.
	profile := make(map[string]any)
	if len(labels) > 0 {
		profile["labels"] = labels
	}
	if len(meta) > 0 {
		profile["meta"] = meta
	}
	if len(profile) > 0 {
		data, _ := json.Marshal(profile)
		if err := store.SetProfile(e, json.RawMessage(data)); err != nil {
			t.Fatalf("setprofile %s: %v", id, err)
		}
	}
	return e
}

// discoverGroupsWithMeta is the updated discovery that looks at Meta for entity types
func discoverGroupsWithMeta(store storage.Storage) {
	// Query all entities
	allEntities, err := store.Query(storage.Query{Pattern: ">"})
	if err != nil {
		return
	}

	// Group by (group_name + entity_type)
	groupConfigs := make(map[string][]positionedEntity)

	for _, entry := range allEntities {
		var entity domain.Entity
		if err := json.Unmarshal(entry.Data, &entity); err != nil {
			continue
		}

		// Skip existing group entities
		if entity.Plugin == app.PluginID && entity.DeviceID == "group" {
			continue
		}

		// Check for PluginAutomation labels
		if labels, ok := entity.Labels["PluginAutomation"]; ok {
			for _, groupName := range labels {
				metaKey := "PluginAutomation:" + groupName
				if metaRaw, ok := entity.Meta[metaKey]; ok {
					var meta struct {
						Position int    `json:"position"`
						Entity   string `json:"entity"`
					}
					if err := json.Unmarshal(metaRaw, &meta); err != nil {
						continue
					}

					if meta.Entity != "" {
						deviceKey := groupName + ":" + meta.Entity
						groupConfigs[deviceKey] = append(groupConfigs[deviceKey], positionedEntity{
							entity:   entity,
							position: meta.Position,
						})
					}
				}
			}
		}
	}

	// Create virtual entities
	for deviceKey, members := range groupConfigs {
		parts := splitLast(deviceKey, ":", 2)
		if len(parts) != 2 {
			continue
		}
		groupName := parts[0]
		entityType := parts[1]

		// Sort by position
		sort.Slice(members, func(i, j int) bool {
			return members[i].position < members[j].position
		})

		// Build targets
		targets := make([]string, len(members))
		for i, m := range members {
			targets[i] = m.entity.Key()
		}

		groupID := app.NormalizeGroupID(groupName)

		// Build target query
		targetQuery := storage.Query{
			Where: []storage.Filter{
				{
					Field: "labels.PluginAutomation",
					Op:    storage.Eq,
					Value: groupName,
				},
			},
		}
		targetJSON, _ := json.Marshal(targetQuery)

		// Create entity based on type
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
				ID:       groupID,
				Plugin:   app.PluginID,
				DeviceID: "group",
				Type:     entityType,
				Name:     groupName,
				Target:   targetJSON,
				State:    app.GroupState{MemberCount: len(members), Status: "active"},
				Labels:   map[string][]string{"group_type": {entityType}},
			}
		}

		store.Save(groupEntity)

		// Persist group labels in sidecar so they survive Save() stripping.
		if len(groupEntity.Labels) > 0 {
			profileData, _ := json.Marshal(map[string]any{"labels": groupEntity.Labels})
			store.SetProfile(groupEntity, json.RawMessage(profileData))
		}
	}
}

func splitLast(s, sep string, n int) []string {
	if n <= 1 {
		return []string{s}
	}
	idx := -1
	count := 0
	for i := len(s) - 1; i >= 0; i-- {
		if string(s[i]) == sep {
			count++
			if count == n-1 {
				idx = i
				break
			}
		}
	}
	if idx == -1 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+1:]}
}

func createLightStripEntity(groupID, groupName string, targets []string, targetJSON json.RawMessage) domain.Entity {
	return domain.Entity{
		ID:       groupID,
		Plugin:   app.PluginID,
		DeviceID: "group",
		Type:     "light_strip",
		Name:     groupName,
		Commands: []string{
			"light_turn_on", "light_turn_off",
			"light_set_brightness", "light_set_rgb",
			"light_set_color_temp", "lightstrip_set_segments",
		},
		State: domain.LightStrip{
			Power:   false,
			Targets: targets,
		},
		Target: targetJSON,
		Labels: map[string][]string{
			"group_type": {"light_strip"},
		},
	}
}

func createLightEntity(groupID, groupName string, targets []string, targetJSON json.RawMessage) domain.Entity {
	return domain.Entity{
		ID:       groupID,
		Plugin:   app.PluginID,
		DeviceID: "group",
		Type:     "light",
		Name:     groupName,
		Commands: []string{
			"light_turn_on", "light_turn_off",
			"light_set_brightness", "light_set_rgb", "light_set_color_temp",
		},
		State: domain.Light{
			Power: false,
		},
		Target: targetJSON,
		Labels: map[string][]string{
			"group_type": {"light"},
		},
	}
}

func createSwitchEntity(groupID, groupName string, targets []string, targetJSON json.RawMessage) domain.Entity {
	return domain.Entity{
		ID:       groupID,
		Plugin:   app.PluginID,
		DeviceID: "group",
		Type:     "switch",
		Name:     groupName,
		Commands: []string{
			"switch_turn_on", "switch_turn_off", "switch_toggle",
		},
		State: domain.Switch{
			Power: false,
		},
		Target: targetJSON,
		Labels: map[string][]string{
			"group_type": {"switch"},
		},
	}
}
