# #11535: NornicDB strict-schema evidence (uid lookup index retirement)

## No-Regression Evidence:

Strict `EnsureSchemaWithBackendStrict` against live NornicDB 1.5.2-hotfix
(`nornicdb-local:main`, amd64, image `612fcee7d027`, disposable Compose
project with a fresh volume, bolt `127.0.0.1:19688`):

- Baseline (pre-D dialect): strict apply fails with exactly 120 failed
  statements — every `CREATE CONSTRAINT <t>_uid_unique` throws
  `IndexAlreadyExists` while the plain `nornicdb_<t>_uid_lookup` index from
  the same statement list still exists.
- After (post-D dialect): strict apply is zero-fail on a fresh store
  (13.81s for the full 351-statement NornicDB list) and over a
  legacy-index-preseeded store (4.12s). A second apply through the
  production adoption filter (existing objects skipped, DROP guards always
  run) is zero-fail, and seeded rows still read exactly once afterwards.
- End state asserted live: all 120 `<t>_uid_unique` constraints present,
  each with its constraint-named backing index (the measured 1.5.x name —
  never a generated one), zero `nornicdb_*_uid_lookup` indexes remain,
  and `EXPLAIN` plans equality (`MATCH (n:X {uid: ...})`) as
  `NodeIndexSeek`.
- Input shape: the full production NornicDB statement list
  (`SchemaStatementsForBackend`), 120 uid-bearing labels.
- Neo4j leg unaffected: the Neo4j statement list is byte-unchanged
  (pinned by the unchanged `graphSchemaNeo4jFingerprint` digest and the
  `DROP INDEX nornicdb_` absence assertion), and the NornicDB writer-compat
  decision is DDL-only (old writers MERGE on uid and never emit DDL), so
  the pre-#11535 tip stays the single compatible predecessor.

Known 1.5.x plan boundary (backend behavior, not a D defect): range and
prefix predicates over uid label-scan on 1.5.x — `EXPLAIN` shows
`NodeByLabelScan`+`Filter`(+`Sort`) for `WHERE n.uid > $after ORDER BY
n.uid LIMIT 500` and for `WHERE n.uid STARTS WITH $prefix`, while only
equality seeks the backing index. The owner 150k-node scale proof
(`TestCloudResourceOwnerBackfillerFirstPageMeetsDeadlineAtScale`) fails on
1.5.x at its own prefix-count gate: `STARTS WITH` counted 10.014s against
the 10s production reader deadline (seed: 150k nodes in 20m46s). The fenced
first page (uid-only projection) measured ~7.5s cold, ~0.5-1s for later
distinct pages, 5ms on result-cache repeats; the production 37-property
projection on 1.5.x remains unmeasured. The rollout keeps the 1.5.x backend
pin and this dialect in lockstep: on pre-1.5 NornicDB the constraint creates
no backing index, so the new statement list must never execute there.

## Observability Evidence:

No new spans, metrics, or log fields. Two existing operator signals change
shape, pinned by unit tests:

- The schema-phase log name `nornicdb_uid_lookup_indexes` becomes
  `nornicdb_uid_lookup_index_drops` (same `graph schema statement
  applying/applied` lines, `graph_backend=nornicdb`, `schema_phase`,
  `statement_index`, `statement_total`, `schema_statement` attributes).
- `statement_total` now equals the emitted statement count on both
  dialects (`TestSchemaStatementTotalMatchesEmittedStatements`), so
  `statement_index` can no longer exceed `statement_total` in the logs.

Live proofs: `TestLiveNornicDBStrictSchemaApplyIsClean` and
`TestLiveNornicDBStrictSchemaApplyRetiresLegacyLookupIndexes`
(`go/internal/graph/schema_nornicdb_11535_live_test.go`, gate
`ESHU_SCHEMA_NORNICDB_11535_LIVE=1`), classified `scheduled` in
`specs/live-tests.d/live-tests-02.yaml`.
