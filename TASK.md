# TASK: plugin-automation

## Status: Stub — No Implementation

This plugin is a scaffold placeholder. Every hook is a no-op pass-through.
No automation logic exists.

## Required Work

- [ ] Define what automation means in this context: rules engine, trigger/condition/action model, scripting, or something else
- [ ] Implement rule storage — rules should be persisted via `OnStorageUpdate` / loaded in `OnInitialize`
- [ ] Implement `OnDevicesList` and `OnEntitiesList` to expose automation rules as addressable entities
- [ ] Implement `OnCommand` to allow creating, updating, enabling, and disabling rules
- [ ] Implement `OnEvent` to evaluate incoming events against stored rules and fire actions (likely via RPC to target plugins)
- [ ] Remove the meaningless `Config.Meta = "plugin-automation-metadata"` stamp in `OnDeviceCreate` — it does nothing useful until there is real device semantics
- [ ] Add integration tests once there is something to test
