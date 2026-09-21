# Agent instructions: internal/reducer/code/value/refresh

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

Re-runs the global value-flow fixpoint after late producers land (issue
#6785). See the README's Purpose and Ownership boundary sections for what
this package owns and what stays in `code/value`, `storage/postgres`, and the
reducer root.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/code/value/refresh/README.md`
- `docs/internal/design/6785-value-flow-cloud-sink-refresh.md`

## Invariants

- **Fixpoint only.** The handler must not persist summaries, sources, or
  graph ids; producers change only graph edges.
- **No import of the reducer root, ever.** This package is a leaf: the root
  imports it for handler registration, never the reverse. `reducer/contract`
  is importable (it is a leaf below root, like for `code/function/summary`).
- **The refresh item is a singleton** anchored to the migration-seeded
  `eshu:global` scope; do not enqueue per-scope refresh intents from the
  projector.
