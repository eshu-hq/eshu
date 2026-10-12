# Reconcile Sweep Stores No Duplicate Generation (#7978)

The reconcile sweep forced a full snapshot of every git scope once per
interval (24 hours by default). The fact builder blanked the freshness hint for
that snapshot, so the ingestion store's unchanged-generation check could not
match it. The store then wrote a new `scope_generations` row and every fact
row, and the reducer rebuilt documents and terms for the same content. On the
QA environment, 23,290 of the 23,547 same-commit generations carried an empty
hint.

## Contract

A snapshot is a duplicate when its scope, `source_commit_sha`, and freshness
hint equal those of the scope's newest `pending` or `active` generation. The
git hint is v3. It folds in `GenerationContentVersion` (the hand-bumped
`gitGenerationContentVersion` plus a digest of the fact-schema registry) and the
default branch. It is never blank. A duplicate stores no generation, fact,
document, or term row.

| Snapshot | Store action | `refresh_skip_reason` |
| --- | --- | --- |
| Duplicate, not a reconcile, match is `active` | drain the stream, store nothing | `unchanged` |
| Duplicate of a `pending` generation with live projector work (rulings a9-120, a9-126) | drain, store nothing | `pending_same_identity` |
| Duplicate reconcile, match is `active` | re-open the active generation's projector work | `reconcile_reprojected` |
| Same, work row already pending or under a live lease | no re-open, no stamp | `reconcile_reprojection_pending` |
| Same, a newer generation activated before the scope lock | drop the snapshot | `reconcile_superseded` |
| Different hint, commit, or version; `DefaultBranchChanged`; reason `reconcile_request_stale`; a `failed` match; or a `pending` match whose work is missing or terminal (ruling a9-126) | store a generation | none |

