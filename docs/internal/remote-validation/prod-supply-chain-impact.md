# prod-supply-chain-impact — production validation

Validation-Slug: prod-supply-chain-impact
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: reachability.go.govulncheck passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
Capability-Assertion: supply_chain.impact_findings.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `supply_chain.impact_findings.list` (tool
`list_supply_chain_impact_findings`), which also carries the
`reachability.go.govulncheck` reachability envelope.
Production profile: `required_runtime: deployed_services`,
`max_scope_size: cve_package_repository_or_digest_scope`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded reducer impact lookup anchored by CVE, package, repository, image
digest, or impact status, plus always-on Go reachability (via govulncheck
call-graph evidence) riding the impact findings envelope as
`reachable`/`not_called`/`unknown`/`unavailable`/`missing_evidence` states.

## Committed reproducible evidence

**Bounded impact-findings lookup and scope anchors** —
`go/internal/query/supply_chain_impact_findings_test.go`:
`TestSupplyChainListImpactFindingsRequiresScopeAndLimit`,
`TestSupplyChainListImpactFindingsUsesBoundedStore`,
`TestSupplyChainListImpactFindingsDoesNotReportPresentCatalogCorrelationAsMissing`,
and `TestSupplyChainListImpactFindingsUsesImageRefScope`. Reproduce:

```bash
cd go && go test ./internal/query -run TestSupplyChainListImpactFindings -count=1
```

**Go reachability classification (govulncheck call-graph evidence)** —
`go/internal/reducer/go_vulnerability_reachability_test.go`:
`TestClassifyGoVulnerabilityReachabilityModuleOnly`,
`TestClassifyGoVulnerabilityReachabilityImportReachable`,
`TestClassifyGoVulnerabilityReachabilitySymbolReachable`,
`TestClassifyGoVulnerabilityReachabilityNotCalled`, and
`TestClassifyGoVulnerabilityReachabilityRequiresOwnedModuleEvidence`. Reproduce:

```bash
cd go && go test ./internal/reducer -run TestClassifyGoVulnerabilityReachability -count=1
```

**Reachability state riding the impact envelope without changing impact truth** —
`go/internal/query/supply_chain_impact_reachability_test.go` and
`go/internal/reducer/supply_chain_impact_reachability_test.go`:
`TestSupplyChainReachabilityStatesPreserveImpactTruth`. Reproduce:

```bash
cd go && go test ./internal/reducer -run TestSupplyChainReachabilityStatesPreserveImpactTruth -count=1
```

**Deployed-services target-story readback** —
`scripts/verify_remote_e2e_target_story.sh` asserts `impact_findings` counts
(`minimums.impact_findings`) against a live deployed stack.
`scripts/test-verify-remote-e2e-target-story.sh` is the script's own local
proof, runnable without live credentials:

```bash
scripts/test-verify-remote-e2e-target-story.sh
```

## Notes

No private data: cited evidence covers CVE/package/repository/digest anchors
and reachability classification only.

Related: #5552 (burn-down).
