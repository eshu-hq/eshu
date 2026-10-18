# #8316: Repair `deployable_unit_correlation` Edges Lost After a Verified Success

Issue #8316. The #8283 changed-evidence gate re-opens a succeeded
`deployable_unit_correlation` item only when its resolved input changed.
Two graph-side paths delete edges after a verified success without touching
Postgres input, so the fingerprint stays equal and the item never re-runs:
path-conflict retirement of a `Repository` node and the orphan sweep. This
note records the repair: push invalidation at both deleters plus a bounded
gap reconciliation, with no periodic replay and no worker serialization.

## Decision record

- **Push, then reconcile.** Each deleter deletes the fingerprint rows of the
  repositories it removed, right after the graph commit
  (`DELETE FROM deployable_unit_reopen_fingerprints WHERE $1 = ANY(repo_ids)`,
  the issue's named form for one repository, array overlap for a batch). A
  missing row always re-opens on the next pass, so the delete is the repair
  signal. The graph delete and the Postgres delete are separate stores and
  can never share a transaction ("in the same transaction ... where
  possible" is therefore read as: in the same process, immediately after the
  commit, idempotent). A crash between the two is closed by the
  reconciliation below, which re-derives the same missing-edge set.
- **Retirement reports its retired id.** The attributed retirement binds
  `r.id AS retired_id` in WITH, before the delete — the RETURN reads a bound
  scalar, never a property off the deleted node — and returns it alongside
  the dropped-edge collection. `CollectRetirementReport` drains every RETURN
  row for ids (the dropped-edge metrics keep their first-row contract) and
  `CanonicalNodeWriter` exposes them through a `RetiredRepositoryObserver`
  port, injected in projector, ingester, and bootstrap-index wiring. The
  observer never fails the write: the graph already committed, so its error
  is logged and the reconciliation re-derives the loss.
- **The sweep reports its swept Repository keys.** The cycle already
  computes the swept keys Go-side; `DeletedRepositoryIDs` carries the
  Repository label's deleted ids (the label's identity is `id`) on the
  result, and the reducer sweep wiring invalidates them under the same sweep
  lease.
- **Gap reconciliation is repo-granularity, maintenance-pass-coupled.** It
  lists the full distinct admitted `deployable_unit_correlation` source
  anchor set (no LIMIT — a first-N prefix would starve tail anchors),
  probes the graph in 500-anchor batches for their outgoing
  `CORRELATES_DEPLOYABLE_UNIT` edges, and deletes the fingerprint rows of
  the edgeless anchors. Repo granularity is exact for this failure class:
  both deleters remove whole nodes, so loss always lands on every incident
  edge at once, and fingerprints are keyed by repository sets. Edge-level
  comparison is not available from the schema: the decision's candidate id
  hashes the deployment repository id, so only the source anchor survives in
  Postgres. The probe mirrors the sweep's proven S2 connected-keys shape
  (UNWIND-anchored concrete-relationship-variable MATCH, DISTINCT ids back).
  It runs in the bootstrap-index maintenance pass, strictly before the
  correlation reopen, on the existing one-shot pass: no new worker, no new
  cadence, no serialization change. A probe or Postgres error fails the
  pass loudly instead of silently keeping lost edges closed.
