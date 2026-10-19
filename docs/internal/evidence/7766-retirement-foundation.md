# Evidence: #7766 fenced tombstone-retirement foundation (foundation lane)

## What changed

Three increments on `feat-7766-retirement`: the `repository_retirements`
tombstone table (migration 207) with idempotent mark/lookup/preview;
the mark-and-fence transaction with tuned bindings and a 1s lock wait;
retired-state read surfaces; the terminal-gated purge with key-sidecar
pruning; and the purged-retirements counter. The purge also deletes the
no-FK projection-intent rows (`shared_projection_intents`,
`shared_projection_unroutable_intents` by repo-or-scopes,
`collector_generation_dead_letters` by scopes) with per-table ledgering,
and Preview counts all three so dry run and ledger agree. Under-lock
request_too_large refusals carry the same PerRepo breakdown the
precheck promises. No caller wires the fencer
or purger yet; the operator trigger rides #8602.

## Why no regression

No-Regression Evidence: the production writes (fencer and purger) have
no callers outside tests. Two read-only probes run live in this diff:
the marker-gate CTE folded into the scope upsert (one indexed single-row
tombstone read over the repo_id-leading partial unique index per upsert;
the ON CONFLICT arm is byte-identical, so admission — never merge
semantics — changes) and the repositories list annotation (one
page-bounded tombstone read per list page). Neither probe writes, and
neither changes a query plan, index choice, worker, lease, or
concurrency knob. Fence and purge request budgets are measured
constants enforced before any lock (`RetirementFenceMaxGenerations`,
`RetirementFenceMaxReducerRows`, 25-repo cap); the purge deletes only
fenced-scope rows plus repo-keyed leftovers with per-table tombstone
ledgering. Baseline: origin/main without the lane. After: `go build
./...` clean, `go vet` on touched packages clean, 8/8 fence-gate (incl.
2 under-lock PerRepo refusal proofs), 4/4
tombstone + live round-trip, 3/3 live fence/summary/freshness, 7/7 + 2/2
purge proofs (live purge seeds and asserts intent/unroutable/dead-letter
deletes with ledger), query/status/telemetry/migrations/queryplan packages
green.

## Why observability is unchanged except one registered counter

No-Observability-Change: except the registered
`eshu_dp_repository_retirements_purged_total` instrument, which no
production path emits yet (registration asserted in instruments_test).
No new span, log key (beyond the designed purge WARN on blocked exits,
covered by tests), status field, or runtime knob reaches operators.
