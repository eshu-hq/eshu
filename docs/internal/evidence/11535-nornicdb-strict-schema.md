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
distinct pages, 5ms on result-cache repeats. The production full projection
(all 35 properties of `cloudResourceOwnerBackfillProjection`) measured live
on 1.5.2-hotfix 2026-11-26 (disposable container
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:v1.5.2-hotfix@sha256:ece888de07a150a353f8fbf4755bb92f1126b3878828b81b964fcefaec956ac5`,
fresh store, full production NornicDB schema, 150k CloudResource nodes seeded
in 28m15s with every projected property populated): COLD first page (500 rows
over 150k nodes through the production `NewNeo4jReader` 10s deadline) took
**8.646s — inside the 10s graph-read deadline** (the server logged a
slow-read warning at 8.65s, so headroom is 1.35s); WARM repeat with distinct
query text (server result cache defeated, store warm) took **61ms**. Verdict:
PASS, no query tuning applied. Throwaway measurer
`go/internal/query/temp_11535_measure_live_test.go` (kept out of the commit;
method: seed all 35 properties in 250-row batches, then time the exact
production first-page query cold, then warm). The rollout keeps the 1.5.x
backend pin and this dialect in lockstep, now enforced by the
`requireNornicDBBackendVersion` schema-apply gate: on pre-1.5 NornicDB the
constraint creates no backing index, so the new statement list must never
execute there.

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

## Read-model skew audit: list-on-winners vs explain-on-legacy (#11535)

Divergence class: **impact list/explain read-model skew**. The findings
list followed the `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` flag onto the
maintained winners table while the explanation store stayed hardcoded to
the legacy fact read (`reader, false` in `go/cmd/api/wiring_handlers.go`
and `go/internal/query/d6_read_admission_wire.go`). The C2 write stop
(#10880, migration 358) left the legacy fact read structurally empty, so
every explain answered `ErrExplanationNotFound` while the list served the
same key: digest-keyed explain
(`cve_id=CVE-2026-00010&subject_digest=sha256:abcdef...`), the MCP
explain, and the packet `graph_answers` (0, want >= 14) failed on the
NornicDB 1.5.x leg (published log: 758 pass + 3 required-fail at commit
05c88a3209; lane rerun on the same commit: 760 pass + the same 3
required-fail) with identical reducer probes and equivalent write counts
on both legs. Live pin: the
decide-first statement run verbatim against the gate Postgres returns the
key (8 same-key supports, all authorized), while the serving API on the
same database answered no-finding. Fix: carry the winners flag to the
explanation store at both sites, matching `serve_adapters_data.go`; no
backend gating (arbiter ruling on #11535).
