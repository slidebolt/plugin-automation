package device

import (
	"github.com/slidebolt/plugin-sdk"
)

func CreateVirtualDevice(b sdk.Bundle, name string, sid string) (sdk.Device, error) {
	dev, err := b.CreateDevice()
	if err != nil {
		return nil, err
	}
	dev.UpdateMetadata(name, sdk.SourceID(sid))
	return dev, nil
}

func CreateGroup(b sdk.Bundle, name string, sid string, members []string) (sdk.Device, error) {
	dev, err := CreateVirtualDevice(b, name, sid)
	if err != nil {
		return nil, err
	}

	// Store members in Raw data
	dev.UpdateRaw(map[string]interface{}{
		"type":    "group",
		"members": members,
	})

	// Default Group Script (Fan-out)
	groupScript := `
		local function setGroupState(ctx, status)
			ctx.UpdateState(status)
		end

		local function publishEntityState(ctx, status, raw, payload)
			local statePayload = { power = status }
			statePayload.brightness = (raw and raw.brightness) or 100
			if raw and raw.color_rgb then
				statePayload.r = raw.color_rgb.r
				statePayload.g = raw.color_rgb.g
				statePayload.b = raw.color_rgb.b
			end
			if raw and raw.color_temperature then
				statePayload.kelvin = raw.color_temperature
			end
			if payload and payload.scene then
				statePayload.scene = payload.scene
			end
			ctx.Publish("entity." .. ctx.ID .. ".state", statePayload)
		end

		function onCommand(cmd, payload, ctx)
			local raw = ctx.GetDeviceRaw() or {}
			local members = raw.members or {}
			local p = payload or {}
			ctx.Log("Group " .. ctx.ID .. " fanning out command: " .. cmd)
			for _, id in ipairs(members) do
				local msg = {}
				for k, v in pairs(p) do msg[k] = v end
				msg.command = cmd
				msg.entity_id = id
				ctx.Publish("entity." .. id .. ".command", msg)
				ctx.Publish("device." .. id .. ".command", msg)
			end

			if payload then
				if payload.r or payload.g or payload.b then
					raw.color_rgb = { r = payload.r or 0, g = payload.g or 0, b = payload.b or 0 }
				end
				if payload.kelvin then
					raw.color_temperature = payload.kelvin
				end
				if payload.level then
					raw.brightness = payload.level
				end
			end
			ctx.UpdateRaw(raw)

			local c = string.lower(cmd or "")
			local status = "last_cmd_" .. (cmd or "")
			if c == "turnoff" or c == "toggleoff" then
				status = "off"
			elseif c == "turnon" or c == "toggleon" or c == "setbrightness" or c == "setrgb" or c == "settemperature" or c == "setscene" then
				status = "on"
			end
			setGroupState(ctx, "active")

			publishEntityState(ctx, status, raw, payload)
		end
	`
	// Create a default entity for the group to receive commands
	ent, _ := dev.CreateEntity(sdk.TYPE_SWITCH)
	ent.UpdateScript(groupScript)

	return dev, nil
}

func CreateLightGroup(b sdk.Bundle, name string, sid string, members []string) (sdk.Device, error) {
	dev, err := CreateVirtualDevice(b, name, sid)
	if err != nil {
		return nil, err
	}

	dev.UpdateRaw(map[string]interface{}{
		"type":    "group",
		"members": members,
	})

	groupScript := `
		local function setGroupState(ctx, status)
			ctx.UpdateState(status)
		end

		local function publishEntityState(ctx, status, raw, payload)
			local statePayload = { power = status }
			statePayload.brightness = (raw and raw.brightness) or 100
			if raw and raw.color_rgb then
				statePayload.r = raw.color_rgb.r
				statePayload.g = raw.color_rgb.g
				statePayload.b = raw.color_rgb.b
			end
			if raw and raw.color_temperature then
				statePayload.kelvin = raw.color_temperature
			end
			if payload and payload.scene then
				statePayload.scene = payload.scene
			end
			ctx.Publish("entity." .. ctx.ID .. ".state", statePayload)
		end

		function onCommand(cmd, payload, ctx)
			local raw = ctx.GetDeviceRaw() or {}
			local members = raw.members or {}
			local p = payload or {}
			ctx.Log("Group " .. ctx.ID .. " fanning out command: " .. cmd)
			for _, id in ipairs(members) do
				local msg = {}
				for k, v in pairs(p) do msg[k] = v end
				msg.command = cmd
				ctx.Publish("entity." .. id .. ".command", msg)
				ctx.Publish("device." .. id .. ".command", msg)
			end

			if payload then
				if payload.r or payload.g or payload.b then
					raw.color_rgb = { r = payload.r or 0, g = payload.g or 0, b = payload.b or 0 }
				end
				if payload.kelvin then
					raw.color_temperature = payload.kelvin
				end
				if payload.level then
					raw.brightness = payload.level
				end
			end
			ctx.UpdateRaw(raw)

			local c = string.lower(cmd or "")
			local status = "last_cmd_" .. (cmd or "")
			if c == "turnoff" or c == "toggleoff" then
				status = "off"
			elseif c == "turnon" or c == "toggleon" or c == "setbrightness" or c == "setrgb" or c == "settemperature" or c == "setscene" then
				status = "on"
			end
			setGroupState(ctx, "active")

			publishEntityState(ctx, status, raw, payload)
		end
	`

	ent, _ := dev.CreateEntityEx(sdk.TYPE_LIGHT, []string{
		sdk.CAP_BRIGHTNESS,
		sdk.CAP_RGB,
		sdk.CAP_TEMPERATURE,
		sdk.CAP_SCENE,
	})
	ent.UpdateScript(groupScript)

	return dev, nil
}
