package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/slidebolt/plugin-automation/app"
	domain "github.com/slidebolt/sb-domain"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// ==========================================================================
// Comprehensive Group Discovery Tests
// ==========================================================================

// TestGroupDiscovery_AddEntityLater validates that when a new entity is added
// after startup with a PluginAutomation label, the group is updated automatically
// through background discovery
func TestGroupDiscovery_AddEntityLater(t *testing.T) {
	_, store, _ := env(t)

	// Step 1: Initial state - create 3 lights with LivingRoom label
	for i := 1; i <= 3; i++ {
		saveEntityWithLabels(t, store, "test", "dev1", "light"+string(rune('0'+i)), "light", "Light "+string(rune('0'+i)),
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"LivingRoom"}})
	}

	// Step 2: Run initial discovery
	discoverGroups(store)

	// Verify LivingRoom group has 3 members
	livingRoomKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "livingroom"}
	raw, _ := store.Get(livingRoomKey)
	var livingRoomGroup domain.Entity
	json.Unmarshal(raw, &livingRoomGroup)

	var targetQuery storage.Query
	json.Unmarshal(livingRoomGroup.Target, &targetQuery)
	members, _ := store.Query(targetQuery)

	if len(members) != 3 {
		t.Errorf("Expected 3 LivingRoom members initially, got %d", len(members))
	}

	// Step 3: Add 2 more lights with LivingRoom label (simulating new device discovered)
	for i := 4; i <= 5; i++ {
		saveEntityWithLabels(t, store, "test", "dev1", "light"+string(rune('0'+i)), "light", "Light "+string(rune('0'+i)),
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"LivingRoom"}})
	}

	// Step 4: Run discovery again (simulating background discovery)
	discoverGroups(store)

	// Step 5: Verify LivingRoom group now has 5 members
	raw, _ = store.Get(livingRoomKey)
	json.Unmarshal(raw, &livingRoomGroup)

	json.Unmarshal(livingRoomGroup.Target, &targetQuery)
	members, _ = store.Query(targetQuery)

	if len(members) != 5 {
		t.Errorf("Expected 5 LivingRoom members after adding 2 more, got %d", len(members))
	}

	// Verify all 5 lights are in the results
	foundIds := make(map[string]bool)
	for _, m := range members {
		var entity domain.Entity
		json.Unmarshal(m.Data, &entity)
		foundIds[entity.ID] = true
	}

	for i := 1; i <= 5; i++ {
		id := "light" + string(rune('0'+i))
		if !foundIds[id] {
			t.Errorf("Expected %s to be in LivingRoom group, but it wasn't found", id)
		}
	}

	t.Logf("✓ Group updated correctly: LivingRoom now has %d members", len(members))
}

// TestGroupDiscovery_RemoveEntityRemovesFromGroup validates that when an entity
// is deleted, it is removed from the group on next discovery
func TestGroupDiscovery_RemoveEntityRemovesFromGroup(t *testing.T) {
	_, store, _ := env(t)

	// Step 1: Create 5 lights with Office label
	for i := 1; i <= 5; i++ {
		saveEntityWithLabels(t, store, "test", "dev1", "light"+string(rune('0'+i)), "light", "Light "+string(rune('0'+i)),
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Office"}})
	}

	// Step 2: Run discovery
	discoverGroups(store)

	// Verify Office group has 5 members
	officeKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "office"}
	raw, _ := store.Get(officeKey)
	var officeGroup domain.Entity
	json.Unmarshal(raw, &officeGroup)

	var targetQuery storage.Query
	json.Unmarshal(officeGroup.Target, &targetQuery)
	members, _ := store.Query(targetQuery)

	if len(members) != 5 {
		t.Errorf("Expected 5 Office members initially, got %d", len(members))
	}

	// Step 3: Delete 2 lights (light2 and light4)
	store.Delete(domain.EntityKey{Plugin: "test", DeviceID: "dev1", ID: "light2"})
	store.Delete(domain.EntityKey{Plugin: "test", DeviceID: "dev1", ID: "light4"})

	// Step 4: Run discovery again
	discoverGroups(store)

	// Step 5: Verify Office group now has 3 members
	raw, _ = store.Get(officeKey)
	json.Unmarshal(raw, &officeGroup)

	json.Unmarshal(officeGroup.Target, &targetQuery)
	members, _ = store.Query(targetQuery)

	if len(members) != 3 {
		t.Errorf("Expected 3 Office members after deleting 2, got %d", len(members))
	}

	// Verify the correct lights remain
	remainingIds := make(map[string]bool)
	for _, m := range members {
		var entity domain.Entity
		json.Unmarshal(m.Data, &entity)
		remainingIds[entity.ID] = true
	}

	expectedRemaining := []string{"light1", "light3", "light5"}
	for _, id := range expectedRemaining {
		if !remainingIds[id] {
			t.Errorf("Expected %s to remain in Office group, but it wasn't found", id)
		}
	}

	// Verify deleted lights are NOT in the group
	if remainingIds["light2"] {
		t.Error("light2 was deleted but still found in Office group")
	}
	if remainingIds["light4"] {
		t.Error("light4 was deleted but still found in Office group")
	}

	t.Logf("✓ Group membership updated correctly: Office now has %d members", len(members))
}

