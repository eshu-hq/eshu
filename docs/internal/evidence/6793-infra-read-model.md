# #6793 Infra Resource Aggregate Read Model

`GET /api/v0/infra/resources/count` and `/inventory` (and the MCP tools
`count_infra_resources` / `get_infra_resource_inventory`) returned HTTP 504 on
a production-scale instance. This note records the diagnosis, the parity
proof, and the before/after measurements for the Postgres read model
(`infra_resource_entities`, migration 109, package
`go/internal/storage/postgres/infra/inventory`).

Backends: NornicDB v1.3.3 (the deployed graph), PostgreSQL 18.

## Diagnosis

- Every graph aggregate over the infra labels loads every node of each label.
  A cold `MATCH (n:L) RETURN count(*)` costs roughly 40-45µs per node on the
  production instance, with no sub-linear path: `count(*)`, uid-index
  predicates, uid ranges, and `STARTS WITH` all measured the same as a bare
  scan. `/count` issued four such passes and `/inventory` one, so each route
  exceeded the 10s graph-read budget and returned 504.
- NornicDB v1.3.3 has an O(1) persisted label count (`NodeCountByLabel`,
  `pkg/cypher/match.go`), but it is dead on the read path: the executor's
  `transactionStorageWrapper` forwards `NodeCount()` and not
  `NodeCountByLabel`, so the fast path's type assertion fails over both Bolt
  and HTTP. A CPU profile of a cold count shows
  `executeMatch -> collectNodesWithStreaming -> transactionStorageWrapper.GetNodesByLabel`.
  It would only help unfiltered totals, not the grouped rollups, so it is not
  the fix.
- Parallel per-label fan-out did not reach the budget either; large labels
  slowed under contention.

## Parity

Performance Evidence: parity of the read-model answer against the legacy
all-graph answer, on the production-scale instance, read-only. The legacy side
ran the exact production Cypher (all four count passes and all five inventory
dimensions) with a long client timeout. The read-model side ran the same
derivation SQL the content writer uses, over the live `content_entities`,
plus the graph pass. Result: 0 differences in `total_resources`,
`by_provider`, `by_environment`, `by_label`, and every bucket of the
provider, environment, resource_category, resource_service, and label
inventory dimensions.

Supporting checks, all read-only on the same instance:

- For every one of the 25 content-derived labels across 9 dimensions, the
  graph value distribution equals the trimmed `content_entities.metadata`
  distribution: 0 mismatches. The canonical node writer promotes metadata
  verbatim, trimmed, with empty strings dropped
  (`storage/cypher/canonical_node_writer_metadata.go`).
