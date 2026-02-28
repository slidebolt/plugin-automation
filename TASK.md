# TASK

## Scope
Harden runtime behavior and test hygiene for this repository.

## Constraints
- No git commits or tags from subprocesses unless explicitly requested.
- Keep changes minimal, testable, and production-safe.
- Prefer deterministic shutdown/startup behavior.

## Required Output
- Small PR-sized patch.
- Repro steps.
- Validation commands and expected results.
- Known risks/limits.

## Priority Tasks
1. Keep strict command contract usage (`turn_on`/`turn_off` etc.) only.
2. Ensure script runtime and shutdown semantics are deterministic.
3. Verify no plugin-specific hardcoding leaks into automation core paths.

## Done Criteria
- Lua automation remains plugin-agnostic and stable across restarts.

## Validation Checklist
- [ ] Build succeeds for this repo.
- [ ] Local targeted tests (if present) pass.
- [ ] No new background orphan processes remain.
- [ ] Logs clearly show failure causes.
