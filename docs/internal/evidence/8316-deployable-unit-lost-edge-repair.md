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
- **Gap reconciliation is repo-granularity, sweep-coupled.** It lists the
  distinct admitted `deployable_unit_correlation` source anchors (bounded
  500), probes the graph once for their outgoing
  `CORRELATES_DEPLOYABLE_UNIT` edges, and deletes the fingerprint rows of
  the edgeless anchors. Repo granularity is exact for this failure class:
  both deleters remove whole nodes, so loss always lands on every incident
  edge at once, and fingerprints are keyed by repository sets. Edge-level
  comparison is not available from the schema: the decision's candidate id
  hashes the deployment repository id, so only the source anchor survives in
  Postgres. The probe mirrors the sweep's proven S2 connected-keys shape
  (UNWIND-anchored concrete-relationship-variable MATCH, DISTINCT ids back).
  It runs inside the hourly sweep cycle under the same lease: no new worker,
  no new cadence, no serialization change. A probe or Postgres error fails
  the cycle loudly (retried next poll) instead of silently keeping lost
  edges closed.
- **No periodic replay.** The issue allows a bounded replay only as a
  measured safeguard if a loss remains after both paths. No loss class
  remains: attributed retirements push their ids, sweeps push their keys,
  the bare (NornicDB) retirement and any commit-then-crash window are
  re-derived by the hourly reconciliation, and a failed invalidation is
  loud. No replay was added.
- **No serialization.** The invalidation is one DELETE after the commit;
  the reconciliation adds one bounded PG scan plus one bounded graph round
  trip to the already-serial hourly sweep cycle. Worker counts, batch
  sizes, and lease behavior are unchanged.

## Index notes

No index was added (index doctrine: hot path plus usable predicate shape).
The invalidation seq-scans the fingerprint table (one row per succeeded
item) and runs only when a deleter actually removed a node — rare, and
measured at 1.4 ms on 20k rows. The admitted-anchors scan runs hourly at
2.6 ms on 50k decisions. Both stay seq scans by measurement, not by guess;
if production row counts move the cost, the follow-up adds the index with
its own proof.

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
  — 2.628 ms, 871 shared-hit buffers. Conclusion: the hourly listing costs
  single-digit milliseconds; no covering index.

Live proof on the full 211-migration bootstrap schema, local PostgreSQL
18.6, isolated schema per test
(`TestInvalidateDeployableUnitReopenFingerprintsForReposLive`,
`TestListDeployableUnitAdmittedAnchorReposLive`):

- Invalidation deletes exactly the rows whose repo_ids contain the removed
  repository (2 of 3), keeps the unrelated row, and no-ops on empty and
  unknown inputs. 5.3 s for both tests including schema bootstrap.
- The admitted-anchors listing returns Written=true plus legacy (no Written
  key) anchors, excludes Written=false, rejected, and other-domain rows,
  and returns empty for limit 0.

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
  Conclusion: ~0.25 s per hourly cycle at the 500-anchor bound — negligible
  against the sweep's own S1/S2 reads. A probe error fails the cycle
  instead of misclassifying, and a wrong-row miscount degrades safe — a
  missed gap keeps today's behavior, a false gap costs one bounded replay.

Performance Evidence: scratch EXPLAIN (ANALYZE, BUFFERS) above (Postgres
18.6, 20k/50k rows), live functional proof on the full bootstrap schema,
and direct probe measurement on the pinned NornicDB backend (~0.25 s for
500 anchors); no hot-path regression: the retirement MATCH spine is
unchanged, the invalidation runs only on actual deletions, and the
reconciliation adds one ~3 ms PG scan plus one ~0.25 s bounded round trip
to the hourly sweep cycle.

No-Observability-Change: no new metric, span, or log line. The sweep
completion line (`graph orphan sweep cycle completed`) gains two counters
(`deployable_unit_fingerprints_invalidated`,
`deployable_unit_gap_repos`); the retirement report keeps its existing
`canonical repository retired` line and counters, with observer failures on
a WARN under the same message family.
