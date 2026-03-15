package main

import "github.com/slidebolt/sdk-types"

func coreDevice() types.Device {
	return types.Device{
		ID:         "plugin-automation",
		SourceID:   "plugin-automation",
		SourceName: "plugin-automation",
	}
}

func coreEntities() []types.Entity {
	return types.CoreEntities("plugin-automation")
}

func scriptsDevice() types.Device {
	return types.Device{
		ID:         "scripts",
		SourceID:   "scripts",
		SourceName: "Scripts",
	}
}

func scriptsEntities() []types.Entity {
	return []types.Entity{
		{
			ID:        "lightstripscenes",
			DeviceID:  "scripts",
			Domain:    "light_strip",
			LocalName: "LightStripScenes",
			Actions: []string{
				"turn_on",
				"turn_off",
				"set_brightness",
				"set_rgb",
				"set_effect",
				"set_segment",
				"clear_segments",
			},
		},
	}
}
