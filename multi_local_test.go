//go:build test_multi_local

// Real IoT integration tests for plugin-automation.
// These require live devices on the local network and are NEVER run in CI.
//
// Run:
//
//	cd raw/plugin-automation
//	go test -tags test_multi_local -v -count=1 ./...
//
// With trace logging:
//
//	LOG_LEVEL=trace go test -tags test_multi_local -v -count=1 ./...
//
// How this works:
//
//	TestMain starts plugin-automation + plugin-wiz + plugin-esphome + plugin-zigbee2mqtt.
//	Each test calls SkipUnlessPlugin for the hardware it needs, so missing
//	devices produce a clean SKIP rather than a FAIL.
//
//	Tests discover real device IDs dynamically (WaitFor polling the gateway)
//	then create virtual groups using those real IDs and issue commands.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
	integrationtesting "github.com/slidebolt/sdk-integration-testing"
	"github.com/slidebolt/sdk-types"
)

// TestMain boots one shared stack for all test_multi_local tests.
// Includes real IoT plugins; each test skips if its hardware is absent.
func TestMain(m *testing.M) {
	integrationtesting.RunAll(m, []integrationtesting.PluginSpec{
		{Module: "github.com/slidebolt/plugin-automation", Dir: "."},
		{Module: "github.com/slidebolt/plugin-wiz", Dir: "../plugin-wiz"},
		{Module: "github.com/slidebolt/plugin-esphome", Dir: "../plugin-esphome", EnvFiles: []string{".env.local"}},
		{Module: "github.com/slidebolt/plugin-zigbee2mqtt", Dir: "../plugin-zigbee2mqtt", EnvFiles: []string{".env.local"}},
	})
}

