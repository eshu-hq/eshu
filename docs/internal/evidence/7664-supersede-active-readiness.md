# 7664: supersede stale reducer rows on active-generation readiness — performance note

The #7664 fix widens one predicate in `supersedeInactiveReducerGenerationsCTE`
(`go/internal/storage/postgres/reducer_generation_filter_sql.go`): the sweep's
readiness gate is now the OR of the stale row's own generation and the scope's
active generation (via the new
`reducerClaimReadinessGateForGenerationSQL`). A stale reducer row whose
`graph_projection_phase_state` rows exist only under the active generation is
retired; a stale row whose phases exist under neither generation is still held
out (#4445/A2); the claim candidate gate stays on the row's own generation, so
no live row becomes claimable early.

## No-Regression Evidence:

Conflict domain: `fact_work_items` (conflict_domain `scope`,
conflict_key = scope_id). Worker/lease settings in proof: 2 concurrent
`ReducerQueue.Claim` callers on independent Postgres connections, 1-minute
leases, production claim SQL, local PostgreSQL 18 in Docker.
Baseline: origin/main before this change — 0 of 300 seeded stale gated rows
retire (the #7664 stall); the live row still claims.
After: one `Claim` retires all 300 stale rows with
`failure_class=reducer_superseded_by_newer_active_generation`, the
negative-control row (readiness met under neither generation) stays pending,
and the live active-generation row is claimed exactly once
(`TestReducerClaimSupersedesStaleWorkWhenReadinessMetUnderActiveGeneration`:
RED 0/300 before, GREEN 300/300 after).
Concurrency: 2 claimers racing over 2 scopes (25 stale + 1 live each) see no
error, no deadlock, 50/50 stale rows superseded, and each live row at
`attempt_count=1` with no live row superseded
(`TestReducerSupersedeActiveReadinessConcurrentClaims`, also RED before).
Plan: `TestReducerSupersedeActiveReadinessClaimPlan` captures
`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` of the real claim statement over
the shape in a rolled-back transaction; the sweep CTE and the phase-state
gate are in the plan.
Why safe: the change adds one read-only OR branch over an already-joined
phase table — no new writes, no new lock targets, no lock-order change; the
sweep stays idempotent (`superseded` is terminal and excluded from the source
set, and EvalPlanQual rechecks `stale.status` on the locked row version); the
scope pointer moves monotonically forward, so a row judged older-than-active
under the statement snapshot stays so. The full
`go/internal/storage/postgres` package suite stays green.

## No-Observability-Change:

No new metric, span, log key, route, worker, lease, or runtime knob.
Operators diagnose the path through the existing durable signals: queue
status counts (`pending` draining on superseded generations), the swept
rows' `failure_class=reducer_superseded_by_newer_active_generation` and
`failure_details.reason=inactive_generation`, and the existing
`eshu_dp_postgres_query_duration_seconds` Postgres query spans covering the
claim statement. Adding a per-sweep counter would require plumbing the
CTE's RETURNING count out of four embedding statements and is out of scope;
the #4445 gate that this fix extends set the same precedent.
