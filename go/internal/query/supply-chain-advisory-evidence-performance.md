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
Remote preserved-volume proof on the representative Compose backlog applied the
new DDL in 241s, then returned `CVE-2021-44228` in 0.691s cold and 0.435s /
0.439s warm with 2 source observations and 5 affected package rows. A missing
`CVE-2099-0000` returned 0 rows in 0.363s / 0.234s / 0.307s. `EXPLAIN
ANALYZE` used the three `fact_records_vulnerability_active_*_lookup_v2_idx`
indexes and completed the present-CVE SQL in 472.419ms.

No-Regression Evidence: issue #1598 adds repository, workload, and service
scopes by seeding advisory lookup keys from active
`reducer_supply_chain_impact_finding` rows that already own target impact
truth. The final result rows still come from active `vulnerability.*` source
facts, and the impact seed path is gated by equality predicates on
`repository_id`, `workload_ids`, or `service_ids` plus the existing bounded
fact-row limit. Focused proof:
`go test ./internal/query -run 'TestSupplyChainListAdvisoryEvidence|TestAdvisoryEvidence(Query|Lookup)' -count=1`
and
`go test ./internal/mcp -run 'TestResolveRouteMapsAdvisoryEvidenceToBoundedQuery' -count=1`.

No-Observability-Change: the route still emits `query.advisory_evidence`, the
Postgres query duration histogram, HTTP status/error bodies, truth envelope
metadata, `count`, `limit`, `truncated`, and `next_cursor`. The change adds no
graph query, queue, reducer lane, worker, runtime knob, or metric label.
