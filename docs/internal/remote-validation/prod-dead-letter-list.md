# prod-dead-letter-list — production validation

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