// TestMultiLocal_WizGroupBroadcast discovers real WiZ bulbs, creates a virtual
// broadcast light group from two of them, sends turn_on, and asserts the
// command is received on NATS for both physical entities.
func TestMultiLocal_WizGroupBroadcast(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")
	s.SkipUnlessPlugin(t, "plugin-wiz")

	// Wait for at least 2 WiZ devices to appear.
	var wizDevices []map[string]any
	found := s.WaitFor(30*time.Second, func() bool {
		_ = s.GetJSON("/api/plugins/plugin-wiz/devices", &wizDevices)
		return len(wizDevices) >= 2
	})
	if !found {
		t.Skipf("need at least 2 WiZ devices, found %d — skipping", len(wizDevices))
	}

	// Pick the first two devices and their first light entity each.
	type leaf struct{ plugin, device, entity string }
	var leaves []leaf
	for _, dev := range wizDevices[:2] {
		devID, _ := dev["id"].(string)
		var entities []map[string]any
		if err := s.GetJSON(fmt.Sprintf("/api/plugins/plugin-wiz/devices/%s/entities", devID), &entities); err != nil || len(entities) == 0 {
			continue
		}
		for _, e := range entities {
			if domain, _ := e["domain"].(string); domain == "light" {
				leaves = append(leaves, leaf{"plugin-wiz", devID, e["id"].(string)})
				break
			}
		}
	}
	if len(leaves) < 2 {
		t.Skipf("could not find 2 light entities on WiZ devices — skipping")
	}
	t.Logf("using leaves: %v", leaves)

	// Label both entities for a new virtual group via PATCH labels.
	groupName := fmt.Sprintf("LocalWizGroup%d", time.Now().UnixNano()%10000)
	for _, l := range leaves {
		path := fmt.Sprintf("/api/plugins/%s/devices/%s/entities/%s/labels", l.plugin, l.device, l.entity)
		if err := s.PatchJSON(path, map[string]any{
			"PluginAutomation": []string{groupName},
		}, nil); err != nil {
			t.Fatalf("patch labels on %s: %v", l.entity, err)
		}
	}
	t.Logf("labeled 2 WiZ lights with group %q", groupName)

	// Wait for virtual group entity to appear.
	groupEntityID := "group-" + lowercasedLocal(groupName)
	ok := s.WaitFor(10*time.Second, func() bool {
		var entities []map[string]any
		_ = s.GetJSON("/api/plugins/plugin-automation/devices/groups/entities", &entities)
		for _, e := range entities {
			if id, _ := e["id"].(string); id == groupEntityID {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("virtual group %q did not appear after 10s", groupEntityID)
	}
	t.Logf("virtual group %q created", groupEntityID)

	// Subscribe to NATS to capture forwarded commands.
	nc, err := nats.Connect(s.NATSURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer nc.Close()

	received := map[string]bool{}
	ch := make(chan types.Command, 16)
	sub, _ := nc.Subscribe("slidebolt.rpc.plugin-wiz.command", func(msg *nats.Msg) {
		var cmd types.Command
		if json.Unmarshal(msg.Data, &cmd) == nil {
			ch <- cmd
		}
	})
	defer sub.Unsubscribe()

	// Send turn_on to the virtual group — both leaves should receive it.
	cmdPath := fmt.Sprintf("/api/plugins/plugin-automation/devices/groups/entities/%s/commands", groupEntityID)
	if err := s.PostJSON(cmdPath, map[string]any{"type": "turn_on"}, nil); err != nil {
		t.Fatalf("send turn_on: %v", err)
	}
	t.Log("turn_on sent to virtual group")

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for len(received) < 2 {
		select {
		case cmd := <-ch:
			received[cmd.EntityID] = true
		case <-deadline.C:
			t.Fatalf("timed out: only %d/%d leaves received turn_on (got: %v)", len(received), 2, received)
		}
	}
	t.Logf("both leaves received turn_on: %v", received)
}

// TestMultiLocal_EspHomeLightStripVirtualization discovers real ESPHome wafer
// lights labeled for a basement group, creates a virtual light_strip from the
// first 4, and sends a set_segment command to verify positional routing.
func TestMultiLocal_EspHomeLightStripVirtualization(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")
	s.SkipUnlessPlugin(t, "plugin-esphome")

	// Wait for at least 4 converged ESPHome light entities.
	var espDevices []map[string]any
	type leaf struct{ device, entity string }
	var leaves []leaf
	found := s.WaitFor(20*time.Second, func() bool {
		_ = s.GetJSON("/api/plugins/plugin-esphome/devices", &espDevices)
		leaves = leaves[:0]
		for _, dev := range espDevices {
			if len(leaves) >= 4 {
				break
			}
			devID, _ := dev["id"].(string)
			if devID == "" || devID == "plugin-esphome" {
				continue
			}
			var entities []map[string]any
			_ = s.GetJSON(fmt.Sprintf("/api/plugins/plugin-esphome/devices/%s/entities", devID), &entities)
			for _, e := range entities {
				if domain, _ := e["domain"].(string); domain == "light" {
					entityID, _ := e["id"].(string)
					leaves = append(leaves, leaf{devID, entityID})
					break
				}
			}
		}
		return len(leaves) >= 4
	})
	if !found {
		t.Skipf("need at least 4 ESPHome light entities, found %d devices", len(espDevices))
	}
	t.Logf("using ESPHome leaves: %+v", leaves)

	groupName := fmt.Sprintf("LocalEspStrip%d", time.Now().UnixNano()%10000)

	// Label each leaf with its positional meta.
	for i, l := range leaves {
		metaBlob, _ := json.Marshal(map[string]any{"domain": "light_strip", "index": i})
		labelsPath := fmt.Sprintf("/api/plugins/plugin-esphome/devices/%s/entities/%s/labels", l.device, l.entity)
		metaPath := fmt.Sprintf("/api/plugins/plugin-esphome/devices/%s/entities/%s/meta", l.device, l.entity)
		if err := s.PatchJSON(labelsPath, map[string]any{"labels": map[string][]string{"PluginAutomation": {groupName}}}, nil); err != nil {
			t.Fatalf("patch labels on %s/%s: %v", l.device, l.entity, err)
		}
		if err := s.PatchJSON(metaPath, map[string]json.RawMessage{
			"PluginAutomation:" + groupName: json.RawMessage(metaBlob),
		}, nil); err != nil {
			t.Fatalf("patch meta on %s/%s: %v", l.device, l.entity, err)
		}
	}
	t.Logf("labeled %d ESPHome lights as strip %q", len(leaves), groupName)

	// Wait for the light_strip virtual entity.
	stripID := "group-" + lowercasedLocal(groupName)
	ok := s.WaitFor(10*time.Second, func() bool {
		var entities []map[string]any
		_ = s.GetJSON("/api/plugins/plugin-automation/devices/groups/entities", &entities)
		for _, e := range entities {
			if id, _ := e["id"].(string); id == stripID {
				if domain, _ := e["domain"].(string); domain == "light_strip" {
					return true
				}
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("light_strip entity %q did not appear after 10s", stripID)
	}
	t.Logf("virtual light_strip %q created", stripID)

	// Subscribe to NATS to capture translated set_rgb.
	nc, err := nats.Connect(s.NATSURL())
	if err != nil {
		t.Fatalf("nats: %v", err)
	}
	defer nc.Close()

	cmdCh := make(chan types.Command, 8)
	sub, _ := nc.Subscribe("slidebolt.rpc.plugin-esphome.command", func(msg *nats.Msg) {
		var cmd types.Command
		if json.Unmarshal(msg.Data, &cmd) == nil {
			cmdCh <- cmd
		}
	})
	defer sub.Unsubscribe()

	// Send set_segment targeting index 2.
	cmdPath := fmt.Sprintf("/api/plugins/plugin-automation/devices/groups/entities/%s/commands", stripID)
	if err := s.PostJSON(cmdPath, map[string]any{
		"type":    "set_segment",
		"segment": map[string]any{"index": 2, "rgb": []int{128, 0, 255}},
	}, nil); err != nil {
		t.Fatalf("send set_segment: %v", err)
	}

	select {
	case got := <-cmdCh:
		if got.EntityID != leaves[2].entity {
			t.Errorf("expected entity %q (index 2), got %q", leaves[2].entity, got.EntityID)
		}
		var p struct {
			Type string `json:"type"`
			RGB  []int  `json:"rgb"`
		}
		if json.Unmarshal(got.Payload, &p) == nil {
			t.Logf("dispatched: type=%s rgb=%v entity=%s", p.Type, p.RGB, got.EntityID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for set_rgb on NATS")
	}
}

// TestMultiLocal_ListEspHomeDevices proves the shared local stack can see real
// ESPHome devices by listing them through the gateway and logging their IDs.
func TestMultiLocal_ListEspHomeDevices(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")
	s.SkipUnlessPlugin(t, "plugin-esphome")

	var devices []map[string]any
	var discovered []map[string]any
	found := s.WaitFor(20*time.Second, func() bool {
		_ = s.GetJSON("/api/plugins/plugin-esphome/devices", &devices)
		discovered = discovered[:0]
		for _, d := range devices {
			id, _ := d["id"].(string)
			if id == "" || id == "plugin-esphome" {
				continue
			}
			discovered = append(discovered, d)
		}
		return len(discovered) > 0
	})
	if !found {
		var ids []string
		for _, d := range devices {
			id, _ := d["id"].(string)
			ids = append(ids, id)
		}
		t.Skipf("no real ESPHome devices found within 20s; visible device ids=%v", ids)
	}

	t.Logf("discovered %d real ESPHome device(s)", len(discovered))
	for _, d := range discovered {
		id, _ := d["id"].(string)
		name, _ := d["name"].(string)
		t.Logf("device id=%s name=%s", id, name)
	}
}

// TestMultiLocal_EspHomeEdisonLightStrip creates a virtual light_strip from
// real ESPHome devices whose IDs contain "edison" and verifies segment routing.
func TestMultiLocal_EspHomeEdisonLightStrip(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")
	s.SkipUnlessPlugin(t, "plugin-esphome")

	var devices []map[string]any
	type leaf struct{ device, entity string }
	var leaves []leaf
	found := s.WaitFor(20*time.Second, func() bool {
		_ = s.GetJSON("/api/plugins/plugin-esphome/devices", &devices)
		leaves = leaves[:0]
		for _, d := range devices {
			id, _ := d["id"].(string)
			if id == "" || id == "plugin-esphome" || !strings.Contains(strings.ToLower(id), "edison") {
				continue
			}
			var entities []map[string]any
			_ = s.GetJSON(fmt.Sprintf("/api/plugins/plugin-esphome/devices/%s/entities", id), &entities)
			for _, e := range entities {
				if domain, _ := e["domain"].(string); domain == "light" {
					entityID, _ := e["id"].(string)
					leaves = append(leaves, leaf{id, entityID})
					break
				}
			}
		}
		return len(leaves) >= 4
	})
	if !found {
		var ids []string
		for _, d := range devices {
			id, _ := d["id"].(string)
			if id != "" && id != "plugin-esphome" {
				ids = append(ids, id)
			}
		}
		t.Skipf("need at least 4 real ESPHome edison light entities; visible device ids=%v", ids)
	}
	t.Logf("using edison leaves: %+v", leaves)

	groupName := fmt.Sprintf("LocalEdisonStrip%d", time.Now().UnixNano()%10000)
	for i, l := range leaves {
		metaBlob, _ := json.Marshal(map[string]any{"domain": "light_strip", "index": i})
		labelsPath := fmt.Sprintf("/api/plugins/plugin-esphome/devices/%s/entities/%s/labels", l.device, l.entity)
		metaPath := fmt.Sprintf("/api/plugins/plugin-esphome/devices/%s/entities/%s/meta", l.device, l.entity)
		if err := s.PatchJSON(labelsPath, map[string]any{"labels": map[string][]string{"PluginAutomation": {groupName}}}, nil); err != nil {
			t.Fatalf("patch labels on %s/%s: %v", l.device, l.entity, err)
		}
		if err := s.PatchJSON(metaPath, map[string]json.RawMessage{
			"PluginAutomation:" + groupName: json.RawMessage(metaBlob),
		}, nil); err != nil {
			t.Fatalf("patch meta on %s/%s: %v", l.device, l.entity, err)
		}
	}
	t.Logf("labeled %d edison lights as strip %q", len(leaves), groupName)

	stripID := "group-" + lowercasedLocal(groupName)
	ok := s.WaitFor(10*time.Second, func() bool {
		var entities []map[string]any
		_ = s.GetJSON("/api/plugins/plugin-automation/devices/groups/entities", &entities)
		for _, e := range entities {
			if id, _ := e["id"].(string); id == stripID {
				if domain, _ := e["domain"].(string); domain == "light_strip" {
					return true
				}
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("edison virtual light_strip %q did not appear after 10s", stripID)
	}
	t.Logf("virtual edison light_strip %q created", stripID)

	nc, err := nats.Connect(s.NATSURL())
	if err != nil {
		t.Fatalf("nats: %v", err)
	}
	defer nc.Close()

	cmdCh := make(chan types.Command, 8)
	sub, _ := nc.Subscribe("slidebolt.rpc.plugin-esphome.command", func(msg *nats.Msg) {
		var cmd types.Command
		if json.Unmarshal(msg.Data, &cmd) == nil {
			cmdCh <- cmd
		}
	})
	defer sub.Unsubscribe()

	cmdPath := fmt.Sprintf("/api/plugins/plugin-automation/devices/groups/entities/%s/commands", stripID)
	if err := s.PostJSON(cmdPath, map[string]any{
		"type":    "set_segment",
		"segment": map[string]any{"index": 2, "rgb": []int{64, 128, 255}},
	}, nil); err != nil {
		t.Fatalf("send set_segment: %v", err)
	}

	select {
	case got := <-cmdCh:
		if got.EntityID != leaves[2].entity {
			t.Fatalf("expected routed entity %q from device %q, got %q", leaves[2].entity, leaves[2].device, got.EntityID)
		}
		t.Logf("set_segment routed to edison device=%s entity=%s", leaves[2].device, got.EntityID)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for set_segment on plugin-esphome command bus")
	}
}

func TestMultiLocal_DebugEspHomeEdisonEntities(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")
	s.SkipUnlessPlugin(t, "plugin-esphome")

	var devices []map[string]any
	found := s.WaitFor(20*time.Second, func() bool {
		_ = s.GetJSON("/api/plugins/plugin-esphome/devices", &devices)
		for _, d := range devices {
			id, _ := d["id"].(string)
			if id != "" && id != "plugin-esphome" && strings.Contains(strings.ToLower(id), "edison") {
				return true
			}
		}
		return false
	})
	if !found {
		t.Skip("no ESPHome edison devices found within 20s")
	}

	s.WaitFor(30*time.Second, func() bool {
		for _, dev := range devices {
			devID, _ := dev["id"].(string)
			if devID == "" || devID == "plugin-esphome" || !strings.Contains(strings.ToLower(devID), "edison") {
				continue
			}
			var entities []map[string]any
			if err := s.GetJSON(fmt.Sprintf("/api/plugins/plugin-esphome/devices/%s/entities", devID), &entities); err == nil && len(entities) > 0 {
				return true
			}
		}
		return false
	})

	for _, dev := range devices {
		devID, _ := dev["id"].(string)
		if devID == "" || devID == "plugin-esphome" || !strings.Contains(strings.ToLower(devID), "edison") {
			continue
		}
		var entities []map[string]any
		if err := s.GetJSON(fmt.Sprintf("/api/plugins/plugin-esphome/devices/%s/entities", devID), &entities); err != nil {
			t.Logf("device=%s entities error: %v", devID, err)
			continue
		}
		t.Logf("device=%s entity_count=%d", devID, len(entities))
		for _, e := range entities {
			id, _ := e["id"].(string)
			domain, _ := e["domain"].(string)
			localName, _ := e["local_name"].(string)
			t.Logf("device=%s entity id=%s domain=%s local_name=%s raw=%v", devID, id, domain, localName, e)
		}
	}
}

func TestMultiLocal_EspHomeEdisonStripViaAPI(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")
	s.SkipUnlessPlugin(t, "plugin-esphome")

	type leaf struct{ device, entity string }
	var devices []map[string]any
	var leaves []leaf
	found := s.WaitFor(20*time.Second, func() bool {
		_ = s.GetJSON("/api/plugins/plugin-esphome/devices", &devices)
		leaves = leaves[:0]
		for _, d := range devices {
			id, _ := d["id"].(string)
			if id == "" || id == "plugin-esphome" || !strings.Contains(strings.ToLower(id), "edison") {
				continue
			}
			var entities []map[string]any
			_ = s.GetJSON(fmt.Sprintf("/api/plugins/plugin-esphome/devices/%s/entities", id), &entities)
			for _, e := range entities {
				if domain, _ := e["domain"].(string); domain == "light" {
					entityID, _ := e["id"].(string)
					leaves = append(leaves, leaf{id, entityID})
					break
				}
			}
		}
		return len(leaves) >= 4
	})
	if !found {
		t.Skip("need at least 4 Edison ESPHome light entities")
	}
	t.Logf("selected Edison leaves: %+v", leaves)

	groupName := fmt.Sprintf("ApiProofEdisonStrip%d", time.Now().UnixNano()%10000)
	for i, l := range leaves[:4] {
		metaBlob, _ := json.Marshal(map[string]any{"domain": "light_strip", "index": i})
		labelsPath := fmt.Sprintf("/api/plugins/plugin-esphome/devices/%s/entities/%s/labels", l.device, l.entity)
		labelsBody := map[string]any{"labels": map[string][]string{"PluginAutomation": {groupName}}}
		t.Logf("PATCH %s body=%v", labelsPath, labelsBody)
		if err := s.PatchJSON(labelsPath, labelsBody, nil); err != nil {
			t.Fatalf("patch labels: %v", err)
		}

		metaPath := fmt.Sprintf("/api/plugins/plugin-esphome/devices/%s/entities/%s/meta", l.device, l.entity)
		metaBody := map[string]json.RawMessage{"PluginAutomation:" + groupName: json.RawMessage(metaBlob)}
		t.Logf("PATCH %s body=%s", metaPath, string(metaBlob))
		if err := s.PatchJSON(metaPath, metaBody, nil); err != nil {
			t.Fatalf("patch meta: %v", err)
		}
	}

	stripID := "group-" + lowercasedLocal(groupName)
	ok := s.WaitFor(10*time.Second, func() bool {
		var entities []map[string]any
		_ = s.GetJSON("/api/plugins/plugin-automation/devices/groups/entities", &entities)
		for _, e := range entities {
			if id, _ := e["id"].(string); id == stripID {
				if domain, _ := e["domain"].(string); domain == "light_strip" {
					return true
				}
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("virtual strip %q not created", stripID)
	}
	t.Logf("GET /api/plugins/plugin-automation/devices/groups/entities -> found %s", stripID)

	nc, err := nats.Connect(s.NATSURL())
	if err != nil {
		t.Fatalf("nats: %v", err)
	}
	defer nc.Close()

	cmdCh := make(chan types.Command, 8)
	sub, _ := nc.Subscribe("slidebolt.rpc.plugin-esphome.command", func(msg *nats.Msg) {
		var cmd types.Command
		if json.Unmarshal(msg.Data, &cmd) == nil {
			cmdCh <- cmd
		}
	})
	defer sub.Unsubscribe()

	cmdPath := fmt.Sprintf("/api/plugins/plugin-automation/devices/groups/entities/%s/commands", stripID)
	cmdBody := map[string]any{
		"type":    "set_segment",
		"segment": map[string]any{"index": 2, "rgb": []int{255, 64, 16}},
	}
	t.Logf("POST %s body=%v", cmdPath, cmdBody)
	if err := s.PostJSON(cmdPath, cmdBody, nil); err != nil {
		t.Fatalf("post command: %v", err)
	}

	select {
	case got := <-cmdCh:
		if got.EntityID != leaves[2].entity {
			t.Fatalf("expected routed entity %q, got %q", leaves[2].entity, got.EntityID)
		}
		t.Logf("command routed to ESPHome entity=%s on device=%s", got.EntityID, leaves[2].device)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for routed ESPHome command")
	}
}

func TestMultiLocal_MainLBAndWizAsOneGroupViaAPI(t *testing.T) {
	s := integrationtesting.GetSuite(t)
	s.RequirePlugin("plugin-automation")
	if !s.WaitFor(60*time.Second, func() bool {
		plugins, err := s.Plugins()
		if err != nil {
			return false
		}
		_, ok := plugins["plugin-zigbee2mqtt"]
		return ok
	}) {
		t.Skip("plugin-zigbee2mqtt did not register within 60s")
	}
	if !s.WaitFor(60*time.Second, func() bool {
		plugins, err := s.Plugins()
		if err != nil {
			return false
		}
		_, ok := plugins["plugin-wiz"]
		return ok
	}) {
		t.Skip("plugin-wiz did not register within 60s")
	}

	type leaf struct {
		plugin string
		device string
		entity string
	}

	var zigbeeLeaves []leaf
	zigbeeReady := s.WaitFor(30*time.Second, func() bool {
		var devices []map[string]any
		_ = s.GetJSON("/api/plugins/plugin-zigbee2mqtt/devices", &devices)
		zigbeeLeaves = zigbeeLeaves[:0]
		for _, d := range devices {
			devID, _ := d["id"].(string)
			if devID == "" || devID == "plugin-zigbee2mqtt" {
				continue
			}
			sourceName, _ := d["source_name"].(string)
			if !strings.HasPrefix(strings.TrimSpace(sourceName), "Main_LB_") {
				continue
			}
			var entities []map[string]any
			_ = s.GetJSON(fmt.Sprintf("/api/plugins/plugin-zigbee2mqtt/devices/%s/entities", devID), &entities)
			for _, e := range entities {
				if domain, _ := e["domain"].(string); domain == "light" {
					entityID, _ := e["id"].(string)
					zigbeeLeaves = append(zigbeeLeaves, leaf{plugin: "plugin-zigbee2mqtt", device: devID, entity: entityID})
					break
				}
			}
		}
		return len(zigbeeLeaves) >= 3
	})
	if !zigbeeReady {
		t.Skipf("need at least 3 Main_LB zigbee lights, found %d", len(zigbeeLeaves))
	}

	var wizLeaves []leaf
	wizReady := s.WaitFor(30*time.Second, func() bool {
		var devices []map[string]any
		_ = s.GetJSON("/api/plugins/plugin-wiz/devices", &devices)
		wizLeaves = wizLeaves[:0]
		for _, d := range devices {
			devID, _ := d["id"].(string)
			if devID == "" || devID == "plugin-wiz" {
				continue
			}
			var entities []map[string]any
			_ = s.GetJSON(fmt.Sprintf("/api/plugins/plugin-wiz/devices/%s/entities", devID), &entities)
			for _, e := range entities {
				if domain, _ := e["domain"].(string); domain == "light" {
					entityID, _ := e["id"].(string)
					wizLeaves = append(wizLeaves, leaf{plugin: "plugin-wiz", device: devID, entity: entityID})
					break
				}
			}
		}
		return len(wizLeaves) > 0
	})
	if !wizReady {
		t.Skip("need at least 1 WiZ light")
	}

	leaves := append(append([]leaf(nil), zigbeeLeaves...), wizLeaves...)
	t.Logf("selected zigbee leaves: %+v", zigbeeLeaves)
	t.Logf("selected wiz leaves: %+v", wizLeaves)

	groupName := fmt.Sprintf("MainLBAndWiz%d", time.Now().UnixNano()%10000)
	for _, l := range leaves {
		labelsPath := fmt.Sprintf("/api/plugins/%s/devices/%s/entities/%s/labels", l.plugin, l.device, l.entity)
		labelsBody := map[string]any{"labels": map[string][]string{"PluginAutomation": {groupName}}}
		t.Logf("PATCH %s body=%v", labelsPath, labelsBody)
		if err := s.PatchJSON(labelsPath, labelsBody, nil); err != nil {
			t.Fatalf("patch labels on %s/%s/%s: %v", l.plugin, l.device, l.entity, err)
		}
	}

	groupID := "group-" + lowercasedLocal(groupName)
	ok := s.WaitFor(10*time.Second, func() bool {
		var entities []map[string]any
		_ = s.GetJSON("/api/plugins/plugin-automation/devices/groups/entities", &entities)
		for _, e := range entities {
			if id, _ := e["id"].(string); id == groupID {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("virtual group %q not created", groupID)
	}
	t.Logf("GET /api/plugins/plugin-automation/devices/groups/entities -> found %s", groupID)

	cmdPath := fmt.Sprintf("/api/plugins/plugin-automation/devices/groups/entities/%s/commands", groupID)
	cmdBody := map[string]any{"type": "turn_on"}
	t.Logf("POST %s body=%v", cmdPath, cmdBody)
	if err := s.PostJSON(cmdPath, cmdBody, nil); err != nil {
		t.Fatalf("post turn_on: %v", err)
	}
	for _, l := range leaves {
		waitForAutomationLightEntityPower(t, s, l.plugin, l.device, l.entity, true, 10*time.Second)
	}

	cmdBody = map[string]any{"type": "turn_off"}
	t.Logf("POST %s body=%v", cmdPath, cmdBody)
	if err := s.PostJSON(cmdPath, cmdBody, nil); err != nil {
		t.Fatalf("post turn_off: %v", err)
	}
	for _, l := range leaves {
		waitForAutomationLightEntityPower(t, s, l.plugin, l.device, l.entity, false, 10*time.Second)
	}

	t.Logf("mixed group command changed %d zigbee+wiz leaves as one group", len(leaves))
}

func lowercasedLocal(s string) string {
	out := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		out[i] = c
	}
	return string(out)
}

func waitForAutomationLightEntityPower(t *testing.T, s *integrationtesting.Suite, pluginID, deviceID, entityID string, wantOn bool, timeout time.Duration) {
	t.Helper()
	path := fmt.Sprintf("/api/plugins/%s/devices/%s/entities", pluginID, deviceID)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var entities []types.Entity
		if err := s.GetJSON(path, &entities); err != nil {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		for _, e := range entities {
			if e.ID != entityID {
				continue
			}
			if len(e.Data.Reported) == 0 {
				break
			}
			var st struct {
				Power bool `json:"power"`
			}
			if err := json.Unmarshal(e.Data.Reported, &st); err == nil && st.Power == wantOn {
				return
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("entity %s/%s/%s did not reach power=%v within %s", pluginID, deviceID, entityID, wantOn, timeout)
}
