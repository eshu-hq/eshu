# AGENTS.md - internal/parser/parsertest guidance

## Read first

1. `README.md` - package boundary and exported helpers
2. `doc.go` - godoc contract
3. `helpers.go` - shared assertion behavior and failure text
4. `../AGENTS.md` - parent parser invariants

## Invariants this package enforces

- This package is test support. Production packages must not import it.
- Helpers that inspect parser payloads preserve the exact type and order checks
  used by the external tests they replace.
- Every helper calls `t.Helper()` before it can fail.
- Fixture files use owner-only permissions and caller-provided paths so tests
  remain independent of the process working directory.

## Common changes and how to scope them

- Add a helper only after two external parser tests need the same assertion.
- Keep returned bucket-item and string-slice helpers strict: require
  `[]map[string]any` and `[]string`, and preserve the original failure text.
- Migrate one language family at a time and keep its fixtures and assertions
  unchanged.
- Test helper changes through the consuming external parser package and the
  recursive parser test suite.

## Failure modes and how to debug

- A failure reported inside `helpers.go` usually means a new helper omitted
  `t.Helper()`.
- A payload type mismatch means the parser contract changed or the migrated
  assertion became weaker; compare the original helper before changing it.
- A working-directory-only failure means a fixture path is no longer rooted in
  `t.TempDir()` or another absolute directory.

## Anti-patterns specific to this package

- Importing language-owned parser packages to encode language behavior here.
- Normalizing or sorting payloads before comparison when the original test
  asserted order.
- Adding fallback type conversions that let malformed payloads pass.

## What NOT to change without an ADR

- Do not turn this package into a production parser dependency or a payload
  compatibility layer.
