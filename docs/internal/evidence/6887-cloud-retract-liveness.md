# #6887 cloud node-retract liveness and delete-shape evidence

## Theory shims (prove-the-theory-first)

Retract-time global live-check, single-uid shape against the parity battery
Postgres (`par6843-battery-postgres-1`, 2043 `reducer_cloud_resource_identity`
rows), `EXPLAIN (ANALYZE, BUFFERS)`:

- Without an expression index the planner uses
  `fact_records_scope_generation_idx` on `fact_kind` and filters the whole
  kind slice on `payload->>'cloud_resource_uid'`: cost 617.39, 6 buffers at
  2k rows. Cost scales with kind cardinality — at 150k rows/kind every
  candidate pays a 150k-row filter on the materialization write path.
- With the partial expression index (`migration 118`,
  `fact_records_cloud_retract_admission_uid_idx` on
  `(payload->>'cloud_resource_uid') WHERE fact_kind =
  'reducer_cloud_resource_identity' AND is_tombstone = FALSE`) the same probe
  is a point Index Scan: cost 12.76, `Index Searches: 1`, 4 buffers,
  independent of kind cardinality. The shim index was dropped after
  measurement; the battery is unmodified.

Graph delete shape against the parity battery NornicDB (`bolt://7687`,
database `nornic`, seeded `CloudResource` nodes), inside a rolled-back
transaction: `CREATE (:CloudResource {uid: 'probe-6887-scratch'})`, then the
shipped `UNWIND $rows AS row MATCH (n:CloudResource {uid: row.uid}) DETACH
DELETE n`, then count → 0, no errors, rolled back. `SHOW INDEXES` on the
battery is empty, so the point-lookup claim rests on the managed-deployment
`cloud_resource_uid_unique` constraint plus the `nornicdb_cloud_resource_uid_lookup`
index — the same lookup the write-path `MERGE (r:CloudResource {uid:
row.uid})` already rides, so the delete costs one MERGE-equivalent lookup per
uid, never a bare-label scan (#6822: bare-label `DETACH DELETE` costs
7.6–9.3 s at 1M nodes even with zero matches).

### Admission-drain fence (review of PR #6892, finding F1)

`reducer_cloud_resource_identity` — the only oracle the cloud-family
live-check reads — is reducer output written by the separate
`cloud_inventory_admission` work item. Nothing orders that item against
another scope's `*_resource_materialization`: `reducerClaimReadinessRequirementsSQL`
gates neither domain, and the projector flips `active_generation_id` in
`Ack` without waiting for the reducer. So a scope whose active generation's
admission has not drained has no admission rows yet, and every uid it still
holds would read dead to another scope's retract. The live tests never saw
it because each seeds the surviving scope's admission rows directly.

Fix, in four parts (rounds 1 and 2 of the post-rebase review):

1. Refuse, never skip. `LiveAdmissionCloudUIDs` returns
   `reducercontract.ErrCloudAdmissionUndrained` (wrapped with
   `scope/generation=status` for the first five) while any active-generation
   `cloud_inventory_admission` work item is nonterminal — `pending`,
   `claimed`, `running`, `retrying`, `failed`, `dead_letter`, the same list
   `generation_liveness_sql.go` uses for the same table. A skip would leak
   forever (a uid absent in G and G+1 is never a candidate again); a
   fallback to the scope's last drained generation misses a uid new in the
   undrained one. Dead-lettered admissions block and name their scope.
2. One statement, one snapshot. The chunk transaction is READ COMMITTED,
   so a fence statement followed by the admission probe would see two
   snapshots and a scope's pointer could flip onto an undrained generation
   between them. `liveAdmissionCloudUIDsFencedSQL` returns the fence rows and
   the admission rows from a single `UNION ALL` statement, so they cannot
   disagree, without REPEATABLE READ and its serialization retries.
3. Cheap refusal. `CloudResourceRetracter.RetractDeadCloudResourceNodes`
   runs `postgres.RequireCloudAdmissionDrained` in its own rolled-back
   transaction before the first per-uid lock, so a routine multi-scope race
   costs one index probe and no 500-lock acquisition on the write path
   (`TestCloudResourceRetracterRefusesBeforeLockingWhenAdmissionUndrained`:
   zero `LockUIDs` calls, zero deletes, one rolled-back transaction).
