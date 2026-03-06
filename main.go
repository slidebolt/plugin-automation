package main

import (
	"log"

	runner "github.com/slidebolt/sdk-runner"
)

// main is the entry point for the plugin-automation.
// It creates a new runner with the PluginAutomationPlugin and starts it.
func main() {
	r, err := runner.NewRunner(&PluginAutomationPlugin{})
	if err != nil {
		log.Fatal(err)
	}
	if err := r.Run(); err != nil {
		log.Fatal(err)
	}
}
