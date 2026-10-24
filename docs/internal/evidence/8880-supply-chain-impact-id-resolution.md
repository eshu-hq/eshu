# #8880 Supply-Chain Impact Active Evidence: Resolve IDs Once

`FactStore.ListActiveSupplyChainImpactFacts` loads the evidence one
supply-chain impact intent needs. Before #8880 it ran one statement that
OR-ed about thirty identity arms over 21 fact kinds and paged it 500 rows at
a time. No index can serve that disjunction, so every page walked every
active generation's facts and detoasted `file` payloads once per arm. One
hosted npm page read 13.9 million buffers (ledger:8880-hosted-npm-baseline-generic-buffers).

The loader now resolves matching fact IDs once per call from per-branch,
index-anchored statements, orders and caps them in Go, and fetches payloads
in 500-ID primary-key pages, all in one read-only repeatable-read snapshot on
the primary. The arbiter ruling (shape c, Addenda A and B) fixed the shape;
this note records the census, the index decisions, and the proof.

## Shape

The catalog lives in `go/internal/storage/postgres/facts/impact`. One Go table
generates every statement, its anchor, the foreign-key guard, and the text of
migrations 212 to 224.

Each branch statement has three MATERIALIZED steps:

1. `candidate` reads `fact_records` only through the anchor predicate: the
   kind list, `is_tombstone = FALSE`, the key or scope arm, and any other arm
   filter.
2. `active` applies the retired active-generation join, verbatim, once per
   distinct `(scope_id, generation_id)` pair among the candidates.
3. The outer query keeps the candidates whose pair survived.

The scope-row arms of the retired predicate (`scope.source_key`,
`scope.payload->>'repo_id'`, `scope.payload->>'id'`) render as
`fact.scope_id = ANY(resolved scopes)`. For a row the active join admits,
`scope` is the row's own scope, and `ScopeResolutionSQL` reads the same
snapshot, so the rewrite selects the same rows. Every other arm is the
retired text.

Exactly one statement returns `vulnerability.suppression` rows. It orders by
`convert_to(fact_id, 'UTF8')` and stops at the cap plus one. Go keeps the
first cap rows and reports truncation past it, as the sentinel did. Core
evidence carries no LIMIT anywhere.

### Why the CTEs

The first draft inlined the active join into each branch. The planner
estimated the two-column join of `ingestion_scopes` and `scope_generations`
at one row and chose to loop `fact_records_scope_generation_idx` over every
active scope, then filter by the key. The file `repo_id` branch read 55,127
buffers for 80 rows that way (ledger:8880-fixture-inlined-join-scope-walk-buffers).
The shim saw the same planner caution on the hosted corpus: the non-file
20-kind OR took 630 ms under `force_generic_plan` and 18 s with literal
arrays (ledger:8880-hosted-nonfile-or-custom-ms). The generic shape was
anchored on `fact_records_scope_generation_idx (scope_id, generation_id,
fact_kind)` with the full OR as a filter. The custom plan shape was not
captured (NOT_CHECKED). The MATERIALIZED candidate CTE removes the choice:
`fact_records` is reachable only through the anchor.

Per-candidate active joins were the next cost. A KEV filter of 1,500 CVEs
probed `scope_generations` once per candidate (13,500 buffers for 4,500
rows). Joining once per distinct pair brought the KEV call to 1,034 buffers
(ledger:8880-fixture-kev-production-buffers).

### The foreign-key guard

A growth-class branch probes only the keys its kind's contract carries. Every
other (kind, key) cell goes to the guard: index
`fact_records_supply_chain_impact_foreign_key_guard_idx` admits a growth or
file row that carries a key an applicable arm compares but no probe serves.
The guard statement repeats the predicate and evaluates the full retired
predicate, with the #5237 file gate, over those rows. The contracts carry
none of those keys
(`TestSupplyChainImpactGrowthContractsCarryNoGuardedKey`,
`TestSupplyChainImpactFileFactCarriesNoUngatedIdentityKey`), so the index is
expected empty. A nonzero row count means a contract drifted. The row set
stays exact either way.

