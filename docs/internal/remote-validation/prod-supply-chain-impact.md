# prod-supply-chain-impact — production validation

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
