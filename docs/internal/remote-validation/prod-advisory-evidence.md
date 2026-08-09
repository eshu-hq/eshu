# prod-advisory-evidence — production validation

Validation-Slug: prod-advisory-evidence
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: supply_chain.advisory_evidence.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
