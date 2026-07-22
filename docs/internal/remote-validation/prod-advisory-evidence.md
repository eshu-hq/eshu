# prod-advisory-evidence — production validation

Capability: `supply_chain.advisory_evidence.list` (tool `list_advisory_evidence`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: cve_advisory_or_package_scope`, `p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded source-only advisory evidence lookup anchored by CVE, advisory id, or package id,
preserving source disagreement without implying repository or image impact.

## Committed reproducible evidence

**Scope/limit validation and bounded store lookup** — `go/internal/query/supply_chain_advisory_evidence_test.go`:
`TestSupplyChainListAdvisoryEvidenceRequiresScopeAndLimit`,
`TestSupplyChainListAdvisoryEvidenceUsesBoundedStore`,
`TestNormalizeAdvisoryEvidenceFilterCanonicalizesIdentityInputs`, and
`TestPageAdvisoryEvidenceRowsKeepsCVEAnchorScoped` /
`TestPageAdvisoryEvidenceRowsKeepsPackageAnchorBroad`. Reproduce:

```bash
cd go && go test ./internal/query -run TestSupplyChainListAdvisoryEvidence -count=1
```

**Repository-scoped resolution** — `go/internal/query/supply_chain_advisory_evidence_scope_test.go`:
`TestSupplyChainListAdvisoryEvidenceResolvesRepositoryScopedFindings` and
`TestSupplyChainListAdvisoryEvidenceRejectsUnknownRepositorySelectorBeforeRead`. Reproduce:

```bash
cd go && go test ./internal/query -run TestSupplyChainListAdvisoryEvidenceResolves -count=1
```

**Scoped-token authorization** — `go/internal/query/supply_chain_advisory_evidence_scoped_token_test.go`:
`TestAuthMiddlewareWithScopedTokensAllowsAdvisoryEvidenceRoute`,
`TestAdvisoryEvidenceScopedTokenDeniesOutOfGrantRepositoryBeforeStoreRead`, and
`TestAdvisoryEvidenceSQLBoundsImpactSelectorByGrants`. Reproduce:

```bash
cd go && go test ./internal/query -run TestAdvisoryEvidenceScopedToken -count=1
```

**Contract declaration** — `go/internal/query/openapi_supply_chain_test.go`:
`TestOpenAPISpecIncludesAdvisoryEvidenceRepositoryScope`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPISpecIncludesAdvisoryEvidenceRepositoryScope -count=1
```

## Notes

No private data: source-disagreement fixtures use synthetic CVE/advisory/package identifiers only.

Related: #5552 (burn-down).
