package main

import (
	"log"

	runner "github.com/slidebolt/sdk-runner"
)

// main is the entry point for the plugin-automation.
// It creates a new runner with the PluginAutomationPlugin and starts it.
func main() {
	if err := runner.RunCLI(func() runner.Plugin { return &PluginAutomationPlugin{} }); err != nil {
		log.Fatal(err)
	}
}