The re-open mirrors the #7209 zombie heal: the same canonical `work_item_id`,
`ON CONFLICT DO UPDATE`, and the in-flight guard. Ruling a9-117 changes two
things. It resets `attempt_count` to 0 when the row belongs to a run that
ended: succeeded, failed, or dead_letter (PR #7997 review; a dead_letter row
there comes from a #7209 heal or liveness re-drive). An expired lease keeps its
count. Each re-open still gets at most the projector's max attempts, and a
re-opened run that dead-letters leads to `reconcile_request_stale` and a
stored generation, not a second re-open
(`TestReconcileReopenOfDeadLetterIsBoundedLive`). A monotonic
`payload.reconcile_reopen_count` stays for operators. It stamps
`reconcile_reason` and `reconcile_requested_at` in the generation payload only
when the row was really re-opened. The statement runs after the scope row lock,
which is the first lock every blocking path takes, and it is fenced on the
generation still being the scope's active generation with the same hint and
commit.

The lifecycle after the re-open:

- Ack of the re-projection stamps `reconcile_projected_at`; `activated_at`
  keeps the original activation.
- `fullReconcileStateQuery` reads the three stamps only while the generation is
  `active`. `decide` holds a request without an Ack for one interval
  (`reconcile_in_flight`). After one interval, or at once when the
  re-projection was dead-lettered, it forces `reconcile_request_stale`, and the
  store stores that snapshot as a new generation. The stale path goes through
  the shared throttle, so while that full is pending or backing off the scope
  is held, and a second stale snapshot that equals the pending full stores
  nothing: at most one stored generation per interval per scope
  (`TestReconcilePolicyStaleRequestForcesOncePerInterval`,
  `TestStaleReconcileSkipsPendingSameIdentityLive`).
- `failProjectorWorkQuery` never marks an activated generation `failed` and
  never clears the scope pointer. It stamps `reconcile_dead_lettered_at`
  instead.

The re-projection retracts drift that carries another generation id. Because
the re-open resets `attempt_count`, the projector service's `attempt_count > 1`
rule no longer applies. The claim reports an activated generation as having a
prior generation, which keeps canonical retraction on: without that claim
change, the Neo4j proof below keeps the planted node. The re-projection does NOT retract drift stamped with the
active generation id itself, because canonical retraction filters on
`generation_id <> $current`. The old behaviour stored a new generation id, so
it did retract that drift.

## Performance Evidence

Performance Evidence: measured at the commit titled "docs: name refinalize on the drift counter and the
liveness probe key (R3-3, R3-4)", the last commit that changes Go code on this
branch (comments only); later commits change only docs and one script. Commits
are cited by subject because a rebase gives every commit a new SHA. It was measured on base
`47ea898275`, before the rebase onto `d632a089f7` (tree-identical for this
branch's files, patch-id equal). The measured path includes the a9-126 `work_live` probe on the candidate read. The
backend is a private local PostgreSQL 18.6 cluster (Homebrew) on a shared host.
The host was CONTENDED: load1 was 31.6-33.2 before and after every run, with
another session's heavy jobs on it. `TestUnchangedReconcileStoresNothingLive`,
with its fixture set to 6,800 facts for this run only, measures the store
commit time through the production `IngestionStore` and `ProjectorQueue`. The
"before" column is the same reconcile with the blank hint that main's
collector sends, in the same process. Each figure is a single-process wall
time, so treat it as an order-of-magnitude result.

| facts per generation | fixed reconcile | blank-hint reconcile (before) | rows written by the fixed reconcile |
| ---: | --- | --- | --- |
| 6,800 | 1.38-1.94 ms (3 runs) | 545-606 ms (3 runs) | no new row in the six counted tables (generation, fact, search document, search term, content, work); the active generation's projector work row is re-opened in place |

The before path wrote 1 generation row, 6,800 fact rows, and 1 work row in each
run. An earlier measurement at the commit titled "telemetry, collector/git: size the skip
counters from the reason set and document hint scope", before the `work_live` probe and
on a load of about 28, gave 1.03-1.15 ms against 308-366 ms.

The independent review measured 1.36 ms against 13.0 ms at 200 facts on its
own PostgreSQL 18.6 instance, and 1.74 ms against 12.2 ms at the commit titled "collector/git, storage: hold an
expired pending full with live work (ruling a9-126)".

The reducer then rebuilds documents and terms for the before path; that cost
was not measured here. The collector still parses the repository for a reconcile, because the
content hint needs parse output. This change does not save the parse.

Plans: generic plans (`plan_cache_mode = force_generic_plan`) with `EXPLAIN
(ANALYZE, BUFFERS)` on 35,001 seeded generations across 800 scopes. One scope
holds 3,040 of them. The query text came from the Go constants.

| statement | plan | execution |
| --- | --- | ---: |
| candidate read | `scope_generations_scope_idx`, then a top-N in-memory Sort (1 row, 25 kB), LIMIT 1 | 0.034-0.050 ms |
| full reconcile state (LATERAL, payload stamps) | `scope_generations_scope_latest_lookup_idx` x2 | 0.060-0.093 ms |
| re-open (in `BEGIN`/`ROLLBACK`) | pkey, `scope_generations_active_scope_idx`, `ingestion_scopes_active_generation_idx` | 0.220-2.109 ms |
| Ack activate with the projected stamp (in `BEGIN`/`ROLLBACK`) | pkey | 0.053-0.066 ms |
| dead letter with the activated guard (in `BEGIN`/`ROLLBACK`) | existing plan, no owned row | 0.075-0.106 ms |

No plan used a sequential scan. The candidate read sorts its scope's rows in
memory (quicksort, 25 kB) to take the newest; that is the same plan as the
`activeGenerationFreshnessQuery` it replaces. The projector claim change adds one
expression to the SELECT list over the already-joined generation row. Its plan
was not re-measured.

Wedged pending full (ruling a9-126). Before the hold, a full generation pending
for longer than `Interval` at the remote head was forced every cycle, parsed,
and then skipped by the store as `pending_same_identity`; each forced scope
also took a per-cycle budget slot. `TestWedgedPendingFullHoldsWithoutParsing`
counts the selected (parsed) repositories over three cycles: 3 with the hold
removed, 0 with it. `TestWedgedPendingFullDoesNotSpendTheBudget` shows a held
scope no longer takes the only slot, so a later overdue scope is forced in the
same cycle.

The hold reads the newest full's commit and whether its projector work row is
`pending`, `retrying`, `claimed`, or `running` (the claim pool's admissible
set). Both reads probe the canonical `work_item_id`
(`'projector_' || scope_id || '_' || generation_id`) on `fact_work_items_pkey`
for the single row the outer `LIMIT 1` keeps. Generic plans on the same 35,001
generations, three runs each:

| statement | scope with 3,040 generations | scope with about 40 generations |
| --- | ---: | ---: |
| candidate read with `work_live` | 0.028-0.543 ms | 0.021-0.336 ms |
| full reconcile state with the newest full's commit and liveness | 0.030-0.058 ms | 0.035-0.050 ms |

A first shape that filtered `fact_work_items` by `scope_id`, `generation_id`,
and `status` used `fact_work_items_scope_generation_idx` and took
1.27-3.58 ms on the large scope (32 buffers). It was rejected for the
primary-key probe. The first run of each shape is cold.

## Differences From The Old Path

The old path stored a new generation for every forced reconcile. The new path
re-projects the active generation instead, which differs in two ways:

1. Graph state stamped with the active generation id itself is not retracted
   (ruling a9-117 Q1); only a projector defect or a manual graph edit can
   create it. `TestUnchangedReconcileKnownGapActiveStampedDriftLive` pins it.
2. Reducer-owned truth is not re-derived: the reducer queue admits none of the
   re-projected generation's intents (`ON CONFLICT DO NOTHING`, #7285), so
   reducer-domain drift is not retracted by a reconcile.
Review F5 is closed without a claim change: `canonical.BuildMaterialization`
marks a reconciliation when the claimed generation is already `active` (a
re-projection; a first projection arrives `pending`), so
`eshu_dp_reconciliation_drift_retractions_total` counts the re-projection's
retractions. `TestBuildMaterializationMarksReprojectionOfActiveGeneration`
pins the cells; the Neo4j proof reads the counter: 0 after the first
projection, at least 1 after the re-projection retracts the planted node, and
0 without the change.

An explicit reindex is not a difference: it still stores the re-parse (ruling
a9-125). A pending watermark is decided before the sweep and `graph_dirty`, and
only a stored full (`FullReconcileState.LastStoredFullAt`) satisfies it, never a
re-projection Ack (review r2 N1; `TestReindexWatermarkPrecedence`,
`TestReindexAfterReprojectionStoresLive`).

## Concurrency Evidence

`TestReconcileReopenLosesLockRaceToNewerActivationLive` holds the scope row in
a transaction that supersedes the active generation and activates a newer one.
The reconcile reads the old generation as a match. It then waits on the scope
lock, which the test observes in `pg_stat_activity`. After the other
transaction commits, the reconcile drops the snapshot with
`reconcile_superseded` and writes nothing.

`TestReconcileReprojectionSupersededBeforeClaimIsNoopLive` re-opens the work
and activates a newer delta first. The claim sweep then retires the re-opened
row as `superseded`, and the stamp stops counting.

A repeated reconcile while the re-open is pending writes nothing and reports
`reconcile_reprojection_pending`.

A re-open while the active generation's row is under a live lease leaves the
lease, the attempt count, and the stamps alone
(`TestReconcileReopenSkipsLiveLeaseWithoutStampLive`). A dead-lettered
re-projection keeps the generation and its scope pointer active
(`TestReconcileReprojectionDeadLetterKeepsActiveGenerationLive`). A snapshot
equal to a pending generation stores nothing, and the same snapshot against a
failed generation stores one (`TestPendingSameIdentitySkipsAndFailedStoresLive`).

A second projection of the same generation re-emits its reducer and shared
intents. `TestReducerQueueReprojectionReEnqueueAdmitsNothingLive` shows that
the reducer queue admits none of them (`ON CONFLICT DO NOTHING`), and
`TestSharedIntentUpsertCreatedAtLastWriterWinsLive` shows that shared intents
upsert in place on a stable `intent_id` and never reopen `completed_at`. Both
pass on PostgreSQL 18.6. The Neo4j proof fixture emits no reducer or shared
intents (0 rows before and after), so it does not cover this part.

## Content Version Gate

`TestGenerationContentVersionPinsCollectorOutput` parses the 44-file
`content_version` fixture directory of the git collector package through the
production snapshotter and
fact builder. It pins a digest of the 238 normalized facts next to
`gitGenerationContentVersion` in `testdata/content_version_pin.json`. When the
output moves while the constant stays the same, the test fails and tells the
author to bump the constant. Seeded violation: appending one Python function to
the fixture turned it RED, and restoring the file turned it GREEN.
`TestContentVersionFixtureDigestIsDeterministic` guards the gate:
content-entity ids (which hash the temp checkout path) and `indexed_at` are
normalized out.

`TestContentVersionFixtureCoversEveryParser` (review F2) enumerates
`parser.DefaultRegistry().ParserKeys()` (35 keys) and fails when the fixture
has no file for one. Seeded violation: removing `jvm/pom.xml` failed it with
`[maven]`; restoring it passed. YAML is covered by Kubernetes, Helm, Argo CD,
CloudFormation, and GitHub workflow files.

The gate is a sample of inputs, so it narrows, but cannot close, the window
where a parser change does not move the hint (ruling a9-125, #7986). Coverage
is per parser key, not per extension or YAML/JSON dialect (review r2 N5), and
the pin test cannot tell fixture growth from a parser change in the same edit
(N4): review catches that case. An explicit reindex stores the re-parse.

## Graph Truth Evidence

`TestUnchangedReconcileKnownGapActiveStampedDriftLive` documents the accepted
gap: a `File` node stamped with the active generation id survives the
re-projection. If a later change retracts that class too, the test fails and
must be inverted.

The explicit-mode Neo4j proofs run on `neo4j:2026-community` (pinned digest)
with the production projector service and canonical writer:

- `TestDeltaRefusedAtAckLeavesOverlayLive`: the #7389 `graph_dirty` heal at the
  active commit re-projects the active generation, stores no generation, and
  retracts the refused writer's overlay.
- `TestUnchangedReconcileRetractsPlantedDriftLive`: an `interval_elapsed`
  reconcile on a one-generation scope stores no generation and retracts a
  planted `File` node. The scope then reads `fresh`.

## Observability Evidence

Observability Evidence: `telemetry.RecordSkippedRefresh(reason)` counts each
skip under one value of the closed `telemetry.RefreshSkipReasons` set.
`/metrics` exposes `eshu_runtime_refresh_skipped_by_reason_total{service_name,reason}`
next to the existing total.

The skip log line `ingestion refresh skipped` carries `refresh_skipped`,
`refresh_skip_reason`, the scope, the matched generation, and
`reconcile_reason`. The caller's span gets an `ingestion.refresh_skipped` event.

A stored reconcile generation records `reconcile_reason` in
`scope_generations.payload`, and a stored default-branch generation records
`default_branch_changed`.
