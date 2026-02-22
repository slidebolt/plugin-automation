package bundle

import (
	"fmt"
	 "github.com/slidebolt/plugin-automation/pkg/device"
	"github.com/slidebolt/plugin-sdk"
	"strings"
)

type AutomationPlugin struct {
	bundle     sdk.Bundle
	controlDev sdk.Device
	controlEnt sdk.Entity
}

func (p *AutomationPlugin) Init(b sdk.Bundle) error {
	p.bundle = b
	b.UpdateMetadata("Automation Engine")
	b.Log().Info("Automation Plugin Initializing...")

	// A single control entity receives orchestration commands from MCP via core.publish.
	ctrl, ent, err := ensureAutomationControl(b)
	if err != nil {
		return err
	}
	ent.OnCommand(func(cmd string, payload map[string]interface{}) {
		if strings.EqualFold(cmd, "create_group") {
			p.handleCreateGroup(payload)
		} else if strings.EqualFold(cmd, "update_group") {
			p.handleUpdateGroup(payload)
		}
	})
	p.controlDev = ctrl
	p.controlEnt = ent
	b.Log().Info("Automation control ready: entity=%s", ent.ID())
	return nil
}

func (p *AutomationPlugin) Shutdown() {}

func NewPlugin() *AutomationPlugin {
	return &AutomationPlugin{}
}

func ensureAutomationControl(b sdk.Bundle) (sdk.Device, sdk.Entity, error) {
	var ctrl sdk.Device

	if obj, ok := b.GetBySourceID(sdk.SourceID("automation-control")); ok {
		switch v := obj.(type) {
		case sdk.Device:
			ctrl = v
		case sdk.Entity:
			dev, err := b.GetDevice(v.DeviceID())
			if err == nil {
				ctrl = dev
			}
		}
	}

	if ctrl == nil {
		created, err := device.CreateVirtualDevice(b, "Automation Control", "automation-control")
		if err != nil {
			return nil, nil, err
		}
		ctrl = created
	}
	_ = ctrl.UpdateMetadata("Automation Control", sdk.SourceID("automation-control"))

	var ent sdk.Entity
	if obj, ok := ctrl.GetBySourceID(sdk.SourceID("automation-control")); ok {
		if existing, ok := obj.(sdk.Entity); ok {
			ent = existing
		}
	}
	if ent == nil {
		created, err := ctrl.CreateEntity(sdk.TYPE_SWITCH)
		if err != nil {
			return nil, nil, err
		}
		ent = created
	}
	if err := ent.UpdateMetadata("Automation Control", sdk.SourceID("automation-control")); err != nil {
		return nil, nil, fmt.Errorf("update control metadata: %w", err)
	}
	return ctrl, ent, nil
}

func (p *AutomationPlugin) handleCreateGroup(payload map[string]interface{}) {
	if payload == nil {
		p.bundle.Log().Error("create_group payload is nil")
		return
	}

	name := asString(payload["name"])
	if name == "" {
		name = "Group"
	}
	sourceID := asString(payload["source_id"])
	if sourceID == "" {
		sourceID = strings.ToLower(strings.ReplaceAll(name, " ", ""))
	}

	members := toStringSlice(payload["members"])
	if len(members) == 0 {
		p.bundle.Log().Error("create_group requires non-empty members")
		return
	}

	grp, err := device.CreateLightGroup(p.bundle, name, sourceID, members)
	if err != nil {
		p.bundle.Log().Error("create_group failed: %v", err)
		return
	}
	p.bundle.Log().Info("create_group success: device=%s name=%s source_id=%s members=%d", grp.ID(), name, sourceID, len(members))
}

func (p *AutomationPlugin) handleUpdateGroup(payload map[string]interface{}) {
	if payload == nil {
		p.bundle.Log().Error("update_group payload is nil")
		return
	}

	sourceID := asString(payload["source_id"])
	if sourceID == "" {
		p.bundle.Log().Error("update_group requires source_id")
		return
	}

	members := toStringSlice(payload["members"])
	if len(members) == 0 {
		p.bundle.Log().Error("update_group requires non-empty members")
		return
	}

	obj, ok := p.bundle.GetBySourceID(sdk.SourceID(sourceID))
	if !ok {
		p.bundle.Log().Error("update_group failed: group not found source_id=%s", sourceID)
		return
	}

	var dev sdk.Device
	switch v := obj.(type) {
	case sdk.Device:
		dev = v
	case sdk.Entity:
		d, err := p.bundle.GetDevice(v.DeviceID())
		if err == nil {
			dev = d
		}
	}

	if dev == nil {
		p.bundle.Log().Error("update_group failed: could not resolve device for source_id=%s", sourceID)
		return
	}

	_ = dev.UpdateRaw(map[string]interface{}{
		"type":    "group",
		"members": members,
	})

	name := asString(payload["name"])
	if name != "" {
		_ = dev.UpdateMetadata(name, sdk.SourceID(sourceID))
	}

	p.bundle.Log().Info("update_group success: device=%s source_id=%s members=%d", dev.ID(), sourceID, len(members))
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

func toStringSlice(v interface{}) []string {
	raw, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
