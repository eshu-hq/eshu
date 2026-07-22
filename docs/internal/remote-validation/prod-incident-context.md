# prod-incident-context — production validation

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
