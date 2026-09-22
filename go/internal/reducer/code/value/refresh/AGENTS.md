# Agent instructions: internal/reducer/code/value/refresh

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

Re-runs the global value-flow fixpoint after late producers land (issues
#6785, #6923), fenced by an input-liveness check before it solves. See the
README's Purpose and Ownership boundary sections for what this package owns
and what stays in `code/value`, `storage/postgres`, and the reducer root.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/code/value/refresh/README.md`
- `docs/internal/design/6785-value-flow-cloud-sink-refresh.md`
- `docs/internal/evidence/6923-value-flow-single-solve.md`

## Invariants

- **Fixpoint only.** The handler must not persist summaries, sources, or
  graph ids; producers change only graph edges.
- **No import of the reducer root, ever.** This package is a leaf: the root
  imports it for handler registration, never the reverse. `reducer/contract`
  and `reducer/crossscope` are importable (both are leaves below root that
  import only `contract`/`factload`, like for `code/function/summary`).
- **The refresh item is a singleton** anchored to the migration-seeded
  `eshu:global` scope; do not enqueue per-scope refresh intents from the
  projector.
- **Fence before load, always.** `checkInputsLiveness` must run before
  `Fixpoint.ProjectValueFlowFixpointEvidence`, never after — a post-load
  fence reintroduces the #6923 pre-convergence read it exists to prevent.
- **The starvation bound is elapsed time, never attempt_count.**
  `value_flow_inputs_not_ready` is non-counting, so `Intent.AttemptCount`
  freezes on every defer; only `crossscope.ReadinessCycleAnchor` compared
  against `crossscope.ProducerReadinessMaxWait` can end the wait. See
  `crossscope.ProducerReadinessMaxWait`'s doc comment for the incident this
  rule prevents repeating.
