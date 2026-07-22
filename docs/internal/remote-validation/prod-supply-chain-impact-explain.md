# prod-supply-chain-impact-explain — production validation

Capability: `supply_chain.impact_explanation.read` (tools
`explain_supply_chain_impact`, `export_supply_chain_impact_packet`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: one_finding_or_advisory_package_repository_path`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

One reducer-owned finding explanation anchored by `finding_id` or
advisory/CVE plus package, repository, or image digest; hydrates only
referenced evidence fact ids (no whole-graph explain).

## Committed reproducible evidence

**Bounded-input requirement, canonical finding rows, and evidence chain** —
`go/internal/query/supply_chain_impact_explain_test.go`:
`TestSupplyChainExplainImpactRequiresBoundedInput`,
`TestSupplyChainExplainImpactQueryUsesCanonicalFindingRows`,
`TestSupplyChainExplainImpactQueryKeepsRollingUpgradeFindingIDStable`,
`TestSupplyChainExplainImpactFindingIncludesEvidenceChain`,
`TestBuildSupplyChainImpactExplanationCoversEvidenceClasses`, and
`TestSupplyChainExplainImpactNoEvidenceResponse`. Reproduce:

```bash
cd go && go test ./internal/query -run TestSupplyChainExplainImpact -count=1
cd go && go test ./internal/query -run TestBuildSupplyChainImpactExplanationCoversEvidenceClasses -count=1
```

**Refusal, anchor, authorization, and review-scope contracts** —
`go/internal/query/supply_chain_impact_explain_refusal_test.go`:
`TestSupplyChainExplainImpactAmbiguousScope` and
`TestSupplyChainImpactAmbiguousExplanationUsesCandidateCount`;
`supply_chain_impact_explain_anchor_test.go`:
`TestSupplyChainExplainImpactAcceptsWorkloadAndServiceAnchors` and
`TestSupplyChainExplainImpactNoEvidenceSurfacesUnsupportedEcosystem`;
`supply_chain_impact_explain_authz_test.go`:
`TestSupplyChainImpactExplainScopedGrantsAcrossTenants`; and
`supply_chain_impact_explain_review_test.go`:
`TestBuildSupplyChainImpactExplanationOmitsEmptyDependencyChain`. Reproduce:

```bash
cd go && go test ./internal/query -run 'TestSupplyChainExplainImpact|TestSupplyChainImpactAmbiguousExplanation|TestSupplyChainImpactExplainScopedGrantsAcrossTenants|TestBuildSupplyChainImpactExplanation' -count=1
```

**Deployed-services remediation-benchmark readback** —
`scripts/verify-remote-e2e-remediation-benchmark.sh` drives both the
`GET /api/v0/supply-chain/impact/explain` HTTP route and the
`explain_supply_chain_impact` MCP tool against a live deployed stack and
records public-safe counts, states, and provenance.
`scripts/test-verify-remote-e2e-remediation-benchmark.sh` is the script's own
local proof, run without live credentials against the fixture at
`scripts/lib/test-verify-remote-e2e-remediation-benchmark-mcp-impact-explain.json`:

```bash
scripts/test-verify-remote-e2e-remediation-benchmark.sh
```

## Notes

No private data: cited evidence covers CVE/package/repository/digest anchors
and evidence-fact-id references only.

Related: #5552 (burn-down).
