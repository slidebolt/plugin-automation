# Git Workflow for plugin-automation

This repository contains the Slidebolt Automation Plugin, which provides group-based control and higher-level automation abstractions. It produces a standalone binary.

## Dependencies
- **Internal:**
  - `sb-contract`: Core interfaces.
  - `sb-domain`: Shared domain models.
  - `sb-logging`: Logging implementation.
  - `sb-logging-sdk`: Logging client interfaces.
  - `sb-messenger-sdk`: Shared messaging interfaces.
  - `sb-runtime`: Core execution environment.
  - `sb-script`: Scripting engine for complex group logic.
  - `sb-storage-sdk`: Shared storage interfaces.
  - `sb-testkit`: Testing utilities.
- **External:** 
  - Standard Go library and NATS.
  - `github.com/cucumber/godog`: BDD testing framework.

## Build Process
- **Type:** Go Application (Plugin).
- **Consumption:** Run as a background plugin service.
- **Artifacts:** Produces a binary named `plugin-automation`.
- **Command:** `go build -o plugin-automation ./cmd/plugin-automation`
- **Validation:** 
  - Validated through unit tests: `go test -v ./...`
  - Validated through BDD tests: `go test -v ./cmd/plugin-automation`
  - Validated by successful compilation of the binary.

## Pre-requisites & Publishing
As a high-level orchestration plugin, `plugin-automation` must be updated whenever any of the core domain, messaging, storage, scripting, or testkit SDKs are changed.

**Before publishing:**
1. Determine current tag: `git tag | sort -V | tail -n 1`
2. Ensure all local tests pass: `go test -v ./...`
3. Ensure the binary builds: `go build -o plugin-automation ./cmd/plugin-automation`

**Publishing Order:**
1. Ensure all internal dependencies are tagged and pushed.
2. Update `plugin-automation/go.mod` to reference the latest tags.
3. Determine next semantic version for `plugin-automation` (e.g., `v1.0.5`).
4. Commit and push the changes to `main`.
5. Tag the repository: `git tag v1.0.5`.
6. Push the tag: `git push origin main v1.0.5`.

## Update Workflow & Verification
1. **Modify:** Update group logic or automation features in `app/` or `cmd/`.
2. **Verify Local:**
   - Run `go mod tidy`.
   - Run `go test ./...`.
   - Run `go test ./cmd/plugin-automation` (BDD features).
   - Run `go build -o plugin-automation ./cmd/plugin-automation`.
3. **Commit:** Ensure the commit message clearly describes the automation plugin change.
4. **Tag & Push:** (Follow the Publishing Order above).