- **Never in the sweep cycle (#8371).** The reconciliation was first wired
  into the reducer sweep cycle, where its admitted-anchors `SELECT` on
  `admission_decisions` parked a second waiter behind the fault-injection
  kill cell's exclusive lock: the cell's census demands exact equality
  (`holders = 1 AND waiters > 0 AND claimed = waiters`), so two waiters
  never align and the shard failed. Any `SELECT` takes a lock conflicting
  with `ACCESS EXCLUSIVE`, so no sweep-cycle statement may touch that
  table. The maintenance pass completes before the kill cell takes its
  lock, which removes the contender entirely (arbiter verdict A; skip-on-
  contention was rejected as test-only behavior in the prod sweep).
- **No periodic replay.** The issue allows a bounded replay only as a
  measured safeguard if a loss remains after both paths. No loss class
  remains: attributed retirements push their ids, sweeps push their keys,
  the bare (NornicDB) retirement and any commit-then-crash window are
  re-derived by the per-pass reconciliation, and a failed invalidation is
  loud. No replay was added.
- **No serialization.** The invalidation is one DELETE after the commit;
  the reconciliation adds one bounded PG scan plus one bounded graph round
  trip to the already-serial maintenance pass. Worker counts, batch
  sizes, and lease behavior are unchanged.

## Index notes

The invalidation seq-scans the fingerprint table (one row per succeeded
item) and runs only when a deleter actually removed a node — rare, and
measured at 1.4 ms on 20k rows; no index was added there (index doctrine:
hot path plus usable predicate shape).

The admitted-anchors listing carries one partial index since #8371 review
P2 (`deployable_unit_gap_admitted_anchor_idx`, migration 196, mirrored in
`admissionstore.AdmissionDecisionSchemaSQL`): the P1 rescoping to the
latest decision per anchor reads `DISTINCT ON (anchor_id) ... ORDER BY
anchor_id, decided_at DESC` over `domain + anchor_kind`, which neither
`(scope_id, generation_id)`-leading index serves. The index is partial and
anchor-leading with `decided_at` following, so the per-pass listing reads
in index order. New-database DDL rides `EnsureSchema`; existing
deployments converge through migration 196 (`CREATE INDEX CONCURRENTLY IF
NOT EXISTS`, one statement per file, per the coordinator's
sole-concurrent-index rule).

## Measurements

Scratch database, local PostgreSQL 18.6 (the compose major), same shapes as
the shipped tables, `VACUUM ANALYZE`d before each run:

- 20,000 fingerprint rows (repo sets of 1–5 drawn from 10,000 repos):
  `DELETE ... WHERE 'repo-42' = ANY(repo_ids)` — 1.419 ms, 413 shared-hit
  buffers, 6 rows deleted. Batch overlap form
  (`repo_ids && ARRAY['repo-42']`) — 1.754 ms, 401 buffers, same plan
  shape. Conclusion: no GIN index; the per-deletion cost is one small seq
  scan on a rare path.
- 50,000 admission decisions (2,500 admitted deployable-unit rows):
  `SELECT DISTINCT anchor_id ... WHERE domain/state/anchor_kind ... LIMIT 500`
  — 2.628 ms, 871 shared-hit buffers (bounded form, since removed).
  Production EXPLAIN of the unbounded form: one Bitmap Index Scan on the
  existing domain/state index, 94 anchors. Conclusion at that time: the
  listing costs single-digit milliseconds in both forms; no covering index.
- #8371 P1/P2 rescoping, same scratch setup (local PostgreSQL 18.6,
  `VACUUM ANALYZE`d): 120,000 admission decisions, 15,000 matching
  `domain + anchor_kind` (1,500 anchors x ~10 generations), 105,000
  other-domain noise rows. `EXPLAIN (ANALYZE, BUFFERS)` of the new
  latest-decision-per-anchor listing: one Bitmap Index Scan on
  `deployable_unit_gap_admitted_anchor_idx` (index searches: 1, 93 shared-hit
  buffers), no Seq Scan, 18.9 ms execution. Conclusion: the new query uses
  the new partial index at a realistic retained-history distribution.

Live proof on the full 214-migration bootstrap schema, local PostgreSQL
18.6, isolated schema per test
(`TestInvalidateDeployableUnitReopenFingerprintsForReposLive`,
`TestListAllDeployableUnitAdmittedAnchorReposLive`,
`TestListAllDeployableUnitAdmittedAnchorReposConvergesAfterSupersedingDecisionLive`):

- Invalidation deletes exactly the rows whose repo_ids contain the removed
  repository (2 of 3), keeps the unrelated row, and no-ops on empty and
  unknown inputs. 5.3 s for both tests including schema bootstrap.
