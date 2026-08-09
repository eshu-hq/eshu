# prod-context-overview — production validation

Validation-Slug: prod-context-overview
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.context_overview passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.context_overview -> mcp:get_repo_context

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.context_overview` (tools `get_repo_context`,
`get_service_context`, `get_ecosystem_overview`, `get_repo_story`,
`get_repo_summary`, `get_service_story`, `get_service_intelligence_report`,
`get_workload_context`, `get_workload_story`, `investigate_service`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 6000`,
`max_truth_level: exact`.

## Claim validated

The one-call platform context envelope (repository, service, workload, and
ecosystem context, plus the bounded story/summary/dossier/intelligence-report/
investigation readbacks over that same context truth) resolves through the
authoritative graph with result-limit accounting and scoped-token
authorization enforced before any data returns.

## Committed reproducible evidence

**Handler-level context, story, and result-limit contract** —
`go/internal/query/workload_context_test.go`:
`TestGetWorkloadContextReturnsEnrichedResponse` and
`TestGetServiceContextAcceptsQualifiedWorkloadID`;
`go/internal/query/context_story_envelope_test.go`:
`TestGetWorkloadContextReturnsEnvelopeWhenRequested`,
`TestGetWorkloadStoryReturnsEnvelopeWhenRequested`, and
`TestGetEntityContextReturnsEnvelopeWhenRequested`;
`go/internal/query/context_story_limits_test.go`:
`TestGetWorkloadContextReturnsResultLimitsAndPartialReasons`. Reproduce:

```bash
cd go && go test ./internal/query -run 'WorkloadContext|ServiceContext|ServiceStory|EntityContext' -count=1
```

**Scoped-token authorization before backend calls** —
`go/internal/query/service_context_authz_test.go`:
`TestGetWorkloadContextGraphAppliesScopedAuthBeforeReturn` and
`TestGetWorkloadContextEmptyGrantReturnsNotFoundWithoutBackendCalls`.

**Full-stack Docker Compose route parity** —
`scripts/verify_relationship_platform_compose.sh`'s `verify_service_contexts`
step drives `get_repo_context`/`get_service_context`-equivalent HTTP routes
against a live Compose stack (`SERVICE_CONTEXT_FILE`, `CONTEXT_FILE`
response captures), exercising the `deployed_services` runtime the production
profile names. Reproduce (requires Docker Compose):

```bash
scripts/verify_relationship_platform_compose.sh
```

## Notes

No private data: cited tests use synthetic fixture repositories/workloads: no
production credentials, deployment-specific values, or customer data appear
in this artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