The predicate is one `payload ?| (CASE fact_kind WHEN ... END)`. Written as
an OR of per-kind arms, the planner split it into a BitmapOr and costed two
arms onto other kind indexes: 1,124 buffers for 3 guard rows
(ledger:8880-fixture-guard-bitmapor-buffers). The CASE form reads 14.

## Census and classes

Active non-tombstone rows on the hosted corpus, 2026-10-23 (coordinator
shim, read-only):

| Kind | Rows | Class |
| --- | ---: | --- |
| file | 131,710 | file |
| vulnerability.known_exploited | 1,755 | growth |
| reducer_platform_materialization | 1,462 | bounded |
| reducer_workload_identity | 1,462 | bounded |
| vulnerability.affected_package | 239 | growth |
| vulnerability.cve | 223 | growth |
| security_alert.repository_alert | 80 | growth |
| package_registry.package_version | 50 | growth |
| reducer_package_consumption_correlation | 9 | growth |
| reducer_service_catalog_correlation | 2 | bounded |
| oci_registry.image_index | 1 | growth |
| oci_registry.image_tag_observation | 1 | growth |
| every other scanned kind, including vulnerability.suppression | 0 | growth, or bounded for suppression |

`ingestion_scopes`: 917, all with an active generation.

Classes follow Addendum A. Growth kinds scale with external catalogs,
registries, SBOMs, runs, or repositories x advisories, so the census is the
corpus, not the class. Bounded kinds scale with topology or operator
authorship. `reducer_sbom_attestation_attachment` is not named in the
addendum. It grows with SBOM count, so it is growth class, and existing
indexes serve both of its keys. `factimpact.Kinds()` records each class with
its census figure.

## Index decisions

| Index | Migration | Decision | Reason |
| --- | --- | --- | --- |
| affected_product criteria | 212 | added | growth; existing lookup leads on cve_id |
| package_id over alert, package_version, hint, sbom.component | 213 | added | growth; no package_id-leading index for these kinds |
| package_version purl | 214 | added | growth; no purl-leading index for the kind |
| security_alert cve_id | 215 | added | growth; the v2 cve index covers vulnerability kinds only |
| vulnerability_hint advisory_id | 216 | added | growth; the v2 advisory index covers vulnerability kinds only |
| OCI manifest and index digest | 217 | added | growth; the refs indexes require source_system, which the read does not state |
| OCI tag resolved_digest | 218 | added | growth; no leading index |
| OCI referrer referrer_digest | 219 | added | growth; the referrer index leads on subject_digest |
| ci_cd and consumption payload scope_id | 220 | added | growth; payload scope_id is a repository arm with no leading index |
| ci_cd and consumption scope-leading anchor | none | dropped (Addendum B) | `fact_records_scope_generation_idx` bounds scope_id, generation_id, and fact_kind as Index Cond: 15/21 buffers without it, 14/14 with it |
| suppression active scan | 221 | added | bounded; no index named the kind |
| platform materialization active scan | 222 | added | bounded; no index named the kind |
| JS/TS file repo_id | 223 | added | file; serves both file branches without a detoast per non-JS/TS file |
| foreign-key guard | 224 | added | makes omitted cells exact by construction; expected empty |
| six lower(btrim) suppression scope indexes | none | skipped | census: 0 suppressions; the bounded-class full filter reads every suppression row once per call |
| consumption package_id and repository_id | none | skipped | `fact_records_package_correlations_v2_lookup_idx` and `..._v2_repository_lookup_idx` are partial on a kind set that includes it |

Upgrade path for the skipped suppression indexes: if a deployment's
suppression count makes
`eshu_dp_supply_chain_impact_active_evidence_branch_seconds{branch="vulnerability_suppression"}`
dominate the load, a new census can move suppression to growth class with
key probes on the six normalized scope keys.

