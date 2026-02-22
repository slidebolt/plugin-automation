package main

import (
	"fmt"
	"github.com/slidebolt/plugin-framework"
	 "github.com/slidebolt/plugin-automation/pkg/bundle"
	"github.com/slidebolt/plugin-sdk"
)

func main() {
	fmt.Println("Starting Automation Plugin Sidecar...")
	framework.Init()

	b, err := sdk.RegisterBundle("plugin-automation")
	if err != nil {
		fmt.Printf("Failed to register bundle: %v\n", err)
		return
	}

	p := bundle.NewPlugin()
	if err := p.Init(b); err != nil {
		fmt.Printf("Failed to init plugin: %v\n", err)
		return
	}

	fmt.Println("Automation Plugin is running.")
	select {}
}