4. Non-counting deferral. The handler classifies the refusal
   (`reducer.ClassifyCloudRetractError`) as `cloud_admission_not_ready`, a
   `Retryable()` readiness class enrolled in
   `nonCountingReducerRetryFailureClasses`, so the queue retries the intent
   without eroding its attempt budget; an unclassified refusal would have
   counted toward `maxAttempts` and dead-lettered a healthy scope's node
   writes under continuous multi-scope ingest. Pinned by
   `TestReducerQueueFailDefersCloudAdmissionReadinessPastAttemptBudget`
   (AttemptCount 42 past MaxAttempts 3 stays `retrying` in that class),
   `TestEveryReadinessFailureClassIsEnrolled` (the go/ast guard sees the
   class beside its method in `internal/reducer/cloud_resource_retract.go`)
   and `TestReadinessDomainsWithoutAClaimGateAreTheKnownSet` (placed nowhere:
   three domains return it and the awaited condition is deployment-wide, so
   no single-scope CTE row can express it).

The EC2 family needs no fence: `LiveEC2PostureUIDs` reads
`ec2_instance_posture`, a collector-emitted kind that lands with the
generation at ingest. Only the cloud family chose a reducer-derived oracle,
which is exactly why only it needed the fence.

Theory shims (private Postgres 18, rolled-back transactions after `ANALYZE`;
scripts `scratchpad/6887-shim*.sql`, session-local, not committed):

| shape | statement | plan | buffers | time |
| --- | --- | --- | --- | --- |
| 2,000 scopes x 10 generations = 20,000 admission items (19,997 `succeeded`, 2 `pending`, 1 `dead_letter`) | pre-lock fence (`undrainedCloudAdmissionSQL`) | Index Scan `fact_work_items_stage_domain_status_idx`, `status = ANY` as index cond, nested loop on `ingestion_scopes_active_generation_idx` | 17 | 0.075 ms |
| same, plus 200,000 admission facts (100 per active scope); 500 candidates, 250 alive; drained | fenced probe (`liveAdmissionCloudUIDsFencedSQL`) | fence branch 2 buffers; alive branch Seq Scan `ingestion_scopes` -> Nested Loop `fact_records_scope_generation_idx` | 204,079 | 199.8 ms |
| same | original alive-only probe (pre-fence statement) | identical alive plan | 204,077 | 201.3 ms |
| same, 3 undrained | fenced probe | same, 253 rows | — | 123.2 ms |
| same, 2 candidates | alive-only probe | Bitmap Index Scan `fact_records_cloud_retract_admission_uid_idx` (migration 118), hash join over Seq Scan `ingestion_scopes` | 130 | <1 ms |

Reading: the fence adds two buffers to the in-transaction probe and nothing
else; fenced and unfenced statements plan the alive branch identically. The
alive branch itself abandons the migration-118 partial index once the
candidate array reaches lock-chunk size (the planner estimates ~1,000 rows
per array element) and walks the scopes instead: ~200 ms and ~204k buffers
per 500-candidate chunk at 2,000 scopes x 100 admitted facts, held under up
to 500 per-uid advisory locks. That is a property of the probe this PR
already carries, not of the fence, and it bounds the "per-candidate O(1)"
argument below to the index path. Recorded as #6946 with the exact input
shape; not widened into this PR.

Proof: `TestCloudResourceLivenessRefusesUndrainedAdmissionLive` — every
nonterminal status on the active generation returns the sentinel naming the
scope; `succeeded` and `superseded` answer normally and the uid admitted
only by the superseded generation reads dead; a nonterminal item on a
non-active generation does not block. RED before the fence (undefined
sentinel, then dead read), GREEN after.

Operator signal: the refusal lands as `failure_class =
cloud_admission_not_ready` on the retrying work item with the sentinel text
(`scope/generation=status`) in `failure_message`, and in the reducer's
handler-failure log; no new metric. A scope stuck in `dead_letter` is
already the operator's queue alarm and now also names itself in every
deferred retract.

### #6946: keep the live probe on the partial index at chunk size

Cause. Postgres does not use a partial index's expression statistics for
planning, so `payload->>'cloud_resource_uid' = ANY($1)` gets the default
selectivity per array element: 2,000 rows per candidate on this seed, so a
500-candidate lock chunk looks like most of the table and the planner walks
`ingestion_scopes` into `fact_records_scope_generation_idx` instead of
probing the migration-118 partial index. It holds in both plan modes. The
retracter calls the probe through pgx's default
`QueryExecModeCacheStatement` (no override in-tree), so after five
executions per pooled connection PostgreSQL compares the generic plan's cost
(53.79) with the custom plan's (1,350.58) and settles on the generic plan;
the generic walk is the worse one, because it probes the index once per
scope with the array unknown.

Change: migration `119_cloud_resource_retract_liveness_stats.sql` only,
`CREATE STATISTICS ... ON ((payload->>'cloud_resource_uid'))` plus
`ANALYZE fact_records`. The probe SQL is unchanged.

