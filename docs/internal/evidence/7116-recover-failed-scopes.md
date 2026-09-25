# #7116 — all-scopes recovery skipped failed scopes without saying so

`POST /api/v0/admin/recover-generations` with `all_scopes` (and the named-scope
form of the same refinalize) selected only `ingestion_scopes` rows with
`status = 'active'` and a non-null `active_generation_id`. A scope whose latest
generation failed carries `status = 'failed'` and no active generation, so it
was skipped, and the response gave no sign that anything was left out. After a
graph-backend rebuild that repository was simply absent from the new graph. The
real case was `repository:r_874801ea` on ops-qa: newest generation dead-lettered
with `graph_write_timeout`, older generations superseded, and 810 items enqueued
without it.

## Root-Cause Evidence:

`AffectedGenerationsTemplate` (`go/internal/storage/postgres/rebuild/reset/reset.go`
at `fb08c0c657`) filtered on `scope.active_generation_id IS NOT NULL AND
scope.status = 'active'`. The projector's failure path
(`failProjectorWorkQuery`, `projector_queue_sql.go`) sets the failed scope to
`status = 'failed'` and clears `active_generation_id` when the failed generation
was the active one, so a scope the projector gave up on can never satisfy that
predicate. Reproduced with a live-Postgres regression before any fix: a seeded
failed scope (newest generation `failed`, older `superseded`, no active
generation) beside an active scope. `RefinalizeScopeProjections(AllScopes)`
returned only the active scope, and a named request for the failed scope returned
`Enqueued = 0`. RED output: `TestRefinalizeFailedScopeAllScopesEnqueuesNewestFailedGeneration`
failed with "all-scopes rebuild skipped failed scope" and
`TestRefinalizeFailedScopeExplicitScopeIDsIncludesFailedScope` with
"result.Enqueued = 0, want 1". The response carried no skipped count, which is
why the operator only found it by comparing the graph to `ingestion_scopes` 20
hours later.

## Design decisions

**A re-projected failed generation becomes active on success.** Read, then proved
against real Postgres: `ProjectorQueue.Ack` runs `updateProjectorScopeGenerationQuery`
(`status = 'active'`, `active_generation_id = $3`) and `activateProjectorGenerationQuery`
(`status = 'active'` for the target generation, whatever its prior status).
`claimProjectorWorkQuery` does not filter on the generation status, and the
recovered work item id (`refinalize_<scope>_<generation>`) is distinct from the
original dead-lettered projector row, so the old row stays `dead_letter` and does
not interfere. The live test claims and acks the recovered work and asserts the
scope ends `active` with `active_generation_id` = the recovered generation.

**Selection.** One statement, one snapshot, covers:

- an active scope through its `active_generation_id` (unchanged); and
- a failed scope with no active generation through its newest generation that is
  not superseded, when that generation is itself `failed`.

A failed scope whose newest non-superseded generation is `pending` is skipped
(`newest_generation_not_failed`): that generation has its own projector work, and
queueing it again would project the same generation twice. A failed scope with
only superseded generations is skipped (`no_recoverable_generation`): nothing in
Postgres can be projected, only a new collection heals it.

**Named scope requests include failed scopes.** The named path and the all-scopes
path share one selection so "recover repository X" cannot answer differently from
"recover everything" for the same scope. An operator who names a failed scope has
asked for it; refusing would send them to `replay` with hand-collected work item
ids, which is the workaround this change removes.

**Reset statements on a never-active generation.** `ApplyPreRetirement` and
`RetireResolutionGenerations` are keyed on `(scope_id, generation_id)` pairs and
take no dependency on the generation ever having been active. The live test seeds
a succeeded reducer item, a completed shared intent, a readiness phase row, and an
active relationship generation for the failed generation, and asserts all four are
cleared or retired.

**Concurrency and locking are unchanged.** The generation read is still the first
statement of the refinalize transaction, takes no row locks, and is bound once by
the drain wait, the claim fence, the enqueue, and the four resets. The drain wait
and the EXCLUSIVE `fact_work_items` fence are untouched. The skipped report comes
out of the same statement, so it cannot disagree with the covered set.

## Skipped-scope report

