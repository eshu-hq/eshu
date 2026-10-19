# Evidence: #7766 fenced tombstone-retirement foundation (#8603 lane)

## What changed

Three increments on `feat-7766-retirement`: the `repository_retirements`
tombstone table (migration 207) with idempotent mark/lookup/preview;
the mark-and-fence transaction with tuned bindings and a 1s lock wait;
retired-state read surfaces; the terminal-gated purge with key-sidecar
pruning; and the purged-retirements counter. No caller wires the fencer
or purger yet; the operator trigger rides #8602.

## Why no regression

No-Regression Evidence: no production path executes the new code — the
fencer and purger have no callers outside tests — so the runtime delta is
zero by construction. Fence and purge request budgets are measured
constants enforced before any lock (`RetirementFenceMaxGenerations`,
`RetirementFenceMaxReducerRows`, 25-repo cap); the purge deletes only
fenced-scope rows plus repo-keyed leftovers with per-table tombstone
ledgering. Baseline: origin/main without the lane. After: `go build
./...` clean, `go vet` on touched packages clean, 6/6 fence-gate, 4/4
tombstone + live round-trip, 3/3 live fence/summary/freshness, 7/7 + 2/2
purge proofs, query/status/telemetry/migrations/queryplan packages
green. No query-plan, index-choice, worker, lease, or concurrency-knob
change ships in this lane.

## Why observability is unchanged except one registered counter

No-Observability-Change except the registered
`eshu_dp_repository_retirements_purged_total` instrument, which no
production path emits yet (registration asserted in instruments_test).
No new span, log key (beyond the designed purge WARN on blocked exits,
covered by tests), status field, or runtime knob reaches operators.
