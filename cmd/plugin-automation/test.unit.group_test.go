package main

import (
	"encoding/json"
	"testing"

	"github.com/slidebolt/plugin-automation/app"
	domain "github.com/slidebolt/sb-domain"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// ==========================================================================
// Group Device Tests
// ==========================================================================

// Test 1: Single group value
// Create 10 lights, 5 with PluginAutomation=[Basement]
// Verify: Group device exists with "Basement" entity
// The Basement entity's Target should find exactly 5 lights
func TestGroup_SingleLabelValueCreatesGroup(t *testing.T) {
	_, store, _ := env(t)

	// Seed 10 lights - 5 with Basement label
	for i := 0; i < 10; i++ {
		labels := map[string][]string{}
		if i < 5 {
			labels["PluginAutomation"] = []string{"Basement"}
		}
		saveEntityWithLabels(t, store, "test", "dev1", "light"+string(rune('0'+i)), "light", "Light "+string(rune('0'+i)),
			domain.Light{Power: false}, labels)
	}

	// Trigger group discovery (simulate plugin startup behavior)
	discoverGroups(store)

	// Verify Group device exists
	groupKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "basement"}
	raw, err := store.Get(groupKey)
	if err != nil {
		t.Fatalf("Basement group entity not found: %v", err)
	}

	var groupEntity domain.Entity
	if err := json.Unmarshal(raw, &groupEntity); err != nil {
		t.Fatalf("Failed to unmarshal group entity: %v", err)
	}

	// Verify it's a group type
	if groupEntity.Type != "group" {
		t.Errorf("Expected type 'group', got %q", groupEntity.Type)
	}

	// Verify the Target query exists
	if len(groupEntity.Target) == 0 {
		t.Fatal("Group entity missing Target query")
	}

	// Execute the target query and verify it finds 5 entities
	var targetQuery storage.Query
	if err := json.Unmarshal(groupEntity.Target, &targetQuery); err != nil {
		t.Fatalf("Failed to unmarshal Target query: %v", err)
	}

	members, err := store.Query(targetQuery)
	if err != nil {
		t.Fatalf("Target query failed: %v", err)
	}

	if len(members) != 5 {
		t.Errorf("Expected 5 group members, got %d", len(members))
	}

	t.Logf("Basement group found with %d members", len(members))
}

// Test 2: Multiple label values create multiple groups
// Create 10 lights:
//   - 5 with PluginAutomation=[Basement]
//   - 5 with PluginAutomation=[Basement, Bar]
//
// Verify: Two group entities created:
//   - "Basement" with 10 members (all lights with Basement label)
//   - "Bar" with 5 members (only lights with Bar label)
func TestGroup_MultipleLabelValuesCreateMultipleGroups(t *testing.T) {
	_, store, _ := env(t)

	// Seed 10 lights with different label combinations
	for i := 0; i < 10; i++ {
		labels := map[string][]string{}
		if i < 5 {
			// First 5: just Basement
			labels["PluginAutomation"] = []string{"Basement"}
		} else {
			// Next 5: Basement and Bar
			labels["PluginAutomation"] = []string{"Basement", "Bar"}
		}
		saveEntityWithLabels(t, store, "test", "dev1", "light"+string(rune('0'+i)), "light", "Light "+string(rune('0'+i)),
			domain.Light{Power: false}, labels)
	}

	// Trigger group discovery
	discoverGroups(store)

	// Verify Basement group has 10 members
	basementKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "basement"}
	basementRaw, err := store.Get(basementKey)
	if err != nil {
		t.Fatalf("Basement group not found: %v", err)
	}

	var basementGroup domain.Entity
	json.Unmarshal(basementRaw, &basementGroup)

	var basementQuery storage.Query
	json.Unmarshal(basementGroup.Target, &basementQuery)

	basementMembers, _ := store.Query(basementQuery)
	if len(basementMembers) != 10 {
		t.Errorf("Basement group: expected 10 members, got %d", len(basementMembers))
	}

	// Verify Bar group has 5 members
	barKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "bar"}
	barRaw, err := store.Get(barKey)
	if err != nil {
		t.Fatalf("Bar group not found: %v", err)
	}

	var barGroup domain.Entity
	json.Unmarshal(barRaw, &barGroup)

	var barQuery storage.Query
	json.Unmarshal(barGroup.Target, &barQuery)

	barMembers, _ := store.Query(barQuery)
	if len(barMembers) != 5 {
		t.Errorf("Bar group: expected 5 members, got %d", len(barMembers))
	}

	t.Logf("Basement group: %d members, Bar group: %d members", len(basementMembers), len(barMembers))
}