`RefinalizeResult.Skipped` (`go/internal/recovery/skipped.go`) carries exact counts
by a closed reason set and at most 10 sample scope ids per reason. Both admin
responses (`recover-generations`, `refinalize`) and the runtime admin refinalize
response carry `skipped_scopes` (always present; empty objects when nothing was
skipped). Reasons: `no_recoverable_generation`, `newest_generation_not_failed`,
`no_active_generation`, `unknown_scope` (a named id with no `ingestion_scopes`
row, computed in Go from the same read's rows). An idempotent replay
(`duplicate: true`) does not carry the report, like the four reset counters: the
`admin_replay_requests` ledger does not persist either.

## No-Regression Evidence:

Conflict domain: none added. The change is one read on `ingestion_scopes` and
`scope_generations` at the top of the operator refinalize transaction; no lock,
lease, claim, or queue statement changed, and worker and lease settings are not
touched. Frequency: once per operator recovery request, never on a hot path.

Scale shim (throwaway `postgres:16`, EXPLAIN ANALYZE, the exact production
statements rendered from `AffectedGenerationsQuery`): 100,000 scopes and 500,000
generations (96,000 active scopes, 3,000 failed scopes with a failed newest
generation, 1,000 failed scopes with only superseded generations).

| Query | Rows returned | Execution time (3 runs) |
| --- | --- | --- |
| Before (`active` arm only) | 96,000 | 11.1, 12.3, 13.3 ms |
| After, default settings | 100,000 | 111, 115, 160 ms (67 ms of it JIT compilation) |
| After, `jit = off` | 100,000 | 60, 77, 179 ms |

Plan shape after: `Index Scan using ingestion_scopes_pkey` with a `Limit` over a
lateral `Index Scan using scope_generations_scope_latest_lookup_idx`, gated by
`One-Time Filter: (status = 'failed' AND active_generation_id IS NULL)`. The
lateral probe ran for 4,000 loops (the failed scopes) out of 100,000 outer rows
and read 20,000 buffers in total; active scopes pay no `scope_generations` probe.
The cost is a once-per-recovery read that grew from about 12 ms to about 60-110 ms
at 100,000 scopes, 125x the size of the ops-qa estate (794 repository scopes).

Row-set differential in the same database, old covered set versus the new one:
`old_rows = 96000`, `new_covered = 99000`, `old_rows_not_identical_in_new = 0`,
`extra_covered_failed_scopes = 3000`, `new_skipped = 1000`
(`no_recoverable_generation = 1000`). The active arm is identical; the additions
are exactly the seeded recoverable failed scopes.

Behaviour proof against live Postgres (`recovery_refinalize_failed_scope_live_test.go`):
all-scopes and named-scope inclusion of a failed scope through its newest failed
generation, no work item for a superseded generation, all four reset statements on
a never-active generation, claim then ack activating the scope, exact skipped
counts for one scope per reason, the failed-scope activation race (the read is
bound once, so a generation activated mid-refinalize is neither enqueued nor
reset), and a two-call convergence check.

## Observability Evidence:

`eshu_dp_recovery_scopes_skipped_total{reason}` (new counter, closed `reason` set,
no scope ids in labels) is incremented per reason by the admin handler when a
refinalize completes, and the handler logs `recover-generations completed` /
`refinalize completed` with `enqueued`, `generations_retired`, `skipped_total`,
and one `skipped_<reason>` field per non-zero reason, at Warn when anything was
skipped and Info otherwise. At 3 AM an operator with only logs and dashboards can
see a partial rebuild without the HTTP response. Covered by
`TestAdminHandler_RecoverGenerations_EmitsSkippedScopeSignals`. The coverage row is
in `docs/public/observability/telemetry-coverage.md`.

## Not proven here

- A run against ops-qa data. The shim is synthetic and the live tests are fixtures;
  the ops-qa scope `r_874801ea` was not touched.
- Per-scope work item counts for repositories with a very large number of
  non-superseded generations. The lateral stops at the first non-superseded
  generation per failed scope, and a failed scope has only a handful in practice.
- `TestConcurrentRefinalizesSerializeWithoutDeadlock` fails on `origin/main`
  (`fb08c0c657`) in this environment before and after this change, at its second
  refinalize's lock-wait assertion, so it gives no signal on this change.
