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
