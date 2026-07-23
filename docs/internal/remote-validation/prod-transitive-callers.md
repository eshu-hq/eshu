# prod-transitive-callers — production validation

Capability: `call_graph.transitive_callers` (tool `analyze_code_relationships`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 5000`,
`max_truth_level: exact`.

## Claim validated

`POST /api/v0/code/relationships` with `relationship_type: "CALLS"`,
`transitive: true`, and `direction: "incoming"` returns a depth-labeled,
multi-hop caller chain resolved from the authoritative NornicDB graph — not
just the direct (1-hop) callers a plain `CALLS` lookup would return.

## Live deployed reproduction (issue #5681)

Reproduced against the default Docker Compose stack
(`docker-compose.yaml`, NornicDB backend, `ESHU_GRAPH_BACKEND=nornicdb`) with
a real bootstrap-indexed corpus: the `graph_analysis_compose` proof fixture
(`tests/fixtures/graph_analysis_compose/graph-analysis-go`), the same
purpose-built 3-hop call chain (`main -> entrypointGraphProof ->
dispatchGraphProof -> persistGraphProof`) that
`scripts/verify_graph_analysis_compose.sh` proves against the Neo4j-compat
stack. This artifact reproduces the equivalent proof on the **default
NornicDB stack** the production deployment actually runs.

Stack brought up with `docker compose -f docker-compose.yaml up -d --wait`,
bootstrap-index ran to completion (`/api/v0/index-status` queue fully
drained, zero outstanding/pending/retrying/failed), and the bearer token was
read from the running `eshu` container's sealed `/data/.eshu/.env`
(`ESHU_API_KEY`), matching the documented Compose bootstrap-credential flow
(`docs/public/run-locally/docker-compose.md#bootstrap-admin-credential`).

Request:

```bash
curl -s -H "Authorization: Bearer $token" \
  -H "Accept: application/eshu.envelope+json" \
  -X POST http://127.0.0.1:8080/api/v0/code/relationships \
  -H "Content-Type: application/json" \
  -d '{"name":"persistGraphProof","repo_id":"graph-analysis-go","direction":"incoming","relationship_type":"CALLS","transitive":true,"max_depth":5}'
```

Observed response (redacted — entity ids/hostnames elided, structure and
values otherwise verbatim):

```json
{
  "data": {
    "name": "persistGraphProof",
    "repo_name": "graph-analysis-go",
    "labels": ["Function"],
    "language": "go",
    "outgoing": [],
    "incoming": [
      {"depth": 1, "type": "CALLS", "reason": "transitive_call_graph", "source_name": "dispatchGraphProof"},
      {"depth": 2, "type": "CALLS", "reason": "transitive_call_graph", "source_name": "entrypointGraphProof"},
      {"depth": 3, "type": "CALLS", "reason": "transitive_call_graph", "source_name": "main"}
    ]
  },
  "truth": {
    "level": "exact",
    "capability": "call_graph.transitive_callers",
    "profile": "production",
    "basis": "authoritative_graph",
    "freshness": {"state": "fresh"},
    "reason": "resolved from transitive graph relationships"
  }
}
```

All three transitive hops resolved with the correct depth labeling
(`dispatchGraphProof` at depth 1, `entrypointGraphProof` at depth 2, `main`
at depth 3), matching the fixture's documented call chain exactly. The
`truth` envelope confirms `profile: production`, `basis:
authoritative_graph`, `level: exact` — the production tier's own claimed
truth shape, not a downgraded fallback.

## Committed reproducible go_test contract

- `go/internal/query/code_call_graph_contract_test.go`:
  `TestHandleRelationshipsReturnsTransitiveCallersForNornicDB` (2-hop,
  NornicDB-dialect BFS) and `TestHandleRelationshipsUsesNornicDBBFSForTransitiveCalls`.
- Matrix `local_full_stack` verification:
  `compose_e2e: transitive-callers` — `scripts/verify_graph_analysis_compose.sh`
  (`verify_transitive_callers`), run against the Neo4j-compat Compose stack.

Reproduce the unit contract:

```bash
cd go && go test ./internal/query -run TestHandleRelationshipsReturnsTransitiveCallersForNornicDB -count=1
```

## Notes

No private data: the fixture is a synthetic, purpose-built Go repository
already committed at `tests/fixtures/graph_analysis_compose/`. No production
credentials, hostnames, or account identifiers appear in this artifact; the
bearer token value is never printed above.

Related: #5407 (artifact-existence gate), #5552 (burn-down), #5681 (this
deployed-proof pass).
