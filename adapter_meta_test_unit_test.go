package main

import (
	"encoding/json"
	"slices"
	"testing"

	light_panel "github.com/slidebolt/sdk-entities/light_panel"
	"github.com/slidebolt/sdk-entities/light_strip"
	"github.com/slidebolt/sdk-types"
)

// makeIntPtr is a test helper that returns a pointer to an int literal.
func makeIntPtr(v int) *int { return &v }

// makeMetaRaw encodes v as JSON and panics on error (test-only helper).
func makeMetaRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestBuildAutoGroupsWithMeta(t *testing.T) {
	tests := []struct {
		name  string
		input []types.Entity
		check func(t *testing.T, groups []types.Entity, err error)
	}{
		{
			name: "no meta infers domain from entity",
			input: []types.Entity{
				{
					ID:       "e1",
					DeviceID: "d1",
					PluginID: "plugin-wiz",
					Domain:   "light",
					Actions:  []string{"turn_on", "turn_off", "set_brightness"},
					Labels:   map[string][]string{"PluginAutomation": {"MyGroup"}},
				},
			},
			check: func(t *testing.T, groups []types.Entity, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(groups) != 1 {
					t.Fatalf("expected 1 group, got %d", len(groups))
				}
				g := groups[0]
				if g.Domain != "light" {
					t.Errorf("domain: got %q, want %q", g.Domain, "light")
				}
				if g.ID != "group-mygroup" {
					t.Errorf("id: got %q, want %q", g.ID, "group-mygroup")
				}
				if g.CommandQuery == nil {
					t.Fatal("expected CommandQuery to be set")
				}
				if vals := g.CommandQuery.Labels["PluginAutomation"]; len(vals) != 1 || vals[0] != "MyGroup" {
					t.Errorf("CommandQuery labels: %v", g.CommandQuery.Labels)
				}
			},
		},
		{
			name: "explicit domain override via meta",
			input: []types.Entity{
				{
					ID:       "e1",
					DeviceID: "d1",
					PluginID: "plugin-zwave",
					Domain:   "switch",
					Actions:  []string{"turn_on", "turn_off"},
					Labels:   map[string][]string{"PluginAutomation": {"LightGroup"}},
					Meta: map[string]json.RawMessage{
						"PluginAutomation:LightGroup": makeMetaRaw(map[string]any{"domain": "light"}),
					},
				},
			},
			check: func(t *testing.T, groups []types.Entity, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(groups) != 1 {
					t.Fatalf("expected 1 group, got %d", len(groups))
				}
				if groups[0].Domain != "light" {
					t.Errorf("domain: got %q, want %q", groups[0].Domain, "light")
				}
			},
		},
		{
			name: "light_strip via meta creates strip entity with sorted members",
			input: []types.Entity{
				{
					ID:       "led1",
					DeviceID: "d1",
					PluginID: "plugin-esphome",
					Domain:   "light_strip",
					Labels:   map[string][]string{"PluginAutomation": {"BasementLS"}},
					Meta: map[string]json.RawMessage{
						"PluginAutomation:BasementLS": makeMetaRaw(map[string]any{"domain": "light_strip", "index": 0}),
					},
				},
				{
					ID:       "led2",
					DeviceID: "d2",
					PluginID: "plugin-esphome",
					Domain:   "light_strip",
					Labels:   map[string][]string{"PluginAutomation": {"BasementLS"}},
					Meta: map[string]json.RawMessage{
						"PluginAutomation:BasementLS": makeMetaRaw(map[string]any{"domain": "light_strip", "index": 1}),
					},
				},
			},
			check: func(t *testing.T, groups []types.Entity, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(groups) != 1 {
					t.Fatalf("expected 1 group, got %d", len(groups))
				}
				g := groups[0]
				if g.Domain != "light_strip" {
					t.Errorf("domain: got %q, want %q", g.Domain, "light_strip")
				}
				if g.Meta == nil {
					t.Fatal("expected Meta to be set")
				}
				raw, ok := g.Meta["strip_members"]
				if !ok {
					t.Fatal("expected strip_members in Meta")
				}
				var members []stripMember
				if err := json.Unmarshal(raw, &members); err != nil {
					t.Fatalf("unmarshal strip_members: %v", err)
				}
				if len(members) != 2 {
					t.Fatalf("expected 2 members, got %d", len(members))
				}
				if members[0].Index != 0 || members[0].EntityID != "led1" {
					t.Errorf("member[0]: %+v", members[0])
				}
				if members[1].Index != 1 || members[1].EntityID != "led2" {
					t.Errorf("member[1]: %+v", members[1])
				}
				// Strip entity must have CommandQuery (fan-out for broadcast commands)
				// and CommandFilter that excludes set_segment so it falls through to OnCommand.
				if g.CommandQuery == nil {
					t.Fatal("strip entity: expected CommandQuery to be set")
				}
				if vals := g.CommandQuery.Labels["PluginAutomation"]; len(vals) != 1 || vals[0] != "BasementLS" {
					t.Errorf("strip entity CommandQuery labels: %v", g.CommandQuery.Labels)
				}
				wantFilter := light_strip.BroadcastActions()
				got := make([]string, len(g.CommandFilter))
				copy(got, g.CommandFilter)
				slices.Sort(got)
				want := make([]string, len(wantFilter))
				copy(want, wantFilter)
				slices.Sort(want)
				if !slices.Equal(got, want) {
					t.Errorf("strip entity CommandFilter: got %v, want %v", got, want)
				}
				if slices.Contains(g.CommandFilter, light_strip.ActionSetSegment) {
					t.Error("strip entity CommandFilter must NOT contain set_segment")
				}
			},
		},
		{
			name: "mixed group and strip from same entity creates two virtual entities",
			input: []types.Entity{
				{
					ID:       "e1",
					DeviceID: "d1",
					PluginID: "plugin-esphome",
					Domain:   "light",
					Actions:  []string{"turn_on", "turn_off"},
					Labels:   map[string][]string{"PluginAutomation": {"BasementG", "BasementLS"}},
					Meta: map[string]json.RawMessage{
						// BasementG has no meta → broadcast
						// BasementLS has index → strip
						"PluginAutomation:BasementLS": makeMetaRaw(map[string]any{"domain": "light_strip", "index": 0}),
					},
				},
			},
			check: func(t *testing.T, groups []types.Entity, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(groups) != 2 {
					t.Fatalf("expected 2 groups, got %d: %v", len(groups), groupIDs(groups))
				}
				byID := indexByID(groups)
				broadcast, ok := byID["group-basementg"]
				if !ok {
					t.Fatal("expected group-basementg")
				}
				if broadcast.Domain != "light" {
					t.Errorf("broadcast domain: got %q, want %q", broadcast.Domain, "light")
				}
				strip, ok := byID["group-basementls"]
				if !ok {
					t.Fatal("expected group-basementls")
				}
				if strip.Domain != "light_strip" {
					t.Errorf("strip domain: got %q, want %q", strip.Domain, "light_strip")
				}
				if strip.Meta == nil || strip.Meta["strip_members"] == nil {
					t.Fatal("expected strip_members on strip group")
				}
			},
		},
		{
			name: "overlapping strips produce independent member lists",
			input: []types.Entity{
				// Basement strip: indexes 0, 1, 2
				{
					ID: "led-a", DeviceID: "da", PluginID: "p1", Domain: "light_strip",
					Labels: map[string][]string{"PluginAutomation": {"BasementS", "HallwayS"}},
					Meta: map[string]json.RawMessage{
						"PluginAutomation:BasementS": makeMetaRaw(map[string]any{"domain": "light_strip", "index": 0}),
						"PluginAutomation:HallwayS":  makeMetaRaw(map[string]any{"domain": "light_strip", "index": 0}),
					},
				},
				{
					ID: "led-b", DeviceID: "db", PluginID: "p1", Domain: "light_strip",
					Labels: map[string][]string{"PluginAutomation": {"BasementS", "HallwayS"}},
					Meta: map[string]json.RawMessage{
						"PluginAutomation:BasementS": makeMetaRaw(map[string]any{"domain": "light_strip", "index": 1}),
						"PluginAutomation:HallwayS":  makeMetaRaw(map[string]any{"domain": "light_strip", "index": 1}),
					},
				},
				{
					ID: "led-c", DeviceID: "dc", PluginID: "p1", Domain: "light_strip",
					Labels: map[string][]string{"PluginAutomation": {"BasementS"}},
					Meta: map[string]json.RawMessage{
						"PluginAutomation:BasementS": makeMetaRaw(map[string]any{"domain": "light_strip", "index": 2}),
					},
				},
			},
			check: func(t *testing.T, groups []types.Entity, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(groups) != 2 {
					t.Fatalf("expected 2 groups, got %d", len(groups))
				}
				byID := indexByID(groups)

				basement := byID["group-basements"]
				if basement.ID == "" {
					t.Fatal("expected group-basements")
				}
				bMembers := mustUnmarshalMembers(t, basement)
				if len(bMembers) != 3 {
					t.Errorf("basement: expected 3 members, got %d", len(bMembers))
				}

				hallway := byID["group-hallways"]
				if hallway.ID == "" {
					t.Fatal("expected group-hallways")
				}
				hMembers := mustUnmarshalMembers(t, hallway)
				if len(hMembers) != 2 {
					t.Errorf("hallway: expected 2 members, got %d", len(hMembers))
				}
			},
		},
		{
			name: "strip members sorted by index regardless of insertion order",
			input: []types.Entity{
				{
					ID: "led-2", DeviceID: "d2", PluginID: "p1", Domain: "light_strip",
					Labels: map[string][]string{"PluginAutomation": {"MyStrip"}},
					Meta: map[string]json.RawMessage{
						"PluginAutomation:MyStrip": makeMetaRaw(map[string]any{"domain": "light_strip", "index": 2}),
					},
				},
				{
					ID: "led-0", DeviceID: "d0", PluginID: "p1", Domain: "light_strip",
					Labels: map[string][]string{"PluginAutomation": {"MyStrip"}},
					Meta: map[string]json.RawMessage{
						"PluginAutomation:MyStrip": makeMetaRaw(map[string]any{"domain": "light_strip", "index": 0}),
					},
				},
				{
					ID: "led-1", DeviceID: "d1", PluginID: "p1", Domain: "light_strip",
					Labels: map[string][]string{"PluginAutomation": {"MyStrip"}},
					Meta: map[string]json.RawMessage{
						"PluginAutomation:MyStrip": makeMetaRaw(map[string]any{"domain": "light_strip", "index": 1}),
					},
				},
			},
			check: func(t *testing.T, groups []types.Entity, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(groups) != 1 {
					t.Fatalf("expected 1 group, got %d", len(groups))
				}
				members := mustUnmarshalMembers(t, groups[0])
				if len(members) != 3 {
					t.Fatalf("expected 3 members, got %d", len(members))
				}
				for i, m := range members {
					if m.Index != i {
						t.Errorf("members[%d].Index = %d, want %d", i, m.Index, i)
					}
				}
				if members[0].EntityID != "led-0" || members[1].EntityID != "led-1" || members[2].EntityID != "led-2" {
					t.Errorf("unexpected member order: %+v", members)
				}
			},
		},
		{
			name: "light_panel via meta creates panel entity with sorted members",
			input: []types.Entity{
				{
					ID:       "panel-b",
					DeviceID: "d2",
					PluginID: "plugin-esphome",
					Domain:   "light",
					Labels:   map[string][]string{"PluginAutomation": {"OfficePanel"}},
					Meta: map[string]json.RawMessage{
						"PluginAutomation:OfficePanel": makeMetaRaw(map[string]any{"domain": "light_panel", "panel_id": 20}),
					},
				},
				{
					ID:       "panel-a",
					DeviceID: "d1",
					PluginID: "plugin-esphome",
					Domain:   "light",
					Labels:   map[string][]string{"PluginAutomation": {"OfficePanel"}},
					Meta: map[string]json.RawMessage{
						"PluginAutomation:OfficePanel": makeMetaRaw(map[string]any{"domain": "light_panel", "panel_id": 10}),
					},
				},
			},
			check: func(t *testing.T, groups []types.Entity, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(groups) != 1 {
					t.Fatalf("expected 1 group, got %d", len(groups))
				}
				g := groups[0]
				if g.Domain != light_panel.Type {
					t.Errorf("domain: got %q, want %q", g.Domain, light_panel.Type)
				}
				raw, ok := g.Meta["panel_members"]
				if !ok {
					t.Fatal("expected panel_members in Meta")
				}
				var members []panelMember
				if err := json.Unmarshal(raw, &members); err != nil {
					t.Fatalf("unmarshal panel_members: %v", err)
				}
				if len(members) != 2 {
					t.Fatalf("expected 2 members, got %d", len(members))
				}
				// Members sorted by panel_id: 10 before 20.
				if members[0].PanelID != 10 || members[0].EntityID != "panel-a" {
					t.Errorf("member[0]: %+v", members[0])
				}
				if members[1].PanelID != 20 || members[1].EntityID != "panel-b" {
					t.Errorf("member[1]: %+v", members[1])
				}
				if g.CommandQuery == nil {
					t.Fatal("panel entity: expected CommandQuery to be set")
				}
				if vals := g.CommandQuery.Labels["PluginAutomation"]; len(vals) != 1 || vals[0] != "OfficePanel" {
					t.Errorf("panel entity CommandQuery labels: %v", g.CommandQuery.Labels)
				}
				wantFilter := light_panel.BroadcastActions()
				got := make([]string, len(g.CommandFilter))
				copy(got, g.CommandFilter)
				slices.Sort(got)
				want := make([]string, len(wantFilter))
				copy(want, wantFilter)
				slices.Sort(want)
				if !slices.Equal(got, want) {
					t.Errorf("panel entity CommandFilter: got %v, want %v", got, want)
				}
				if slices.Contains(g.CommandFilter, light_panel.ActionSetPanel) {
					t.Error("panel entity CommandFilter must NOT contain set_panel")
				}
			},
		},
		{
			name: "domain override without index creates broadcast group",
			input: []types.Entity{
				{
					ID:       "e1",
					DeviceID: "d1",
					PluginID: "plugin-esphome",
					Domain:   "switch",
					Actions:  []string{"turn_on", "turn_off"},
					Labels:   map[string][]string{"PluginAutomation": {"LedGroup"}},
					Meta: map[string]json.RawMessage{
						// domain set but no index → broadcast with overridden domain
						"PluginAutomation:LedGroup": makeMetaRaw(map[string]any{"domain": "light_strip"}),
					},
				},
			},
			check: func(t *testing.T, groups []types.Entity, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(groups) != 1 {
					t.Fatalf("expected 1 group, got %d", len(groups))
				}
				g := groups[0]
				if g.Domain != "light_strip" {
					t.Errorf("domain: got %q, want %q", g.Domain, "light_strip")
				}
				// No positional meta → no strip_members
				if g.Meta != nil && g.Meta["strip_members"] != nil {
					t.Error("unexpected strip_members on broadcast group")
				}
				if g.CommandQuery == nil {
					t.Fatal("expected CommandQuery to be set")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			groups, err := buildAutoGroups(tc.input, nil)
			tc.check(t, groups, err)
		})
	}
}

// --- helpers ---

func groupIDs(groups []types.Entity) []string {
	ids := make([]string, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
	}
	return ids
}

func indexByID(groups []types.Entity) map[string]types.Entity {
	m := make(map[string]types.Entity, len(groups))
	for _, g := range groups {
		m[g.ID] = g
	}
	return m
}

func mustUnmarshalMembers(t *testing.T, entity types.Entity) []stripMember {
	t.Helper()
	raw, ok := entity.Meta["strip_members"]
	if !ok {
		t.Fatalf("entity %s has no strip_members", entity.ID)
	}
	var members []stripMember
	if err := json.Unmarshal(raw, &members); err != nil {
		t.Fatalf("unmarshal strip_members: %v", err)
	}
	return members
}
