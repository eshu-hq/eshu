# prod-contract-impact — production validation

Validation-Slug: prod-contract-impact
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.contract_impact passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.contract_impact -> mcp:investigate_contract_impact

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.contract_impact` (tool `investigate_contract_impact`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 2000`,
`max_truth_level: exact`.

## Claim validated

Deterministic HTTP-provider contract-impact rows are derived from graph-backed
`Endpoint` evidence, with truncation and provenance surfaced explicitly; the
`topic` and `grpc` endpoint families return explicit unsupported states rather
than fabricated rows until they are projected.

## Committed reproducible evidence

**Handler contract, family deferral, and scope gating** —
`go/internal/query/contract_impact_test.go`:
`TestContractImpactRequiresSupportedProfile` (profile gating),
`TestContractImpactRejectsUnscopedRequest` (scope requirement),
`TestContractImpactDefaultsFamilyToHTTP`,
`TestContractImpactReportsGRPCDeferralWithoutGraphRead` (explicit unsupported
family, no fabricated read), and
`TestContractImpactHTTPProvidersUseScopedEndpointQuery` (scoped-token
authorization applied before the `Endpoint` graph query). Reproduce:

```bash
cd go && go test ./internal/query -run TestContractImpact -count=1
```

**OpenAPI contract lockstep** —
`go/internal/query/openapi_contract_impact_test.go` proves the documented HTTP
contract matches the handler's actual request/response shape.

**Scoped-token authorization on the mounted route** —
`go/internal/query/auth_scoped_routes_impact_test.go` covers scoped-token
route admission for the impact family that includes contract-impact.

## Notes

No private data: cited tests use synthetic `Endpoint` fixtures; no production
credentials or deployment-specific values appear in this artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
