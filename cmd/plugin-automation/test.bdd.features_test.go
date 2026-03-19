//go:build bdd

// BDD feature tests for plugin-automation.
// These run Cucumber/Gherkin scenarios against the live storage+messenger
// test environment (embedded NATS, no external infrastructure required).
//
// Run:
//
//	go test -tags bdd -v ./cmd/plugin-automation/...
package main

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
)

func TestBDDFeatures(t *testing.T) {
	if _, err := os.Stat("features"); err != nil {
		t.Skip("features directory not present")
	}

	suite := godog.TestSuite{
		Name: "plugin-automation-bdd",
		ScenarioInitializer: func(ctx *godog.ScenarioContext) {
			c := newBDDCtx(t)
			c.RegisterSteps(ctx)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("BDD suite failed")
	}
}
