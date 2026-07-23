# prod-read-only-cypher — production validation

Capability: `graph_query.read_only_cypher` (tool `execute_cypher_query`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: bounded_query_window`, `p95_latency_ms: 2000`,
`max_truth_level: exact`. Diagnostics-only read path; purpose-built tools
are preferred for prompt contracts.

## Claim validated

Read-only Cypher capped by server-side `LIMIT` and deadline/timeout; mutation
keywords are rejected before any graph read, including limits smuggled
inside string literals.

## Committed reproducible evidence

**Mutation rejection, bounded limit/deadline enforcement** —
`go/internal/query/code_cypher_handler_test.go`:
`TestValidateReadOnlyCypher_RejectsMutationKeywords`,
`TestValidateReadOnlyCypher_AllowsReadOnlyQueries`,
`TestValidateReadOnlyCypher_RejectsLongQueries`,
`TestHandleCypherQuery_RejectsMutations`,
`TestHandleCypherQueryRejectsMutationWithEnvelopeError`,
`TestHandleCypherQueryRejectsUnsupportedProfileBeforeGraph`,
`TestHandleCypherQuery_ExecutesReadOnlyQuery`,
`TestHandleCypherQueryPassesDeadlineToGraph`,
`TestHandleCypherQueryAddsBoundedLimitAndEnvelope`,
`TestHandleCypherQueryRejectsExplicitLimitAboveRequestedLimit`,
`TestHandleCypherQueryIgnoresLimitInsideStringLiteral`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleCypherQuery -count=1
cd go && go test ./internal/query -run TestValidateReadOnlyCypher -count=1
```

**Additional validation coverage** — `go/internal/query/code_cypher_test.go`.

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
