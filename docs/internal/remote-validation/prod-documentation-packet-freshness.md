# prod-documentation-packet-freshness — production validation

Validation-Slug: prod-documentation-packet-freshness
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: documentation_evidence_packet.freshness passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `documentation_evidence_packet.freshness` (tool
`check_documentation_evidence_packet_freshness`). Production profile:
`required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 800`, `max_truth_level: exact`.

## Claim validated

Documentation evidence packet freshness is served by `packet_id`, reusing the
same permission-gated packet read model as
`documentation_evidence_packet.read`.

## Committed reproducible evidence

**Handler contract** — `go/internal/query/documentation_test.go`:
`TestDocumentationHandlerReturnsPacketFreshness`. Reproduce:

```bash
cd go && go test ./internal/query -run TestDocumentationHandlerReturnsPacketFreshness -count=1
```

## Notes

No private data: the cited test uses a synthetic finding/packet fixture; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
