# Supply Chain Impact Readiness Performance

This note records bounded query-shape evidence for
`GET /api/v0/supply-chain/impact/findings` readiness.

No-Regression Evidence: issue #1022 scopes advisory readiness counts to the
requested source anchor. CVE-scoped reads count advisory facts for that CVE.
Package-, repository-, and subject-digest-scoped reads first derive the bounded
package set from the explicit `package_id`, reducer package-consumption rows, or
SBOM component rows, then count only advisory package facts whose `package_id`
matches that set. Unanchored calls retain the previous all-active behavior only
for internal callers that supply no CVE, package, repository, subject digest, or
impact-status anchor. Focused proof:
`go test ./internal/query -run TestPostgresSupplyChainImpactReadinessScopesAdvisoryFacts -count=1`.

Observability Evidence: the readiness route continues to use
`query.supply_chain_impact_findings`, the Postgres query duration histogram,
the readiness envelope's `evidence_sources[]`, `source_snapshots[]`,
`source_states[]`, `missing_evidence[]`, and `freshness` fields. The change adds
no route, graph query, queue, reducer lane, worker, runtime knob, metric
instrument, span, log key, or metric label.
