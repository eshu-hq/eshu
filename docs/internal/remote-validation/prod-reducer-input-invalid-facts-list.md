# prod-reducer-input-invalid-facts-list — production validation

Capability: `operator.reducer_input_invalid_facts.list` (tool
`list_reducer_input_invalid_facts`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: scope_generation_domain_fact_kind`, `p95_latency_ms: 1500`,
`max_truth_level: exact`. Durable recompute-on-demand read surface for
`input_invalid` quarantines.

## Claim validated

Bounded `reducer_input_invalid_facts` read (issue-4630) requiring
`scope_id`, `generation_id`, `limit`, and `timeout_ms`; scoped tokens are
restricted to granted repository/scope ids; no raw payload output other than
`fact_id`/`fact_kind`/`missing_field`/`failure_class`.

## Committed reproducible evidence

**Handler bounds, filters, scoped grants, telemetry** —
`go/internal/query/admin_input_invalid_facts_test.go`:
`TestListInputInvalidFactsRequiresScopeGenerationLimitAndTimeout`,
`TestListInputInvalidFactsEmpty`,
`TestListInputInvalidFactsFiltersAndTruncates`,
`TestListInputInvalidFactsScopedGrants`,
`TestListInputInvalidFactsScopedRepositoryOnlyGrantReachesStore`,
`TestListInputInvalidFactsScopedEmptyGrantSkipsStore`,
`TestListInputInvalidFactsRecordsTelemetry`,
`TestAdminHandler_InputInvalidFactsQueryLiveRepositoryScopedGrant`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestListInputInvalidFacts -count=1
```

**MCP tool dispatch registration** — `go/internal/mcp/tools_test.go`:
`TestEveryRegisteredToolHasDispatchRoute` (asserts
`list_reducer_input_invalid_facts` has a live dispatch route). Reproduce:

```bash
cd go && go test ./internal/mcp -run TestEveryRegisteredToolHasDispatchRoute -count=1
```

**Design and write-path documentation** —
`docs/internal/evidence/4630-input-invalid-quarantine-read-surface.md`
records the reducer's best-effort idempotent quarantine writer
(`ReducerInputInvalidFactStore.WriteQuarantinedFacts`,
`go/internal/storage/postgres/reducer_input_invalid_facts.go`) that this read
surface serves.

## Notes

No private data: this artifact cites only committed tests and a committed
evidence note; the read surface itself withholds raw payloads by design.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