Rejected hypotheses, each measured in a rolled-back transaction on the
private PostgreSQL 18 stack (2,000 scopes, a superseded and an active
generation each, 100 admitted uids per generation, 400,000
`reducer_cloud_resource_identity` rows): an `unnest`-driven join instead of
`= ANY` (14,060 buffers, no change); a MATERIALIZED CTE fetching the
candidate slice first without statistics (`Seq Scan` inside the CTE,
12,078 buffers); the same CTE with the statistics present (planned
identically to the inline read in both modes: 1,279 / 1,279 buffers
custom, 2,467 / 2,467 generic, shim `6946-shim5`), so it was dropped from
the change rather than shipped on a self-restating text guard. Migration
103's note that `CREATE STATISTICS` did not move a multi-column join
estimate is a different case: this is a single-expression equality whose
only missing input is the expression's own ndistinct/MCV.

Proof: `TestCloudResourceLivenessProbePlanStaysOnIndexAtChunkSizeLive`
seeds the shape above, `VACUUM (ANALYZE)`s, derives the alive branch from
the shipped fenced probe at its `UNION ALL` boundary, and for each of
`force_custom_plan` and `force_generic_plan` PREPAREs it on a dedicated
session and explains an `EXECUTE` of it (an EXPLAIN of the bare
parameterized text is planned with the values in hand and never shows the
generic plan). Each arm requires the partial index, no
`fact_records_scope_generation_idx` scan, and at most 5,000 shared buffers
on the root node (hit + read + dirtied + written; an unparseable root
`Buffers:` line fails the arm instead of grading a child node).

| plan mode | before (no statistics) | after (migration 119) |
| --- | --- | --- |
| custom | scope walk, 37.7 ms in the RED run; 14,115-14,619 buffers / 39.6 ms on the shim seed | Hash Join or Merge Join over the partial index, 1,423-1,682 buffers, ~1.5 ms |
| generic | scope walk, 733 ms in the RED run; 14,142 buffers / 580 ms on the shim seed with 800k dead tuples (a separate review measured 14,752 / 822 ms) | Nested Loop over the partial index and `ingestion_scopes_active_generation_idx`, 2,479-3,021 buffers, ~1 ms |

RED: with the statistics object dropped both arms fail with "did not use
the partial uid index". GREEN after applying the migration file: two
two-arm runs (custom / generic) at 1,682 / 3,021 and 1,423 / 2,479
buffers; five earlier custom-only runs at 1,700 / 1,393 / 1,393 / 1,693 /
1,680. Buffer counts move with heap state (the test's `VACUUM (ANALYZE)`
is table-wide and the private stack is not hermetic), which is why the
budget sits far above the observed band and far below the walk. The
liveness live tests and the postgres unit suite pass on the branch.

The real migrator applies it: with the ledger row cleared and the object
dropped, `docker compose run db-migrate` on the private stack recorded
migration 119 and left `fact_records_cloud_retract_admission_uid_stats`
analyzed (`pg_stats_ext_exprs` n_distinct -0.38, MCV present). Editing an
already-applied migration file is not an option for a boundary note: the
ledger checksums the whole file, comments included, and every existing
database would refuse bootstrap.

Migration 118's header records the same walk at "~204k buffers per chunk".
That figure is the #6892 shim row above (2,000 scopes x 10 generations,
200,000 admission facts, the since-destroyed `wt6887` stack); on this seed
the same plan shape costs 14,115-14,619 buffers on a vacuumed heap. The
walk fetches one heap page per matching fact per scope, so its cost tracks
heap layout and size; the index path's does not, which is the point of the
fix. 118's text stays as written because of the ledger.

`ANALYZE fact_records` samples 30,000 rows regardless of table size
(`default_statistics_target` 100 x 300) and took 326-328 ms on the
400,000-row seed with 800,000 dead tuples, so the migration's duration is
bounded by the sample, not the heap. It runs inside the migrator's 5 s
bootstrap ownership window, whose `lock_timeout` bounds lock acquisition
only and whose 55P03 handling hard-fails; that is a pre-existing migrator
property that migration 118's index build already sat inside, filed as
#6956.

Performance Evidence: before/after on the same seed and host, 500-candidate probe, custom plan 14,115-14,619 buffers / 37.7-39.6 ms -> 1,423-1,682 buffers / ~1.5 ms; generic plan (production steady state) 14,142-14,752 buffers / 580-822 ms -> 2,479-3,021 buffers / ~1 ms. Alive row set unchanged (250 of 500), SQL unchanged. ANALYZE cost 326-328 ms once at migration, then autovacuum.

No-Observability-Change: no new metrics, spans, or log keys; the probe's
row counts and the per-chunk retract log are unchanged.
