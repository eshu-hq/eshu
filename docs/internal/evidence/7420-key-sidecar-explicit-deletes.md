# Key Sidecar Explicit Deletes Evidence (#7420 PR1)

Three tables carry a per-fact foreign key
`fact_id REFERENCES fact_records(fact_id) ON DELETE CASCADE`:
`relationship_reference_candidate_keys` (rrck),
`package_manifest_consumption_keys` (pmck) and
`package_registry_identity_keys` (prik). A retention prune of one generation
fires one referential-integrity trigger per deleted fact per foreign key.

This PR is the prerequisite for dropping those foreign keys (PR2). It adds:

- migrations 181 and 182: `(scope_id, generation_id, fact_id)` partition
  indexes on pmck and prik (rrck has had one since migration 058);
- explicit child deletes at both retention fact delete sites, in the table
  order rrck, pmck, prik, before the facts go;
- a guard test that fails on any `fact_records` delete that is neither
  kind-disjoint from the key families nor paired with the child deletes.

With the foreign keys still present, the explicit deletes leave the cascade
nothing to delete. The triggers still fire per fact, so site A stays at about
the same cost until PR2. PR2 drops the constraints. The `DROP CONSTRAINT`
takes ACCESS EXCLUSIVE on both `fact_records` and the child table (arbiter
correction), so PR2 must bound its lock wait.

## Root cause

Root-Cause Evidence: the trigger invocation is the cost, not the rows. On a
1.5M-fact generation (20% share of each child, 7.5M facts, PostgreSQL 18.6),
the cascade issued 1,500,000 calls per child:
rrck 7,051.2 ms, pmck 5,710.6 ms, prik 5,055.0 ms (pg_stat_statements on the
`DELETE FROM ONLY ... WHERE $1 OPERATOR(pg_catalog.=) "fact_id"` trigger
statements). Pre-deleting the children while keeping the foreign key saves
only about 4%: each per-fact trigger still runs a primary-key probe that
finds nothing. The keep-FK pre-delete theory is therefore rejected. The fix
is the explicit set-based delete plus the foreign key drop.

## Site A: whole-generation prune

Statement shipped (form F1), one per candidate and table, in order rrck,
pmck, prik, timed under the new `delete_fact_keys` phase, just before
`DELETE FROM scope_generations`:

```sql
DELETE FROM <child> WHERE scope_id = $1 AND generation_id = $2
```