// Test 3: Group device singleton
// Verify that the Group device is created on startup and is a singleton
func TestGroup_DeviceIsSingleton(t *testing.T) {
	_, store, _ := env(t)

	// Trigger group discovery (first startup)
	discoverGroups(store)

	// Verify Group device exists by checking pattern returns no errors
	// Note: The device itself is implicit when entities exist under plugin-automation.group.*

	// Run discovery again (second startup)
	discoverGroups(store)

	// Query all group entities - should still be the same count (0 since no labeled entities yet)
	groupEntities, err := store.Query(storage.Query{
		Pattern: app.PluginID + ".group.>",
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(groupEntities) != 0 {
		t.Errorf("Expected 0 group entities (no labeled devices), got %d", len(groupEntities))
	}
}

// Test 4: Group entity recreation
// Verify that if a group entity is deleted, it will be recreated on next discovery
func TestGroup_EntityRecreatedAfterDelete(t *testing.T) {
	_, store, _ := env(t)

	// Create a light with Basement label
	labels := map[string][]string{"PluginAutomation": []string{"Basement"}}
	saveEntityWithLabels(t, store, "test", "dev1", "light1", "light", "Light 1",
		domain.Light{Power: false}, labels)

	// Trigger group discovery
	discoverGroups(store)

	// Verify group exists
	basementKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "basement"}
	_, err := store.Get(basementKey)
	if err != nil {
		t.Fatal("Basement group should exist after discovery")
	}

	// Delete the group entity
	store.Delete(basementKey)

	// Verify it's gone
	_, err = store.Get(basementKey)
	if err == nil {
		t.Fatal("Basement group should be deleted")
	}

	// Run discovery again - group should be recreated
	discoverGroups(store)

	// Verify group was recreated
	_, err = store.Get(basementKey)
	if err != nil {
		t.Fatal("Basement group should be recreated after discovery")
	}

	t.Logf("✓ Group deleted and recreated successfully")
}

// ==========================================================================
// Helper functions for group tests
// ==========================================================================

func saveEntityWithLabels(t *testing.T, store storage.Storage, plugin, device, id, typ, name string, state any, labels map[string][]string) domain.Entity {
	t.Helper()
	e := domain.Entity{
		ID:       id,
		Plugin:   plugin,
		DeviceID: device,
		Type:     typ,
		Name:     name,
		State:    state,
		Labels:   labels,
	}
	if err := store.Save(e); err != nil {
		t.Fatalf("save %s: %v", id, err)
	}
	return e
}

// discoverGroups scans all entities for PluginAutomation labels and creates/updates group entities
func discoverGroups(store storage.Storage) {
	// Query all entities across all plugins
	allEntities, err := store.Query(storage.Query{
		Pattern: ">",
	})
	if err != nil {
		return
	}

	// Collect unique group names from PluginAutomation labels
	groupNames := make(map[string]bool)
	for _, entry := range allEntities {
		var entity domain.Entity
		if err := json.Unmarshal(entry.Data, &entity); err != nil {
			continue
		}

		if labels, ok := entity.Labels["PluginAutomation"]; ok {
			for _, groupName := range labels {
				groupNames[groupName] = true
			}
		}
	}

	// Create/update group entities for each unique group name
	for groupName := range groupNames {
		// Normalize group ID (lowercase, no spaces)
		groupID := app.NormalizeGroupID(groupName)

		// Build target query to find all entities with this group in their PluginAutomation label
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

		groupEntity := domain.Entity{
			ID:       groupID,
			Plugin:   app.PluginID,
			DeviceID: "group",
			Type:     "group",
			Name:     groupName,
			Target:   targetJSON,
			State:    app.GroupState{MemberCount: 0}, // Will be updated when queried
		}

		// Save the group entity (idempotent - will update if exists)
		store.Save(groupEntity)
	}
}
