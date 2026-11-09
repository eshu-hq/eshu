# #10422 Stale-Generation Fence: No-Regression and Observability Evidence

The scope pass-state upsert skips, on both the insert and the update path,
when a lifecycle-newer (`ingested_at`, `generation_id`) retained generation
is reader-visible (activated and active or superseded). Tokens are per-pass
monotonic, so the fencing-token guard alone cannot order across generations;
the fence decides once per statement (materialized CTE) under the writer's
READ COMMITTED transaction. Missing lineage fails open to the token guard.

## No-Regression Evidence:

- Baseline: every admitted pass records (token guard only).
- After: passes whose generation is lifecycle-newest (the order is total,
  so same-generation is the only tie, already excluded; lineage absent
  fails open) record exactly as before; only a pass with a strictly newer
  reader-visible generation skips. No schema change, no new migration, no
  new index: the EXISTS probe rides the existing
  `scope_generations_scope_idx` prefix (`scope_id` equality) over a
  per-scope generation history.
- Backend/version: disposable PostgreSQL 18.6 (pg-10422), schema migrated to
  the lane head (312 migrations).
- Absent lineage: a write for a generation with no scope_generations row
  fails the findings foreign key before the record step, so the row is
  untouched and no skip is reported (pinned live); inside the statement an
  absent row would fail open to the token guard by NULL semantics.
- Input shape: synthetic fence proof (one scope, gen-1 superseded, gen-2
  active with a later ingested_at, strict fencing tokens mirroring the
  production sequence).
- Terminal counts: `TestStaleGenerationPassPreservesScopePassStateLive`
  FAIL pre-fix (stale replay re-marked the cleared row partial) then PASS
  4.70s post-fix; sibling writer-path proofs
  `TestHasOutOfStampActiveRowsLive` PASS 4.72s and
  `TestScopedFallbackConvergesLive` PASS 0.15s; core package
  `go/internal/reducer/supplychain/core` GREEN (includes the F1 guard pin
  and the new fence-clause pin); storage status suites GREEN.
- Gate enrollment: the proof rides the reducer contention gate via
  `TestStaleGenerationPassPreservesScopePassStateLive` in the `-run` filter
  and the `--select` verifier plus
  `scripts/lib/reducer_contention_inventory.json`;
  `TestSupplyChainImpactLiveProofsRunInTheReducerContentionGate` GREEN.
- Why safe: the fence is a single-statement predicate inside the existing
  upsert; it adds no lock, lease, claim, worker, queue, migration, or
  transaction scope. A concurrent activation committing before the statement
  is seen (READ COMMITTED, one decision per statement); one committing
  after is ordered after, and the newer generation's own pass overwrites.

## Observability Evidence:

- New `scope_pass_state_stale_skipped` success-log sub-signal on the reducer
  success line (1 when the fence left the row untouched), emitted from
  `impact.go`, named in the reducer-domains coverage doc
  (telemetry-coverage verifier agrees).
- The skipped pass still commits its per-generation findings under its own
  admission; only the shared per-scope row is held. Operators distinguish
  a hold (old token on the row plus the signal) from a fresh record.
