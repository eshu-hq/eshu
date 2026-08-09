# prod-supply-chain-impact-explain — production validation

Validation-Slug: prod-supply-chain-impact-explain
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: supply_chain.impact_explanation.read passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