- The admitted-anchors listing returns Written=true plus legacy (no Written
  key) anchors, excludes Written=false, rejected, and other-domain rows,
  and returns empty for limit 0.
- The convergence proof seeds admit-then-reject (excluded: the second pass
  is a no-op), admit-reject-readmit (listed: latest state wins), and
  admit-only (listed) anchors over three generations and asserts the exact
  listed set. It fails on the pre-P1 query (the stale anchor is listed)
  and passes after.

Graph side:

- Retirement RETURN shape: the MATCH/WHERE/OPTIONAL-MATCH spine is
  byte-identical; the change adds one scalar binding (`r.id AS retired_id`)
  and one RETURN column. Steady-state plan (MATCH matches nothing) is
  unchanged by construction. Hermetic text and multi-row parse tests pin
  the shape; `TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges`
  now asserts the observer receives the retired id on the attributed
  (Neo4j) shape and nothing on the bare shape, so the CI live lanes prove
  the column against the pinned backends.
- Gap probe, measured on the pinned NornicDB backend
  (`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c`, isolated container,
  NoAuth bolt): 500 seeded Repository nodes, 490 with an outgoing
  CORRELATES_DEPLOYABLE_UNIT edge, probe over all 500 anchors returned the
  490 connected ids in 224 ms with no schema and 261 ms with the Repository
  id uniqueness constraint (client-measured incl. row collect on localhost;
  the 10 edgeless anchors were exactly the gap set in both runs).
  Conclusion: ~0.25 s per maintenance pass at the 500-anchor bound —
  negligible against the pass's own backfill and reopen work. A probe error
  fails the pass instead of misclassifying, and a wrong-row miscount
  degrades safe — a missed gap keeps today's behavior, a false gap costs
  one bounded replay.

Performance Evidence: scratch EXPLAIN (ANALYZE, BUFFERS) above (Postgres
18.6, 20k/50k rows), scratch EXPLAIN of the rescoped listing (one Bitmap
Index Scan on `deployable_unit_gap_admitted_anchor_idx`, 120k rows, 18.9
ms), live functional proof on the full bootstrap schema (including the
admit-reject convergence proof), and direct probe measurement on the
pinned NornicDB backend (~0.25 s per 500-anchor batch); no hot-path
regression: the retirement MATCH spine is unchanged, the invalidation runs
only on actual deletions, and each pass issues one indexed listing plus
one ~0.25 s bounded round trip per 500-anchor batch (one batch at the
current 94-anchor scale).

Observability Evidence: one new phase completion log line plus duration
metric, `deployable_unit_gap_reconciliation` (completion with
`deployable_unit_gap_repos` and
`deployable_unit_fingerprints_invalidated` counts), following the
established `recordPhase` convention. The sweep completion line keeps its
two counters; the retirement report keeps its existing `canonical
repository retired` line and counters, with observer failures on a WARN
under the same message family.

## Follow-up: migration 196 convergence guard and partial index

Performance Evidence: baseline per-pass listing seq-scans plus
DISTINCT-sorts all retained admission history with no supporting index;
after, scratch EXPLAIN (ANALYZE, BUFFERS) on PostgreSQL 18.6 over 120k
admission_decisions (15k matching rows across 1,500 anchors x 10
generations, 105k other-domain noise, VACUUM ANALYZEd) plans one Bitmap
Index Scan on `deployable_unit_gap_admitted_anchor_idx` (index searches
1, 93 shared-hit buffers), no Seq Scan, execution 18.9 ms. Committed
lifecycle proof
(`TestDeployableUnitGapAdmittedAnchorIndexLifecycleAndSeekLive`)
asserts apply, reapply, rollback, rebuild, exact rows with and without
the index, and index-seek planning on the production listing constant.
The change is safe because the index only narrows the access path: row
answers are byte-identical with and without it, and the listing runs
once per maintenance pass.

No-Observability-Change: this follow-up adds no metric, span, or log
line; the existing `deployable_unit_gap_reconciliation` phase log and
duration metric from the base change already cover the pass.