// TestGroupDiscovery_BackgroundDiscoveryNewGroup validates that the background
// discovery mechanism automatically creates new groups when entities with
// previously unseen PluginAutomation labels are added
func TestGroupDiscovery_BackgroundDiscoveryNewGroup(t *testing.T) {
	_, store, _ := env(t)

	// Step 1: Create initial lights with Basement label
	for i := 1; i <= 3; i++ {
		saveEntityWithLabels(t, store, "test", "dev1", "basement-light"+string(rune('0'+i)), "light", "Basement Light "+string(rune('0'+i)),
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Basement"}})
	}

	// Step 2: Run initial discovery - should only create Basement group
	discoverGroups(store)

	// Verify only Basement group exists
	groups, _ := store.Query(storage.Query{Pattern: app.PluginID + ".group.>"})
	if len(groups) != 1 {
		t.Errorf("Expected 1 group (Basement) initially, got %d", len(groups))
	}

	basementKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "basement"}
	if _, err := store.Get(basementKey); err != nil {
		t.Error("Basement group should exist")
	}

	// Step 3: Simulate time passing and new entities appearing
	// (in real scenario this would happen through the ticker)

	// Add new entities with Attic label
	for i := 1; i <= 2; i++ {
		saveEntityWithLabels(t, store, "test", "dev1", "attic-light"+string(rune('0'+i)), "light", "Attic Light "+string(rune('0'+i)),
			domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Attic"}})
	}

	// Step 4: Run discovery again (simulating background ticker)
	discoverGroups(store)

	// Step 5: Verify both Basement and Attic groups now exist
	groups, _ = store.Query(storage.Query{Pattern: app.PluginID + ".group.>"})
	if len(groups) != 2 {
		t.Errorf("Expected 2 groups (Basement and Attic) after background discovery, got %d", len(groups))
	}

	// Verify Attic group was created and has correct members
	atticKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "attic"}
	raw, err := store.Get(atticKey)
	if err != nil {
		t.Fatal("Attic group should have been created by background discovery")
	}

	var atticGroup domain.Entity
	json.Unmarshal(raw, &atticGroup)

	var targetQuery storage.Query
	json.Unmarshal(atticGroup.Target, &targetQuery)
	members, _ := store.Query(targetQuery)

	if len(members) != 2 {
		t.Errorf("Attic group should have 2 members, got %d", len(members))
	}

	// Verify Basement group still exists with 3 members
	raw, _ = store.Get(basementKey)
	var basementGroup domain.Entity
	json.Unmarshal(raw, &basementGroup)

	json.Unmarshal(basementGroup.Target, &targetQuery)
	members, _ = store.Query(targetQuery)

	if len(members) != 3 {
		t.Errorf("Basement group should still have 3 members, got %d", len(members))
	}

	t.Logf("✓ Background discovery created new Attic group with %d members, Basement still has %d", 2, 3)
}

