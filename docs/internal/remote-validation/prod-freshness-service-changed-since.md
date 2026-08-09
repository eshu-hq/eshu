# prod-freshness-service-changed-since — production validation

Validation-Slug: prod-freshness-service-changed-since
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: freshness.service_changed_since passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: freshness.service_changed_since -> mcp:get_service_changed_since

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `freshness.service_changed_since` (tool
`get_service_changed_since`). Production profile:
`required_runtime: deployed_services`,
`max_scope_size: service_id_since_service_generation`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded service-scope changed-since delta diffing a prior service
materialization generation's evidence snapshot set against the current
active generation's set in `service_evidence_snapshots` (ownership family
only in stage 1), with an unknown `service_id` returning `service_not_found`,
an unresolved `since_generation_id` returning `not_found`, and no current
active service generation returning an explicit `unavailable` diff instead
of fabricated zero deltas.

## Committed reproducible evidence

**Handler contract, required-parameter validation, and not-found/unavailable
states** — `go/internal/query/freshness_service_changed_since_test.go`:
`TestServiceChangedSinceUnchangedProducesNoFalseDeltas`,
`TestServiceChangedSinceUnknownServiceNotFound`,
`TestServiceChangedSinceUnavailableWhenNoActiveGeneration`,
`TestServiceChangedSinceUnknownPriorGenerationNotFound`,
`TestServiceChangedSinceRequiresServiceID`, and
`TestServiceChangedSinceRequiresSinceReference`. Reproduce:

```bash
cd go && go test ./internal/query -run TestServiceChangedSince -count=1
```

## Notes

No private data: cited tests use synthetic service/generation fixtures; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