- The per-label graph count equals the `content_entities` count for every
  content-derived label. TerraformModule and TerraformOutput differ only by
  the Terraform state projector's nodes (`evidence_source =
  'projector/tfstate'`), which the graph read adds back through an indexed
  seek. CloudResource and TerraformStateResource have no content rows and are
  read from the graph.

## Route latency

Performance Evidence: interleaved A/B, six samples per side. Source: a build
of this branch before the evidence_source change; main side: the main build
of the same day. Both arms read the same production-scale NornicDB. The
Postgres side is NOT comparable: main reads the production Postgres, while the
patched API reads a local PostgreSQL 18 seeded to the production per-label and
per-dimension distribution (the read model cannot be created on production
without a deploy). The comparison therefore shows the route shape, not an
absolute production number.

| Route | main | patched (read model + graph pass) |
| --- | --- | --- |
| `/infra/resources/count` | 6/6 HTTP 504 at the 10s budget | 6/6 HTTP 200, median 2.21s, max 2.97s |
| `/infra/resources/inventory?group_by=provider` | 504 until NornicDB's result cache warmed | 6/6 HTTP 200, median 1.17s, max 3.25s |

With the graph pass isolated, the table reads take 11-23ms cold or warm
(parallel seq scan of a narrow table). The remaining time was the graph pass,
and most of that pass was the whole-label TerraformModule/TerraformOutput
scan. The evidence_source change below removes it.

When the patched binary runs against a database without migration 109, the
routes behave exactly like main (graph path, identical bodies), and the
backfill writes nothing. A deploy that lands the API before the migration is
therefore harmless.

## evidence_source indexes

Performance Evidence: local NornicDB v1.3.3, seeded with the production shape
of TerraformModule and TerraformOutput (content-derived nodes plus a handful of
state-projector nodes). The query was the production `CALL { ... UNION ALL ...
}` branch shape with `WHERE n.evidence_source = $graph_writer_evidence_source`,
cold with unique aliases:

- without the indexes: 0.28-0.80s per query;
- with `tf_module_evidence_source` / `tf_output_evidence_source`: 0.003-0.016s,
  with identical row counts;
- an 8s CPU profile of the indexed loop ran 4,650 queries through
  `tryCollectNodesFromPropertyIndex`; the unindexed loop ran 6 queries, 65% of
  CPU in `GetNodesByLabel`. A bound parameter and a literal seek alike.

No-Regression Evidence: writer throughput with the production canonical upsert
shape (`UNWIND $rows AS row MERGE (n:L {uid: row.entity_id}) SET n +=
row.props`, 500-row batches), 6 rounds each way in alternating order. Creates:
median 281 rows/s without the indexes against 253 with them. Changed-value
updates: 47 against 69. No-op updates: 14.2k against 13.6k. The run-to-run
spread (about ±40%) exceeds any index effect, and the sign flips between
batches.

## Projector cost

No-Regression Evidence: one fixture repository with 20,000 infra entities over
3,500 files and 30,000 non-infra entities over 5,000 files (8,500 touched
paths), through the real `ContentWriter.Write` on PostgreSQL 18:
`derive_infra_inventory` took 0.68s and 0.79s of a 4.49s and 3.94s Write
(upsert_files about 0.95s, upsert_entities 1.45-2.28s, reap 0.56-0.69s). That
is about 15-20% of the content stage for a full generation. A 50-path delta
derive took 13-28ms. The standalone probe is
`TestMirrorPathsLiveLargeRepositoryCost`.

The startup backfill of a table seeded to production shape took 5.2s on an idle
PostgreSQL 18 and runs in the background.

## Retention lock

No-Regression Evidence: `TestRetentionLiveLockHoldAndDeriveWaitCost` drives
the real `PruneSupersededGenerations` over one batch of 10 superseded
generations (50,000 content_entity facts, all infra rows of one repository) on
an idle local PostgreSQL 18, three rounds:

| Round | retention tx | derive lock held | same-repo 1-path derive (idle baseline) | unrelated-repo derive |
| --- | --- | --- | --- | --- |
| 1 | 2.41s | 1.18s | 1.10s (18ms) | 0.9ms |
| 2 | 2.30s | 1.10s | 1.16s (14ms) | 2.2ms |
| 3 | 4.89s | 3.74s | 1.16s (15ms) | 0.9ms |

Retention holds each affected repository's derive lock from just before the
content_entities prune until its commit (through the content_files prune and
the scope_generations delete). A projector derive of that repository waits
for that window, about 1s per 50,000 pruned facts here; other repositories
are not blocked. This is a bounded stall per retention batch, not a
deadlock: derive holds at most one repository lock and retention acquires its
set in repo_id order.

The probe could not run on a plain bootstrapped schema.
`generationRetentionRowCountsQuery` (on main before this change) joins a
relation `iac_reachability` and counts `content_file_references.reference_id`.
Neither exists in the bootstrap migrations, which create `iac_reachability_rows`
and no `reference_id` column. Every retention batch with candidates therefore
fails at its row count on a real schema. The measurement database added a view
and a surrogate column to get past that; the probe skips when the relation is
missing. That defect predates this change and is tracked as #6809.

## Concurrency

Live PostgreSQL proofs, run with `-race`, in
`go/internal/storage/postgres/infra/inventory`:

- duplicate delivery;
- a delta after a full generation;
- a file tombstone;
- an empty fresh set for a path;
- an entity that moves paths;
- a tombstone that names a stale path;
- the backfill converging a whole repository;
- retention's orphan cleanup scoped to the affected repositories;
- a derive of the same repository racing retention, which blocks until
  retention commits and cannot resurrect the pruned row (mutation-checked: a
  no-op lock fails the test);
- a failed backfill leaving no marker, and the background loop retrying until
  the marker is recorded.

Observability Evidence:

- `eshu_dp_infra_inventory_reads_total{route,source}` shows whether the table
  or the graph served each read.
- `eshu_dp_infra_inventory_derives_total{outcome}` counts ok,
  skipped_not_installed, and error derives.
- `eshu_dp_infra_inventory_backfill_runs_total{outcome}` counts completed,
  already_complete, and failed backfill attempts.
- The content-writer stage log `derive_infra_inventory` (path count, rows
  deleted and inserted, duration) and the events
  `infra_inventory.derive.skipped_not_installed`, `infra_inventory.backfill`,
  and `infra_inventory.backfill.failed`.
- Every statement is timed by `eshu_dp_postgres_query_duration_seconds`
  through `InstrumentedDB`.
- Responses carry `truth.basis`: hybrid (table plus graph), content_index
  (table only), or authoritative_graph.

## After the evidence_source change

Performance Evidence: the same shape A/B, a build of this branch with the
evidence_source change, against the same production-scale NornicDB. The production graph does not have the new
indexes yet: bootstrap creates them at deploy, and graph DDL is not run
against production outside a deploy. The Module/Output branch is therefore
still a filtered label scan there.

- Patched routes, first call of the new process: count 0.81s, inventory by
  provider 0.67s, inventory by label 0.69s. All HTTP 200, truth basis hybrid,
  level derived. These are NOT cold figures: the route's graph pass is fixed
  query text, and NornicDB's result and data caches were not controlled (the
  same text had run in earlier sessions), so they were at least partly warm.
  They are faster than the cold graph pass the route contains (next bullet),
  which a cold route cannot be. Repeat calls return in about 45-55ms from the
  query-text cache. Do not budget from these numbers.
- The new graph pass alone, six cold runs with unique aliases (which defeat
  the query-text cache): 1.11-1.27s without the indexes. This is the figure
  to budget from: before deploy a cold unscoped count costs about this graph
  pass plus the 11-23ms table read. Its whole-label part (CloudResource and
  TerraformStateResource) takes 0.17-0.19s cold, including network round trip.
  With the indexes the Module/Output part becomes a seek (see the index section
  above), so the expected cold graph pass after deploy is about 0.2-0.3s. That
  post-deploy figure is a projection from the local index shim, not a
  production measurement.
- The main build answered HTTP 401 in this run because of a harness credential
  change, so the main side of this A/B is the earlier run: 6/6 HTTP 504 at the
  10s budget, which matches the production sweep.

## Drift reconcile

The backfill marker means "every repository was derived once". It does not
mean the table stays equal to `content_entities`. A content writer that does
not derive leaves rows behind that the marker never notices: an older
ingester, projector, or bootstrap-index binary during a rolling upgrade, a
manual SQL change, or a restore. The next Write repairs only the paths it
touches, so a deleted file's rows can stay forever.

The reducer now runs a reconcile loop (`InfraInventoryReconcileRunner`,
storage `inventory.ReconcileCycle`). Each cycle checks up to
`ESHU_INFRA_INVENTORY_RECONCILE_REPO_BUDGET` repositories (default 500) in
`repo_id` order, then waits `ESHU_INFRA_INVENTORY_RECONCILE_INTERVAL`
(default 5m). The walk is a shared work queue: its position lives in one row
of `infra_resource_entity_reconcile_cursor`, and each cycle claims its page
(`inventory.ClaimPage`) in a short transaction that locks that row, lists the
next page after the stored position, and advances the position past it. A
page shorter than the budget reached the end and resets the position, so the
next claim starts over. Replicas that claim at the same moment serialize on
the row lock for the few milliseconds of the page listing and take disjoint
pages, and a restarted process resumes where the walk stands instead of
re-checking the low end of the ordering or enumerating the corpus to choose a
start. Per repository it compares
(row count, sum of a per-row `hashtextextended` over every derived column) of
the infra-typed content rows against the table rows. A match writes nothing
and takes no lock.

A single mismatch is not treated as drift. `ContentWriter.Write` commits its
content rows (autocommit, outside the derive lock) before its derive runs,
so a check that lands between the two sees the table behind even though
nothing is wrong. The advisory lock orders the reconcile only against derive
transactions, not against those content statements. The walk therefore
records a first mismatch as `suspect` and writes nothing. The next cycle, one
interval later, re-checks the suspects first; one that still differs is
re-checked under the repository's derive lock and, if it still differs,
re-derived in the same transaction with `MirrorRepo`'s delete and insert
(`repaired`). A Write's content-to-derive gap is seconds, far shorter than
the interval. Suspect re-checks count against the cycle budget. Nothing runs
until the backfill marker exists.
`TestReconcileCycleLiveDoesNotCountAnInFlightWriteAsRepaired` commits content
rows without deriving, asserts `suspect` with the table untouched, runs the
derive, and asserts the next cycle's re-check is a `match`; making the walk
repair on its first mismatch fails it.

Why this option: it converges from every cause of drift, including ones a
writer-side gate cannot see (manual SQL, restores, future writer bugs), and
it reuses the idempotent, lock-safe `MirrorRepo`. The cost is a stale window.
For a reducer process that runs uninterrupted it is at most one full walk
plus one interval: a walk visits every repository in
(repositories / budget) cycles, and a suspect is repaired on the following
cycle. Because every claim advances the one stored position, a full walk
takes (repositories / budget) claims in total across all replicas, however
often any of them restarts; N replicas walk it N times faster. Suspects are
kept in memory by the replica that found them, so a restart between a
suspect and its re-check delays that repair by one walk.
`TestReconcileCycleLivePersistsAndResumesTheWalkCursor` proves a fresh
process resumes after the stored position, and
`TestReconcileCycleLiveReplicasClaimDisjointPages` runs two replicas at once
over five repositories with a budget of two: every round's pages are
disjoint, two rounds cover every repository, and exactly one claim wraps.
`TestReconcileRepositoriesLivePageStopsAtBudget` proves from EXPLAIN ANALYZE
that a walk page stops at the budget: with 400 repositories and a budget of
10, each recursive skip scan produced at most 11 rows, and removing a side's
inner `LIMIT` makes it produce 401.

Performance Evidence: shim on a local PostgreSQL 18 seeded to a
production-like shape: 301 repositories (one with 50,000 content rows of
which 20,000 infra, 20 with 8,000 of which 3,000, 279 with 1,800 of which
300), all derived into the table. `EXPLAIN (ANALYZE, BUFFERS)` of the digest
pair, first run after a restart and then warm:

| Repository | content digest | table digest |
| --- | --- | --- |
| large | 16.9 / 15.8 ms (BitmapAnd of the repo_id and entity_type indexes) | 6.7 / 5.7 ms (repo_path index) |
| medium | 5.6 / 5.3 ms | 1.1 / 0.9 ms |
| small | 1.0 / 0.5 ms (repo_id index scan, type filter) | 0.15 / 0.12 ms |

Both sides are bounded by `repo_id`. For larger repositories the planner also
bitmap-scans the whole `entity_type` index (about 3-4 ms here), a cost that
grows with the corpus's infra row count. Forcing a repo_id-only plan measured
the same full-walk time, so no new index is justified. Full walk of all 301
repositories through the real per-repository check (every repository matched, so
this is the unlocked digest path the walk runs): 0.46-0.67s, slowest single
repository 30-44 ms, 0 mismatches. The per-cycle repository listing uses a
recursive skip scan over each table's `repo_id` index: 2.5 ms for a 100-repo
page, against 171 ms for a `UNION`/`DISTINCT` over every infra row. The
earlier random start listed every repository through the same scan to pick
one: on a second local PostgreSQL 18 shim with 20,000 repositories, that
listing took 260 ms and returned 20,001 ids to the process, against 1.8 ms for
a 100-repo page. The claimed page replaces it: a primary-key row lock plus
one page listing.

Not built: the proposed "skip repositories whose max
`content_entities.indexed_at` predates the last reconcile" watermark. There
is no `(repo_id, indexed_at)` index, so it reads every row of the repository
(17.4 / 8.7 ms for the large one), about the cost of the digest it would
skip. It is also unsound: a writer that only deletes content rows leaves
`max(indexed_at)` unchanged, and that is exactly the drift this loop exists
to catch. An unchanged repository costs one digest pair and records `match`.

Concurrency: the reconcile holds at most one repository lock per transaction,
like the derive and the backfill, and retention takes its set in `repo_id`
order, so no cycle can form. `TestReconcileRepoLiveConcurrentDeriveDoesNotDeadlock`
races a reconcile against ten derives of the same repository while a third
transaction holds the lock, under `-race`, and the table ends exact.

Observability Evidence: `eshu_dp_infra_inventory_reconcile_total{outcome}`
(`match`, `suspect`, `repaired`, `error`; `error` also counts a failed cycle,
but never a clean shutdown), `eshu_dp_infra_inventory_reconcile_duration_seconds`
(failed cycles included), span `reducer.infra_inventory_reconcile` (repos
checked, suspect, repaired, failed, walk wrapped), logs `infra_inventory.reconcile.drift` (repo_id, content and
table row counts), `infra_inventory.reconcile.failed`, and
`infra_inventory.reconcile.cycle_failed`.

## Rolling-upgrade fence

The reconcile alone left a stale window during a rolling upgrade: a new API
can record the backfill marker while an older ingester, projector, or
bootstrap-index binary still writes `content_entities` without deriving, and
readers would trust the table until the walk reached each repository. If the
reducer was also old, nothing repaired it. A reader-side "clean walk" gate
does not close this either, because it cannot see an old writer that is still
running.

Migration 109 therefore fences old writers in the database, the same approach
as migration 096's trigger fence against old reducer pods. The design and its
cost thresholds follow a separate arbiter ruling. Every connection opened by
`runtime.OpenPostgres` runs `SET eshu.infra_inventory_writer = 'derive'`
(`inventory.WriterSessionSQL`, through pgx's after-connect hook). Triggers on
`content_entities` skip sessions that carry that setting; any other session
that writes an infra-typed row (an older ingester, projector, bootstrap-index,
or reducer, or manual SQL) marks its repository in
`infra_resource_entity_dirty_repos` in the same transaction. INSERT and UPDATE
are statement-level triggers with transition tables, so a derive-aware session
evaluates one WHEN per statement and an unaware statement takes each mark
lock once. DELETE is row-level with the session test first, so a bulk
retention prune builds no transition tuplestore. An ungated TRUNCATE trigger
marks every mirrored repository. Readers (`inventory.ReadModelReady`) trust
the table only when the marker exists and no repository is marked. Each
reconcile cycle repairs marked repositories first, oldest first and counted
against the budget: lock, delete the mark, re-derive, commit (`fenced`). A
mark proves content changed without a derive, so it needs no second check.
A replica whose mark DELETE finds nothing (another replica or the backfill
discharged it) rolls back and reports nothing, so each mark is re-derived and
counted once (`TestWriterFenceLiveTwoReplicasRepairAMarkOnce`). The
backfill's `MirrorRepo` discharges a mark the same way; path derives never
do, because an unaware write can have changed any path.

Interleaving: the mark upsert is `ON CONFLICT (repo_id) DO UPDATE SET
marked_at = dirty.marked_at WHERE false`. It takes an existing mark's row
lock until the unaware write commits, but never rewrites the row, so a large
unaware write leaves no dead tuples (`TestWriterFenceLiveStatementShapes`
asserts the mark's `xmin` does not move). A repair's DELETE of the mark waits
for that write and, under Read Committed, re-checks the row and deletes it;
the re-derive statement that follows takes a new snapshot and reads the
write's rows. A write that starts after the repair's DELETE inserts a new mark
that survives the repair. With `DO NOTHING`, which takes no lock, a repair
could clear the mark and re-derive while the write's rows were still
invisible, leaving the table short with no mark.
`TestWriterFenceLiveRepairWaitsForAnOpenUnawareWrite` holds an unaware write
open, asserts the repair blocks on the mark (`pg_stat_activity`
`wait_event_type = 'Lock'`), commits, and asserts the table has the row and
the mark is gone. Each of these mutations fails a live test: `DO NOTHING`, a
rewriting upsert, a reader gate on the marker alone, a cycle that skips
marked repositories, and an insert trigger without the session predicate.

Lock hold: only unaware sessions take mark locks, and they hold one for the
rest of their own transaction. The older content writer autocommits each
300-row batch, about 6 ms each at production index cost in the shim below,
so another unaware writer of the same repository waits at most that long per
batch. An older reducer's retention prune holds the marks of every
repository it prunes for its whole transaction (2.3-4.9 s for a 50,000-fact
batch in the retention lock proof above), once per prune batch, during the
rollout only. New sessions never take a mark lock.
Two older multi-repository transactions can deadlock on marks taken in
opposite orders; PostgreSQL aborts one and it retries on its next run.

A connection pooler that drops or does not forward session settings loses
the setting. That fails safe for this binary: its writes are marked, reads
stay on the graph, and the reconcile repairs them. A transaction-mode pooler
can also hand one binary's setting to another client's transaction, so an
older binary sharing such a pooler could write unmarked. Eshu's deployment
has no pooler; one placed in front of the DSN must forward
`eshu.infra_inventory_writer`, and a stripped setting shows as
`postgres.session_unfenced` at startup and `ok_unfenced_session` derives.

The ruling's statement-level bodies first used `SELECT DISTINCT repo_id,
clock_timestamp()`, which keeps one row per content row because the time
differs per row, so a multi-row unaware statement for one repository failed
with SQLSTATE 21000. The shipped bodies deduplicate repositories before
stamping the time, and the live shape test runs a 300-row unaware batch
upsert.

No-Regression Evidence: shim on a local PostgreSQL 18 with
`content_entities` and all of its production indexes (both trigram GINs),
the committed migration 109 fence DDL, and 50,000 `K8sResource` rows of one
repository (every row passes the label filter, the worst case) with
production-width `source_cache` and `metadata`. Passes: insert and re-upsert
as 300-row `INSERT ... ON CONFLICT (entity_id) DO UPDATE` statements (the
content writer's shape), deletes by `(repo_id, relative_path)` in 300-path
statements (5,000 rows), one bulk `DELETE ... WHERE repo_id` of the other
45,000 rows (a retention prune's shape), and one 50,000-row single-statement
insert (the review's worst case, previously 11.7 s unaware). Configurations
alternated in order each round, fresh table and a `CHECKPOINT` before each;
medians, microseconds per row against no triggers:

| Pass | no triggers | fenced (new binaries) | unfenced (older binaries) |
| --- | --- | --- | --- |
| insert, 5 rounds, production indexes | 1,057 ms | 1,062 ms, +0.09 us/row | 1,038 ms, -0.38 us/row |
| re-upsert | 1,148 ms | 1,146 ms, -0.05 us/row | 1,172 ms, +0.48 us/row |
| path deletes, 5,000 rows | 19.2 ms | 18.9 ms, -0.05 us/row | 27.3 ms, +1.62 us/row |
| bulk delete, 45,000 rows | 12.2 ms | 18.2 ms, +0.13 us/row | 88.2 ms, +1.69 us/row |
| one 50,000-row statement | 1,020 ms | 1,019 ms, -0.02 us/row | 1,045 ms, +0.51 us/row |
| insert, 7 rounds, PK only | 157 ms | 162 ms, +0.11 us/row | 166 ms, +0.19 us/row |
| re-upsert, PK only | 197 ms | 208 ms, +0.22 us/row | 212 ms, +0.30 us/row |
| path deletes, PK only | 14.6 ms | 15.1 ms, +0.11 us/row | 23.8 ms, +1.86 us/row |
| bulk delete, PK only | 10.5 ms | 16.2 ms, +0.13 us/row | 88.2 ms, +1.73 us/row |
| one 50,000-row statement, PK only | 120 ms | 138 ms, +0.36 us/row | 139 ms, +0.39 us/row |

With production indexes the fenced and unfenced insert and upsert deltas
are inside the round-to-round range (fenced insert 1,022-1,077 ms against
none 1,010-1,095 ms). The PK-only table resolves the absolute cost: at most
0.36 us per row for fenced sessions, where the only visible term is the
transition capture of a single 50,000-row statement (the writer batches 300
rows), and at most 1.86 us per row for unfenced ones, paid on deletes, where
the row-level trigger upserts once per deleted infra row. The ruling's
bounds were 0.5 and 3.0 us per row. The unaware single-statement insert that
took 11.7 s with the row-level rewriting upsert now costs 1,045 ms against
1,020 ms. Every fenced run left 0 marks; every unfenced run left exactly 1.

After a rollback to a release before migration 109 the triggers stay,
because migrations are recorded and not reverted. Every older binary is then
an unaware writer: it pays the unfenced column above (at most about 2 us per
deleted infra row, about 0.5 us per upserted one) and accumulates marks that
nothing repairs until a release with the reconcile runs again. That older
release does not read the table, so the marks are harmless there. The upgrade
guide (`docs/public/deploy/kubernetes/upgrades-rollbacks.md`) says to keep the
triggers, and how to recover if they were dropped: a recorded migration is not
re-applied, so the operator removes its record and the backfill marker before
upgrading again.

Observability Evidence: `eshu_dp_infra_inventory_reconcile_total{outcome="fenced"}`
counts repositories repaired after an unaware write; span attribute
`eshu.infra_inventory.repos_fenced` and log event
`infra_inventory.reconcile.fenced` (warn, with `repo_id`) name them. Gauges
`eshu_dp_infra_inventory_dirty_repos` and
`eshu_dp_infra_inventory_dirty_oldest_age_seconds`, recorded every reconcile
cycle whether or not the marker exists, and the `/admin/status` field
`infra_inventory` (`state` of `not_installed`, `backfilling`, `fenced`, or
`ready`, with the count, oldest age, and the reason reads are on the graph)
say why unscoped reads are on the graph. A derive whose own connection lacks
the setting counts `eshu_dp_infra_inventory_derives_total{outcome="ok_unfenced_session"}`
and logs `infra_inventory.derive.unfenced_session` at ERROR, and each writer
binary logs `postgres.session_unfenced` at startup when its session lacks it.

Follow-ups, out of scope for this change: move the backfill out of the API
and MCP processes into the projector, which owns the derive, and record the
first production `eshu_dp_infra_inventory_reconcile_duration_seconds` p95
after deploy (the shim above is smaller than a production corpus).
