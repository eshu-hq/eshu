# prod-dead-letter-list — production validation

Validation-Slug: prod-dead-letter-list
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: operator.dead_letters.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `operator.dead_letters.list` (tool `list_dead_letter_work_items`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: failure_class_domain_scope_collector_time_window`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

First-class operator dead-letter triage surface: a bounded, required-`limit`
and required-`timeout_ms` read over `fact_work_items` dead-letter state, with
deterministic `updated_at desc`/`work_item_id asc` ordering, limit-plus-one
truncation, component-scoped visibility, and scoped tokens restricted to
granted repository/scope IDs. No raw failure message or payload is exposed.

## Committed reproducible evidence

**Handler contract, truncation, and scope gating** —
`go/internal/query/admin_dead_letters_test.go`:
`TestAdminHandler_DeadLettersQueryRequiresLimitAndTimeout`,
`TestAdminHandler_DeadLettersQueryFiltersAndTruncates`, and
`TestAdminHandler_DeadLettersQueryScopedGrants`. Reproduce:

```bash
cd go && go test ./internal/query -run TestAdminHandler_DeadLetters -count=1
```

**MCP tool wiring** — `go/internal/mcp/dead_letters_test.go`:
`TestDeadLetterWorkItemsToolResolvesToAdminQueryRoute` and
`TestDeadLetterWorkItemsToolRequiresLimitAndTimeout`. Reproduce:

```bash
cd go && go test ./internal/mcp -run TestDeadLetterWorkItems -count=1
```

**Live-seeded-row integration proof** —
`go/internal/query/admin_dead_letters_test.go`:
`TestAdminHandler_DeadLettersQueryLiveSeededRow` seeds a real dead-letter row
against a live Postgres store and asserts it round-trips through the read
surface, matching the local full-stack profile's
`integration_test: dead-letter-list-live-seeded-row` entry.

## Notes

No private data: the read surface itself redacts raw failure message/payload
by contract, and the cited tests use synthetic work-item fixtures.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
