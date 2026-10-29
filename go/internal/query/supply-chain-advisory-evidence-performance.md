# Advisory Evidence Read Performance

This note records the query-shape contract for
`GET /api/v0/supply-chain/advisories/evidence`.

Performance Evidence: issue #868 replaced the broad active vulnerability fact
CTE with a selector-first seed and matched-fact query. The read model passes
advisory and CVE lookup ids as a bounded `text[]`, keeps package scope as a
separate equality anchor, includes literal vulnerability fact-kind predicates so
Postgres can use the matching partial indexes, and avoids alias/correlation
anchor JSONB scans as top-level lookup paths. The active identity indexes lead
with `cve_id`, `advisory_id`, or `ghsa_id`, then carry `scope_id` and
`generation_id` so the read can prove active-generation truth without scanning
every active vulnerability fact. Aliases remain returned source evidence after a
first-class `cve_id`, `advisory_id`, `ghsa_id`, package, or PURL anchor
identifies the advisory. Focused proof:
`go test ./internal/query -run 'TestAdvisoryEvidence(Query|Lookup)|TestSupplyChainListAdvisoryEvidence|TestPostgresAdvisoryEvidenceStore|TestBuildAdvisoryEvidenceRows' -count=1`
and
`go test ./internal/storage/postgres -run 'TestBootstrapDefinitionsInclude(AdvisoryEvidenceReadIndexes|SupplyChainImpactFactIndexes)$' -count=1`.

Performance Evidence (issue #8870 fix (c), 2026-10-29): the eight seed/matched
legs are lookup-driven `LATERAL` probes into the payload-key index leader with
active-generation membership as a correlated `EXISTS` subplan (`OFFSET 0`
keeps the planner from flattening it back into a per-scope semi-join).
SUPERSEDED by the #9474 P1 entry below: the package legs as first written
were pinned to `fact_kind = 'vulnerability.affected_package'`, which silently
dropped package-matching rows of other kinds (GitLab CVE envelopes carry
`package_id`). Timings quoted here belong to that superseded shape and are
kept as history only. Shape pins live in
`supply_chain_advisory_evidence_sql_test.go`
(`TestAdvisoryEvidenceQueryProbesLookupValueFirstWithoutScopeFanout`).
Remote preserved-volume proof on the representative Compose backlog applied the
new DDL in 241s, then returned `CVE-2021-44228` in 0.691s cold and 0.435s /
0.439s warm with 2 source observations and 5 affected package rows. A missing
`CVE-2099-0000` returned 0 rows in 0.363s / 0.234s / 0.307s. `EXPLAIN
ANALYZE` used the three `fact_records_vulnerability_active_*_lookup_v2_idx`
indexes and completed the present-CVE SQL in 472.419ms.

Performance Evidence (issue #9474 P1 verdict a, 2026-10-29): GitLab CVE
envelopes carry `package_id` (`newGitLabCVEEnvelope`), so pinning the
package legs to `vulnerability.affected_package` silently dropped
package-matching rows of other kinds. Each of the four package legs is now
two slices: the affected_package slice is unchanged, and a new slice pinned
to the five remaining advisory kinds keeps the shared six-kind `IN` and
`ANY($1)` envelope. Fourteen legs, `OFFSET 0` x14. Two partial indexes in
the same v2 leader shape (`(payload->>key), scope_id, generation_id,
fact_kind, fact_id`) carry the five-kind predicate: migrations
240 (`package_id`) and 241 (`purl`), plus the matching bootstrap fragment.
No key-presence narrowing; a hermetic shim proves the planner selects both
indexes per package leg with no `fact_records` seq scan in the
seed/matched CTEs. The legs name no indexes: a new binary against a schema
missing 240/241 is slow, not wrong, and the schema job completes the
migrations before API readiness. Focused proof:
`go test ./internal/query -run 'TestAdvisoryEvidencePackageLegs'`
(RED-first hermetic fixtures: lone cve-with-package_id, tombstoned sibling,
synthetic reference-with-purl) and the old-vs-new differential on one
`REPEATABLE READ` snapshot: byte-identical including order on
KEV-CVE/package-only/impact-scoped/GLAD-pair/5000-row inputs, with the only
delta on P1-shaped inputs being exactly the previously dropped rows
(3 rows added, 0 removed). Local shim on 20k rows: 5-20ms per input,
index builds 1-20ms, each new index 8KB vs 2696KB table. Primary corpus
(91 GB fact_records, active ingestion, read-only tx, 2026-10-29): new-shape
`EXPLAIN (ANALYZE, BUFFERS)` KEV-CVE 0.59-0.75s, package-only 0.65s,
GLAD-shaped 0.63-0.73s, impact-scoped 0.92-1.14s; every leg an index probe
(v2 identity + existing affected_package partials + both new five-kind
slices), zero `fact_records` seq scans. Old-vs-new differential on one
`REPEATABLE READ` snapshot, byte-identical including order: KEV-CVE
(1 row, old 315.7s/new 0.7s), package-only pkg:npm/axios (2084 rows,
35.6s/0.7s), impact-scoped r_33474efb (1472 rows, 41.5s/1.0s), GLAD-shaped
pkg:composer/symfony/symfony (194 rows, 33.9s/1.1s), busiest package
npm://registry.npmjs.org/axios (2084 rows, 37.8s/1.1s). Gate restatement
(arbiter, #9474): non-impact inputs <=1s (met, <=750ms); impact-scoped is a
scoped exception — ~500ms of its ~0.9-1.1s sits in the pre-existing,
byte-identical `impact_candidates` CTE (1503-scope fan-in over the impact
repository index), named as the next measured bottleneck with a follow-up
issue filed in this goal. Shim index builds on the primary: 405.8s +
398.9s sequential (804.7s combined, CONCURRENTLY, writer-safe); each index
149MB vs 91GB table (0.16%). Combined build exceeds the 600s default, so
the chart default `schemaBootstrap.activeDeadlineSeconds` moves 600 to
3600 in this diff (matching the QA/AWS overlays); expect a ~14-minute
large-database upgrade window. Shape pins live in
`supply_chain_advisory_evidence_sql_test.go`
(`TestAdvisoryEvidenceQueryProbesLookupValueFirstWithoutScopeFanout`).

No-Observability-Change (#9474): the route still emits
`query.advisory_evidence`, the Postgres query duration histogram, HTTP
status/error bodies, truth envelope metadata, `count`, `limit`,
`truncated`, and `next_cursor`. The change adds no graph query, reducer
lane, worker, runtime knob, or metric label.

No-Observability-Change: the route still emits `query.advisory_evidence`, the
Postgres query duration histogram, HTTP status/error bodies, truth envelope
metadata, `count`, `limit`, `truncated`, and `next_cursor`. The change adds no
graph query, queue, reducer lane, worker, runtime knob, or metric label.
