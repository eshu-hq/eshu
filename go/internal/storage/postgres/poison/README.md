# Poison

## Purpose

The poison package owns the dead-letter/poison class in `fact_work_items`
(#4740): rows with `status = 'dead_letter'` whose scope has no strictly-newer
`scope_generations` row. The generation-liveness sweep never reaches this
class — `dead_letter` is terminal and unclaimable, and the wedge-detection
`NOT EXISTS` guard treats same-generation `dead_letter` reducer work as still
in progress. Without this arm, such a scope can never self-heal.

## Ownership boundary

Detection (`CountDeadLetters`) and bounded recovery (`RecoverDeadLetters`)
over the poison class only. It does not own claiming, retry budgets for
non-terminal rows, or the runner loop — the sweep cadence and the auto-retry
flag live in `reducer/maintenance/poison` and `cmd/reducer`. It does not own
the generation-liveness sweep for wedged `active` generations.

## Exported surface

- `Store`, `NewStore` — Postgres-backed detector and bounded recoverer
- `Policy`, `Normalize` — re-drive budget (`MaxRecoverAttempts`, default 1)
  and per-sweep cap (`BatchLimit`, default 200)
- `StuckCounts`, `RecoveryResult` — gauge input and sweep summary
- `CountDeadLettersQuery`, `RecoverDeadLettersQuery` — shipped statements,
  exported as one contract (the parent package pins the recovery statement in
  its container-image capability latch)

See `doc.go` for the full contract.

## Dependencies

- `storage/postgres/db` — `ExecQueryer` surface the store runs against

Non-test files must never import the parent `postgres` package: the parent's
tests and `cmd/reducer` import this one, so the reverse import is a cycle.

## Telemetry

None emitted here. The gauges (`eshu_dp_poison_dead_letter_*`,
`eshu_dp_poison_liveness_*`) are registered by `cmd/reducer` from the counts
this package returns, and the runner in `reducer/maintenance/poison` logs the
sweep outcome.

## Gotchas / invariants

- The recovery UPDATE's `AND target.status = 'dead_letter'` is a write-time
  re-verify, not a redundant predicate: under Read Committed a row a
  concurrent worker reclaimed between the candidate snapshot and the row lock
  must be skipped (EvalPlanQual recheck). Keep the row-self predicate on the
  locked relation.
- The attempt budget lives in the row's JSONB payload and is capped with
  `LEAST`; there is no schema column. A row at or past the ceiling is
  excluded by the candidate CTE, never retried.
- `defaultBatchLimit` (200) deliberately matches the generation-liveness sweep
  batch default; changing one does not change the other.
- `"Strictly newer"` is `(ingested_at, generation_id)` compared
  lexicographically, mirroring the generation supersede comparator. A scope
  with any newer generation has moved on: its dead-letter row is historical,
  not live poison.

## Related docs

- `go/internal/reducer/recovery-runners.md` (poison dead-letter section)
- `docs/internal/design/6693-postgres-target-tree/control-plane.md`

## Performance and observability evidence

No-Regression Evidence: the #6693 move from the parent `postgres` package
changed Go identifiers only (`PoisonLivenessStore` to `Store`,
`CountPoisonDeadLetters` to `CountDeadLetters`,
`RecoverPoisonDeadLetters` to `RecoverDeadLetters`, and the same de-stutter
for `Policy`, `StuckCounts`, `RecoveryResult`, the constructor, and the two
query constants). Both shipped SQL statements are byte-identical and both
defaults are unchanged (`MaxRecoverAttempts` 1, `BatchLimit` 200), so every
query plan, lock target (`work_item_id`), and budget gate is the same.
Baseline `eac0860933` (pre-move) against the moved tree on local Docker
Postgres (`postgres:18-alpine`, isolated proof schemas, minimal seed: 3
poison scopes plus a healed decoy, one bounded-arm row, one race row):
`DetectionGapAndGauge` PASS and `GaugeUsesPartialIndex` PASS on both trees;
`BoundedArmIncrementsThenZero` and the write-time race FAIL identically on
both trees with SQLSTATE 42703 because the minimal proof schema predates the
`container_image_identity_*` columns the shipped UPDATE requires
(pre-existing, reported separately — not a move regression). No new query,
index, worker, batch, or lease change, so there is nothing left to measure.

No-Observability-Change: this package emits no metrics, spans, or logs. The
`eshu_dp_poison_dead_letter_*` and `eshu_dp_poison_liveness_*` gauges are
registered by `cmd/reducer` from the counts returned here, and that wiring is
identifier-only in this move.
