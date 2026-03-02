### `plugin-automation` repository

#### Project Overview

This repository contains the `plugin-automation`, a plugin for the Slidebolt system. This plugin provides a virtual switch capability, allowing for the creation of simple automations and virtual devices within the Slidebolt ecosystem.

#### Architecture

The `plugin-automation` is a Go application that implements the `runner.Plugin` interface from the `slidebolt/sdk-runner`. Its primary function is to act as a virtual switch. It listens for commands and updates its state accordingly, without controlling any physical hardware.

Key architectural points:

-   **Virtual Switch**: The plugin handles commands for entities within the `switch` domain.
-   **State Management**: When it receives a `turn_on` or `turn_off` command, it updates the entity's state to reflect the new power status.
-   **Event Emission**: After updating its state, the plugin emits an event to notify the rest of the system about the change. This allows other components to react to the virtual switch's state changes.

This plugin is likely used for creating simple automations, testing, or as a building block for more complex virtual devices.

#### Key Files

| File | Description |
| :--- | :--- |
| `go.mod` | Defines the Go module and its dependencies on `sdk-runner`, `sdk-types`, and `sdk-entities`. |
| `main.go` | Contains the complete implementation of the plugin, including the command handling for the virtual switch. |

#### Available Commands

This plugin is not intended to be run directly by the user. It is a component that is loaded and managed by the Slidebolt system. It responds to the following commands for entities in the `switch` domain:

-   `turn_on`
-   `turn_off`
