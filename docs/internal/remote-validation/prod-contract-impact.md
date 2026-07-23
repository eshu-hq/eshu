# prod-contract-impact — production validation

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