Every new index is one `CREATE INDEX CONCURRENTLY IF NOT EXISTS` per file.
`TestNewIndexStatementsMatchMigrations` pins each file to `NewIndexes()`.
On the plan fixture each rebuild plus `ANALYZE fact_records` took about 2 s,
and the indexes measured 16 KB (guard) to 2.6 MB (package_id). 2,000 TOASTed
file inserts took 452 to 507 ms with or without all 13 indexes across two
runs, within run-to-run noise. `TestSupplyChainImpactNewIndexInsertCostLive`
logs those figures. Deployment: a concurrent build waits for in-flight old
loader transactions, so run the migration when no long impact pass is active
or accept the wait. The builds take no write lock.

## Proof

Performance Evidence: on a local PostgreSQL 18.6 disposable database, the
npm call fell from 784,160 to 950 shared buffers under both
`plan_cache_mode` values (ledger:8880-fixture-npm-baseline-buffers,
ledger:8880-fixture-npm-production-buffers). KEV fell from 28,470 to 1,034
(ledger:8880-fixture-kev-baseline-buffers,
ledger:8880-fixture-kev-production-buffers). A KEV pass matching nothing
fell from 2,847 to 70 (ledger:8880-fixture-kev-zero-row-buffers).
`TestSupplyChainImpactActiveEvidenceBufferBudgetLive` records every
statement the production loader sends and every page of the frozen retired
statement, and replays each under PREPARE plus EXPLAIN (ANALYZE, BUFFERS)
EXECUTE in both modes. It requires 100x for npm and 10x for KEV. Before the
change it failed at 1.0x.

Hosted: the shim measured the same npm filter at 13,887,269 buffers for the
retired statement and 105,161 for candidate statements on existing indexes
only, with the same 267 fact IDs (ledger:8880-hosted-npm-baseline-generic-buffers,
ledger:8880-hosted-npm-candidate-buffers). The shipped loader with migrations
212 to 224 has not been measured on the hosted corpus (NOT_CHECKED), and
neither has the KEV 100x bar or the 50 ms warm KEV call
(ledger:8880-hosted-kev-baseline-generic-buffers is the baseline page). On
the fixture, the per-candidate cost that bar depends on is 0.23 buffers per
KEV row.

Exactness: `TestSupplyChainImpactActiveEvidenceMatchesRetiredStatementLive`
compares the loader with the frozen pre-#8880 statement
(`testdata/list_active_supply_chain_impact_facts_eb62fa3.sql`, rendered from
blob `ddcca90366bd96d1d941f188ccff497c3e3a9ce7`, sha256 pinned) walked to
exhaustion with the retired cursor and sentinel cap. It compares rows,
order, payloads, and the truncation bool, across nine filter shapes: npm
first round, npm with every identity, KEV, Maven, repository only, digest
only, cap crossed mid-stream, cap reached exactly, and empty. The matrix
covers every arm and kind, tombstones, superseded and pending generations,
normalized suppression anchors with Unicode White_Space, deployment-only
suppressions, a fact matching two keys, foreign keys on growth kinds and on
files under both FileRepositoryIDs states, 650 core rows across the page
boundary, and a 620-row identity stream through a stub function. All nine
pass. `TestSupplyChainImpactFileKindGateLive` and
`TestSupplyChainImpactAdvisoryFilterPlanLive` were re-derived on the loader
and pass.

Plans: `TestSupplyChainImpactBranchPlansStayOnAnchorsLive` runs every branch
with real binds on 150 repository scopes of TOASTed files, the arm matrix,
and 4,000 contract-faithful rows of every other kind spread over active and
superseded generations. Each branch reads `fact_records` only through its
anchor in both plan modes. The resolved-scope probes carry both `scope_id`
and `generation_id` bounds in their Index Cond. The bounded kinds read their
rows through any index partial on the kind alone. Workload and service
catalog take a whole-index GIN bitmap scan: no Index Cond, every row of the
kind, 0 rechecked rows. Dropping each new index moves the plan off it, and
rebuilding it from the migration statement brings it back:

