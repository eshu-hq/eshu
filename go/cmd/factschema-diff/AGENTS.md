# AGENTS.md - cmd/factschema-diff guidance

## Read first

1. `README.md` - command purpose, baseline resolution, and breaking-change
   rule.
2. `diff.go` - the pure schema-comparison logic (no git dependency).
3. `main.go` - CLI flags and git wiring (`git merge-base`, `git show`).
4. `docs/internal/design/contract-system-v1.md` §5 (versioning policy) and §6
   (enforcement gates) - the design contract this command implements.

## Invariants

- The command is build-time/CI-time only. Do not add runtime storage, graph,
  network, or telemetry dependencies.
- `diff.go` must stay free of any `os/exec` or filesystem dependency so its
  tests run as pure unit tests against in-memory JSON fixtures. All git and
  filesystem I/O belongs in `main.go`.
- A schema file with no baseline counterpart at `-base-ref` is a pass, not a
  failure. Do not change this without updating the design decision recorded
  in `README.md` and re-checking `docs/internal/design/contract-system-v1.md`
  §5/§6 for whether a contracts release tag now exists.
- Every reported violation must name the specific field and violation kind
  (`removed_required_field`, `narrowed_type`, `widened_required`). Do not
  regress to a generic "schema changed" message — an external collector
  author must be able to act on the message alone.
- Keep this package under the repo's 500-line file cap; `diff.go` and
  `main.go` are already split along the pure-logic/CLI-wiring boundary
  described above — do not merge them back into one file.

## Common changes

- **New violation kind**: add a `ViolationKind` constant in `diff.go`, a
  detection branch in `compareSchemas`, and a fixture-driven test in
  `diff_test.go` proving both the failing case and that a major bump
  suppresses it.
- **New baseline source** (e.g. resolving a `factschema-<version>` tag once
  one exists): extend `parseOptions`/`run` in `main.go`; do not change
  `compareSchemas`'s signature, which is baseline-source-agnostic by design.