Form F2 (`DELETE ... USING fact_records`, joining the generation's facts) is
rejected: it plans as a sequential scan of `fact_records`.

Coordinator fixture (1.5M-fact generation, 20% share, warm):

| Transaction | Time | Shared hits | WAL |
| --- | --- | --- | --- |
| BEFORE: FKs present, cascade only | 28.133 s | 26,369,461 | 182 MB |
| AFTER: FKs dropped, F1 child deletes, then generation delete | 2.428 s | 5,037,979 | 182 MB |

AFTER per statement: rrck 654 ms (1,500,000 rows), pmck 99.6 ms (299,853
rows), prik 41.3 ms (74,801 rows), `scope_generations` 1.63 s. Cold
`shared_buffers`: 36.5 s before, 6.8 s after.

Executor fixture (150,000-fact target generation at 5% share, 3M facts, md5
fact ids, warm second run; `7420-key-sidecar-sites.sql`):

| State | rrck | pmck | prik | `scope_generations` delete | Total |
| --- | --- | --- | --- | --- | --- |
| A0: FKs present, cascade only (today) | | | | 2,807.8 ms | 2.81 s |
| A1: FKs present plus F1 deletes (this PR) | 59.0 ms | 11.4 ms | 3.9 ms | 2,656.2 ms | 2.73 s |
| A2: FKs dropped plus F1 deletes (PR2) | 60.5 ms | 12.3 ms | 3.3 ms | 272.1 ms | 0.35 s |

This PR is neutral at site A (2.81 s to 2.73 s); PR2 removes 87%.

## Site B: over-limit chunk page (#7981)

The chunk page is the generation's first `min(BatchRowLimit, 100000)` facts
by `(observed_at, fact_id)`. Fact ids are hashes, so a page is not a
contiguous `fact_id` range and the range form is not available. Candidates
for the page's child delete, measured on a 100,000-id page:

- B1 `fact_id = ANY($1::text[])`: plan hazard. Coordinator fixture: prik
  chose a Seq Scan (4,728 pages at 20% share, 19,540 at 5% per page, 15 full
  scans per generation); pmck with the new index flipped between the primary
  key and a skip scan on the new index (100 searches, 854 ms vs 82 ms).
- B2 `scope_id = $1 AND generation_id = $2 AND fact_id = ANY($3::text[])`:
  shipped. Coordinator fixture with contiguous ids: mixed (prik 6,519 vs
  24,490 buffers but 322.8 vs 110.4 ms; pmck 26,417 buffers, 96 searches,
  355 ms). On md5 ids it is the only stable form (table below).

Executor fixture, 100,000-id page of the 150,000-fact generation
(`7420-key-sidecar-arms.sql`; buffers are the Delete node's shared blocks):

| Share | Form | Table | Access path | Buffers | Time |
| --- | --- | --- | --- | --- | --- |
| 20% | B1 custom | rrck | Seq Scan | 111,539 | 112.3 ms |
| 20% | B1 custom | pmck | Seq Scan | 21,900 | 27.9 ms |
| 20% | B1 custom | prik | Seq Scan | 5,518 | 10.3 ms |
| 5% | B1 custom | rrck | Bitmap on pkey, 3,829 searches | 141,075 | 318.6 ms |
| 5% | B1 custom | pmck | Seq Scan | 27,718 | 84.7 ms |
| 5% | B1 custom | prik | Seq Scan | 7,368 | 29.7 ms |
| 5% | B1 generic | rrck | Index Scan on pkey, 3,829 searches | 239,472 | 328.5 ms |
| 20% | B2 custom | rrck | Index Scan on partition idx, 2 searches | 201,794 | 183.5 ms |
| 20% | B2 custom | pmck | Bitmap on partition idx, 1 search | 20,577 | 126.9 ms |
| 20% | B2 custom | prik | Bitmap on partition idx, 1 search | 5,062 | 104.0 ms |
| 5% | B2 custom | rrck | Index Scan on partition idx, 2 searches | 201,796 | 208.1 ms |
| 5% | B2 custom | pmck | Index Scan on partition idx, 1 search | 40,297 | 140.2 ms |
| 5% | B2 custom | prik | Index Scan on partition idx, 1 search | 9,838 | 113.3 ms |
| 5% | B2 generic | all three | Index Scan on partition idx | 201,796 / 40,297 / 9,838 | 203.1 / 130.8 / 107.0 ms |
| stale | B2 custom and generic | all three | Index Scan on partition idx | 201,778 / 40,416 / 10,171 | 187.9 / 135.7 / 97.8 ms |

B2 stays on the partition index in every case: custom and forced-generic
plans, 5% and 20% shares, and a generation loaded after ANALYZE with
autovacuum off (stale). Its buffers match between 5% and 20%, so they track
the generation, not the table. About 90 ms of each B2 statement is the
start-up cost of sorting the 100,000-element array. The site A F1 statement
is equally stable (Bitmap or Index Scan on the partition index, 1 search, in
every case): 154,165 / 30,470 / 7,706 buffers at 5% generic.

Per page, whole chunk path (executor fixture, 150,000 facts = pages of
100,000 and 50,000, warm):

| State | Page 1 | Page 2 | Total |
| --- | --- | --- | --- |
| B0: FKs present, fact delete only (today) | 2,054 ms | 998 ms | 3.11 s |
| B1: FKs present plus B2 deletes (this PR) | 2,502 ms | 1,192 ms | 3.75 s |
| B2: FKs dropped plus B2 deletes (PR2) | 894 ms | 395 ms | 1.35 s |

This PR adds about 20% to the over-limit chunk path until PR2, because the
per-fact triggers still fire. PR2 removes 57% relative to today. The chunk
path runs only for a generation whose own facts exceed `BatchRowLimit`.

## Index cost and doctrine

No existing index serves `(scope_id, generation_id)` on pmck or prik: the
primary keys lead with `fact_id`, and the lookup indexes
(`ecosystem, package_name, scope_id, generation_id, ...`) put `scope_id`
third. Without the new index the pmck F1 delete plans as a Seq Scan
(67,368 pages at 5% share). Build: 3.2 s and 85 MB at 1.5M pmck rows,
0.8 s and 21 MB at 374k prik rows. Insert tax on a 500-row `ON CONFLICT`
batch: +2.3 to 3.2 ms (+16 to 22%) for pmck, same shape for prik. Both are
sole `CREATE INDEX CONCURRENTLY IF NOT EXISTS` statements;
`scripts/lib/migration_lock_shape.py check` exits 0 on both.

## Accuracy and concurrency

Each live test runs on the migrated schema with the three foreign keys
dropped in its isolated schema only (PR2's end state):

- `TestGenerationRetentionNormalPruneLeavesNoOrphanFactKeysLive` and
  `TestGenerationRetentionChunkDrainLeavesNoOrphanFactKeysLive`: zero orphan
  key rows (anti-join to `fact_records` and `scope_generations`) after every
  cycle. On main they fail with 103 / 72 / 11 and 256 / 204 / 57 orphans.
- Readers ignore orphans: `TestReadinessIgnoresOrphanPackageKeysLive` (pmck
  and prik readers join keys to the live fact; a real fact behind the same
  registry key makes ownership ambiguous, so the orphan would have mattered)
  and `TestOrphanRelationshipKeysAreInvisibleToDeferredLoadLive`.
- No stale-row shadowing: `TestKeyWritersReplaceOrphanFactKeysLive`. The
  writers delete by `fact_id` before they insert.
- Writes cannot recreate keys for a pruned generation. Ingestion writes keys
  in the fact transaction, after the fact rows (`ingestion.go`, the
  `upsertStreamingFacts` batch callback), and `fact_records` keeps its
  foreign key to `scope_generations`
  (`TestOrphanFactKeysCannotBeWrittenForAPrunedGenerationLive`).
- Concurrency (conflict domain: key rows of one scope): a key writer on the
  active generation commits under a 2 s `lock_timeout` while retention holds
  its deletes for the superseded generation. A rolled-back attempt changes
  nothing, and a retry deletes 0 rows
  (`TestRetentionFactKeyDeletesBesideKeyWriterLive`). The package key scope
  rebuild waits and then completes without a deadlock, with and without the
  retention scope lock (`TestRetentionFactKeyDeletesBesideBackfillRebuildLive`).
  The seeded RED reverses the order (prik before pmck). The rebuild then
  fails with `deadlock detected (SQLSTATE 40P01)`
  (`TestRetentionFactKeyReversedOrderDeadlocksLive`). In production,
  retention, ingestion and the rebuild all lock the `ingestion_scopes` row
  first, so the child order is a second line of defense.

Fact delete sites (`TestFactRecordDeleteSitesAreKeySidecarSafe`; the seeded
violations in `TestFactRecordDeleteGuardCatchesPlantedViolations`): retention
`deleteScopeGenerationsForRetentionQuery` (site A, paired) and
`generationRetentionChunkDeleteFactsQuery` (site B, paired); kind-literal
`reducer_container_image_identity` in three containerimage statements;
kind-parameter retires bound to `reducer_eshu_search_document`,
`reducer_aws_cloud_runtime_drift_finding`,
`reducer_terraform_config_state_drift_finding` and
`reducer_code_drifted_finding`. Ingestion deletes no facts; its writers delete
keys by `fact_id` themselves.

## Signals

Performance Evidence: site A and site B tables above, with
`docs/internal/evidence/7420-key-sidecar-fixture.sql`,
`7420-key-sidecar-arms.sql` and `7420-key-sidecar-sites.sql`. Plan stability
is pinned by `TestGenerationRetentionFactKeyPlansStayOnPartitionIndexLive`
(node and index checks under cold, analyzed, custom and generic plans,
buffers at most 4 per deleted row plus 64, seeded REDs for the fact_id-only
form and a dropped partition index).

Observability Evidence: `eshu_dp_generation_retention_phase_duration_seconds`
gains `phase=delete_fact_keys` (site A). The chunk page's key deletes stay in
`prune_chunked_facts`. `eshu_dp_generation_retention_rows_pruned_total`
reports the three tables from the deletes' affected rows. The cycle log
`phase_seconds` and `rows_pruned_total` fields carry both. No new metric or
label key, and no new cardinality.