// TestGroupDiscovery_MultipleDifferentGroups validates that different
// PluginAutomation labels create different groups with correct membership
func TestGroupDiscovery_MultipleDifferentGroups(t *testing.T) {
	_, store, _ := env(t)

	// Create entities with different PluginAutomation labels
	testData := []struct {
		id       string
		groups   []string
		expected []string // expected group membership
	}{
		{"light1", []string{"Upstairs"}, []string{"Upstairs"}},
		{"light2", []string{"Upstairs", "Bedroom"}, []string{"Upstairs", "Bedroom"}},
		{"light3", []string{"Upstairs", "Bedroom"}, []string{"Upstairs", "Bedroom"}},
		{"light4", []string{"Downstairs", "Kitchen"}, []string{"Downstairs", "Kitchen"}},
		{"light5", []string{"Downstairs", "Kitchen"}, []string{"Downstairs", "Kitchen"}},
		{"light6", []string{"Downstairs", "LivingRoom"}, []string{"Downstairs", "LivingRoom"}},
		{"light7", []string{"Outdoor", "Garage"}, []string{"Outdoor", "Garage"}},
		{"light8", nil, nil}, // No PluginAutomation label
	}

	for _, td := range testData {
		labels := map[string][]string{}
		if td.groups != nil {
			labels["PluginAutomation"] = td.groups
		}
		saveEntityWithLabels(t, store, "test", "dev1", td.id, "light", "Light "+td.id,
			domain.Light{Power: false}, labels)
	}

	// Run discovery
	discoverGroups(store)

	// Expected group memberships:
	// Upstairs: light1, light2, light3 (3 members)
	// Bedroom: light2, light3 (2 members)
	// Downstairs: light4, light5, light6 (3 members)
	// Kitchen: light4, light5 (2 members)
	// LivingRoom: light6 (1 member)
	// Outdoor: light7 (1 member)
	// Garage: light7 (1 member)
	expectedGroups := map[string]int{
		"upstairs":   3,
		"bedroom":    2,
		"downstairs": 3,
		"kitchen":    2,
		"livingroom": 1,
		"outdoor":    1,
		"garage":     1,
	}

	// Verify each group
	for groupName, expectedCount := range expectedGroups {
		groupID := app.NormalizeGroupID(groupName)
		groupKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: groupID}

		raw, err := store.Get(groupKey)
		if err != nil {
			t.Errorf("Group %s not found", groupName)
			continue
		}

		var group domain.Entity
		json.Unmarshal(raw, &group)

		// Execute target query
		var targetQuery storage.Query
		json.Unmarshal(group.Target, &targetQuery)
		members, _ := store.Query(targetQuery)

		if len(members) != expectedCount {
			t.Errorf("Group %s: expected %d members, got %d", groupName, expectedCount, len(members))
			for _, m := range members {
				var e domain.Entity
				json.Unmarshal(m.Data, &e)
				t.Logf("  Member: %s", e.ID)
			}
		}

		// Verify all members have the correct label
		for _, member := range members {
			var entity domain.Entity
			json.Unmarshal(member.Data, &entity)

			hasLabel := false
			if labels, ok := entity.Labels["PluginAutomation"]; ok {
				for _, label := range labels {
					if app.NormalizeGroupID(label) == groupID {
						hasLabel = true
						break
					}
				}
			}
			if !hasLabel {
				t.Errorf("Entity %s in group %s doesn't have the correct label", entity.ID, groupName)
			}
		}

		t.Logf("✓ Group %s: %d members (expected %d)", groupName, len(members), expectedCount)
	}

	// Verify total number of groups
	groups, _ := store.Query(storage.Query{Pattern: app.PluginID + ".group.>"})
	if len(groups) != len(expectedGroups) {
		t.Errorf("Expected %d total groups, got %d", len(expectedGroups), len(groups))
	}
}

