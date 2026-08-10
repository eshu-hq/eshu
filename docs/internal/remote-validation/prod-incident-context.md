# prod-incident-context — production validation

Validation-Slug: prod-incident-context
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: incident.context.read passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: incident.context.read -> mcp:get_incident_context

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `incident.context.read` (tool `get_incident_context`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: incident_or_service_time_window`, `p95_latency_ms: 1500`,
`max_truth_level: derived`.

## Claim validated

Bounded active-incident source-fact read plus declared/applied/live routing
slots; runtime, Jira, PR, image, build, deploy, and commit edges are
explicitly reported missing rather than guessed until proven by other
evidence.

## Committed reproducible evidence

**Handler and missing-evidence-slot behavior** —
`go/internal/query/incident_context_handler_test.go`,
`go/internal/query/incident_context_model_test.go`,
`go/internal/query/incident_context_store_test.go`,
`go/internal/query/incident_context_routing_test.go`,
`go/internal/query/incident_context_runtime_test.go`,
`go/internal/query/incident_context_scope_test.go`,
`go/internal/query/incident_context_truncation_test.go`. Reproduce:

```bash
cd go && go test ./internal/query -run TestIncidentContext -count=1
cd go && go test ./internal/query -run TestGetIncidentContext -count=1
```

**OpenAPI contract declaration** —
`go/internal/query/openapi_incident_context_test.go` proves the production
route is contract-declared.

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