| Index | Branch | RED buffers (generic / custom) | GREEN buffers |
| --- | --- | --- | --- |
| affected_product criteria | affected_product_criteria | 116 / 116 | 9 |
| package_id | package_id | 956 / 956 | 12 |
| package_version purl | package_version_purl | 349 / 4,305 | 9 |
| security_alert cve_id | security_alert_cve_id | 209 / 209 | 9 |
| vulnerability_hint advisory_id | vulnerability_hint_advisory_id | 368 / 4,305 | 9 |
| OCI digest | oci_image_digest | 923 / 4,305 | 10 |
| OCI tag resolved_digest | oci_tag_resolved_digest | 349 / 349 | 9 |
| OCI referrer_digest | oci_referrer_referrer_digest | 302 / 302 | 9 |
| payload scope_id | repository_payload_scope_id | 643 / 643 | 10 |
| suppression active scan | vulnerability_suppression | 335 / 335 | 238 |
| platform active scan | platform_materialization | 592 / 592 | 500 |
| JS/TS file repo_id | file_repo_id | 2,416 / 2,416 | 90 |
| JS/TS file repo_id | file_scope | 1,224 / 1,224 | 95 |
| foreign-key guard | foreign_key_guard | 47,251 / 47,251 | 14 |

The two active-scan indexes win by less: a bounded statement reads every row
of its kind for the payload filter, and PostgreSQL 18's btree skip scan of
`fact_records_scope_generation_idx` finds those rows almost as cheaply when
they sit in few (scope, generation) prefixes. With one prefix the fixture
tied (493 vs 501), which is recorded here, not a failure. PostgreSQL before
18 has no skip scan.

## Concurrency

Isolation is unchanged: READ ONLY REPEATABLE READ, ACCESS SHARE only, no row
locks, one snapshot per call. Scope resolution, every branch, every payload
page, and the identity stream read that one snapshot. A call issues more
statements (20 for the npm fixture call), each milliseconds, and holds its
snapshot for far less time than the retired 30 s to 75 min transactions, so
it holds back the xmin horizon less. No worker count, page size, or round
count changed.

## Observability

Observability Evidence: each statement records
`eshu_dp_supply_chain_impact_active_evidence_branch_seconds{branch}` and
`eshu_dp_supply_chain_impact_active_evidence_branch_rows_total{branch}`. The
`branch` label is the closed catalog name set plus `scope_resolution`,
`payload_page`, and `identity`. Guard rows count on
`eshu_dp_supply_chain_impact_active_evidence_foreign_key_guard_rows_total`,
and a WARN line names the kinds. The reducer's `reducer execution
succeeded` line adds `sub_signal_active_evidence_branch_<name>_rows` and
`sub_duration_active_evidence_branch_<name>_seconds` per statement, plus the
`active_evidence_loads`, `active_evidence_resolved_fact_ids`,
`active_evidence_payload_pages`, `active_evidence_guard_rows`, and
`active_evidence_truncated_loads` sub-signals, summed over the pass.
`TestSupplyChainImpactHandlerLogsActiveEvidenceStatementStats` pins the
merge. An operator who sees `load_active_evidence` climb can read which
branch grew, and how many IDs and pages the pass resolved, from the same
line.

## Commands

```bash
cd go
export ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DSN='postgres://<user>@127.0.0.1:5432/postgres?sslmode=disable'
export ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DISPOSABLE=1
go test ./internal/storage/postgres -run 'TestSupplyChainImpactActiveEvidence|TestSupplyChainImpactBranchPlansStayOnAnchorsLive|TestSupplyChainImpactNewIndexInsertCostLive' -count=1 -v
go test ./internal/storage/postgres/facts/impact ./internal/storage/postgres ./internal/reducer/supplychain/core -count=1
```

## NOT_CHECKED

- The shipped loader on the hosted corpus, including the KEV 100x bar and
  the 50 ms warm KEV call.
- The custom-plan shape behind the 18 s hosted figure.
- Hosted index sizes and build times for migrations 212 to 224.
- Whether `package_manifest_consumption_keys` could anchor consumption
  (not needed: existing correlation indexes serve it).
