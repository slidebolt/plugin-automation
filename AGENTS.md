# plugin-automation Instructions

`plugin-automation` follows the reference runnable-module architecture.

- Keep `cmd/plugin-automation/main.go` as a thin wrapper only.
- Put runtime lifecycle and group-management wiring in `app/`.
- Keep private translation logic under `internal/...`.
- Existing `cmd` tests may use compatibility aliases during migration, but new logic should go into `app/` or `internal/...`.