// TestGroupDiscovery_LabelValuesWithSpaces validates that group names with spaces
// are properly normalized and the queries still work correctly
func TestGroupDiscovery_LabelValuesWithSpaces(t *testing.T) {
	_, store, _ := env(t)

	// Create lights with multi-word group names
	saveEntityWithLabels(t, store, "test", "dev1", "light1", "light", "Light 1",
		domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Master Bedroom"}})

	saveEntityWithLabels(t, store, "test", "dev1", "light2", "light", "Light 2",
		domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Master Bedroom", "Guest Bathroom"}})

	saveEntityWithLabels(t, store, "test", "dev1", "light3", "light", "Light 3",
		domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"Guest Bathroom", "Home Office"}})

	// Run discovery
	discoverGroups(store)

	// Verify Master Bedroom group (normalized to master-bedroom)
	masterBedroomKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "master-bedroom"}
	raw, err := store.Get(masterBedroomKey)
	if err != nil {
		t.Fatal("Master Bedroom group not found")
	}

	var masterBedroom domain.Entity
	json.Unmarshal(raw, &masterBedroom)

	if masterBedroom.Name != "Master Bedroom" {
		t.Errorf("Group name should be 'Master Bedroom', got %q", masterBedroom.Name)
	}

	var targetQuery storage.Query
	json.Unmarshal(masterBedroom.Target, &targetQuery)

	// The query value should be the ORIGINAL label (with space), not the normalized ID
	if len(targetQuery.Where) != 1 {
		t.Fatalf("Expected 1 where clause, got %d", len(targetQuery.Where))
	}

	if targetQuery.Where[0].Value != "Master Bedroom" {
		t.Errorf("Query value should be 'Master Bedroom', got %v", targetQuery.Where[0].Value)
	}

	// Execute and verify
	members, _ := store.Query(targetQuery)
	if len(members) != 2 {
		t.Errorf("Master Bedroom should have 2 members, got %d", len(members))
	}

	// Verify Guest Bathroom group
	guestBathroomKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "guest-bathroom"}
	raw, _ = store.Get(guestBathroomKey)
	var guestBathroom domain.Entity
	json.Unmarshal(raw, &guestBathroom)

	json.Unmarshal(guestBathroom.Target, &targetQuery)
	members, _ = store.Query(targetQuery)

	if len(members) != 2 {
		t.Errorf("Guest Bathroom should have 2 members, got %d", len(members))
	}

	// Verify Home Office group
	homeOfficeKey := domain.EntityKey{Plugin: app.PluginID, DeviceID: "group", ID: "home-office"}
	raw, _ = store.Get(homeOfficeKey)
	var homeOffice domain.Entity
	json.Unmarshal(raw, &homeOffice)

	json.Unmarshal(homeOffice.Target, &targetQuery)
	members, _ = store.Query(targetQuery)

	if len(members) != 1 {
		t.Errorf("Home Office should have 1 member, got %d", len(members))
	}

	t.Logf("✓ Space-separated group names work: Master Bedroom (2), Guest Bathroom (2), Home Office (1)")
}

// TestGroupDiscovery_SimulatedTicker validates the background discovery
// by simulating multiple discovery cycles
func TestGroupDiscovery_SimulatedTicker(t *testing.T) {
	_, store, _ := env(t)

	// Initial discovery
	discoverGroups(store)
	groups1, _ := store.Query(storage.Query{Pattern: app.PluginID + ".group.>"})

	// Add new group
	saveEntityWithLabels(t, store, "test", "dev1", "new-light", "light", "New Light",
		domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"NewGroup"}})

	// Simulate ticker firing
	time.Sleep(100 * time.Millisecond)
	discoverGroups(store)

	groups2, _ := store.Query(storage.Query{Pattern: app.PluginID + ".group.>"})
	if len(groups2) != len(groups1)+1 {
		t.Errorf("Expected %d groups after ticker, got %d", len(groups1)+1, len(groups2))
	}

	// Add another group
	saveEntityWithLabels(t, store, "test", "dev2", "another-light", "light", "Another Light",
		domain.Light{Power: false}, map[string][]string{"PluginAutomation": {"AnotherGroup"}})

	// Simulate another ticker cycle
	time.Sleep(100 * time.Millisecond)
	discoverGroups(store)

	groups3, _ := store.Query(storage.Query{Pattern: app.PluginID + ".group.>"})
	if len(groups3) != len(groups1)+2 {
		t.Errorf("Expected %d groups after second ticker, got %d", len(groups1)+2, len(groups3))
	}

	t.Logf("✓ Simulated ticker discovery works: started with %d, ended with %d groups", len(groups1), len(groups3))
}
