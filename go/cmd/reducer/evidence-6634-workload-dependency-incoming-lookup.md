# Workload dependency incoming lookup proof

This note records the correctness and bounded latency proof for wrapping the
workload dependency lookup's two directional branches in a `CALL` subquery.
The lookup remains one graph round trip and retains the same indexed
`Repository.id` anchors, traversal directions, returned columns, and `UNION`
deduplication.

## Theory and environment

The theory was that NornicDB's known bare top-level `UNION` behavior suppresses
the incoming branch when the outgoing branch is empty. The proof used Eshu
source `2402cd8eacf4ca5360bb516c8a3a56ce32695592` and the retained fresh-stack
first cell from `scripts/verify-ifa-determinism.sh`, after
`eshu-bootstrap-data-plane` applied the production schema:

- image: `timothyswt/nornicdb-cpu-bge:v1.3.2`
- immutable image/index digest:
  `sha256:a47ae7eadc80229d3109ade7a57dfc1f1504b7586798859e2b2ac6fc38897440`
- effective graph settings: authentication disabled; embeddings, BM25, vector
  search, and asynchronous writes disabled; no pprof listener configured
- schema: `repository_id` uniqueness constraint and
  `nornicdb_repository_id_lookup` property index on `Repository(id)`, both
  online
- input: one `repo_ids` value; one matching target `Repository`; zero outgoing
  `DEPENDS_ON` edges; one incoming `DEPENDS_ON` edge; maximum observed fan-out
  for the selected anchor was one
- expected output: one narrow `(source_repo_id, target_repo_id)` row

No current NornicDB source checkout was available under the locally documented
checkout locations. The pinned binary was therefore the behavior oracle, and
the repository's already measured `CALL { ... UNION ... }` portability rule in
`docs/public/reference/nornicdb-query-pitfalls.md` selected the candidate shape.

The read-only theory shim used the production query text over the Bolt-HTTP
endpoint, with the target-only repository ID in `repo_ids`:

```bash
curl -sS -u "$proof_basic_auth" -H 'Content-Type: application/json' \
  -d "$bare_union_payload" \
  "${NORNICDB_HTTP_BASE}/db/nornic/tx/commit"
curl -sS -u "$proof_basic_auth" -H 'Content-Type: application/json' \
  -d "$incoming_branch_payload" \
  "${NORNICDB_HTTP_BASE}/db/nornic/tx/commit"
```

The bare production query returned zero rows in 3.005 ms. The incoming branch
alone returned its one expected row in 2.377 ms. Separate anchor checks returned
`target_anchor_count=1`, `outgoing_count=0`, and `incoming_count=1`. This proves
the query shape, rather than missing graph truth, caused the empty result.

## Red and green contract proof

The live regression seeds one uniquely named source/target pair in an isolated
container, verifies the incoming branch directly as a positive control, and
then invokes `neo4jWorkloadDependencyLookup.ListRepoDependencyEdges` through
the production `query.NewNeo4jReader`. It also covers outgoing-only lookup,
both endpoints without duplicate results, and an unknown repository:

```bash
cd go
proof_tmp="$(mktemp -d)"
proof_cache="$(mktemp -d)"
TMPDIR="$proof_tmp" GOTMPDIR="$proof_tmp" GOCACHE="$proof_cache" \
  GOTOOLCHAIN=go1.26.6 CC=clang CGO_CFLAGS=-std=gnu17 \
  ESHU_WORKLOAD_DEPENDENCY_LOOKUP_LIVE=1 \
  ESHU_NEO4J_URI="$proof_bolt_uri" \
  go test ./cmd/reducer \
    -run 'TestWorkloadDependencyLookup(ListsRepoDependencyEdgesWithAnchoredDirections|ReturnsIncomingOnlyEdgeLive)$' \
    -count=1 -v
```

Before the implementation change, both regressions failed: the live production
lookup returned zero rows for the incoming-only case, and the query-shape test
reported that the production query lacked `CALL {`. After wrapping the two
unchanged branches and adding the plain outer return, all focused cases passed
(`ok github.com/eshu-hq/eshu/go/cmd/reducer 0.048s`).

## No-regression measurement

No-Regression Evidence: after two warm-ups, seven Bolt-HTTP trials used the
same bootstrapped backend and the same one-source/one-edge outgoing anchor, a
case where both old and new forms return the correct single row. The bare form
measured 0.748-4.068 ms with a 1.171 ms median; the `CALL`-wrapped form measured
0.715-1.347 ms with a 0.982 ms median. Every trial returned exactly one row.
At this sub-millisecond-to-low-millisecond scale the difference is measurement
noise; the result establishes no measurable regression, not a speedup. The
correctness delta is zero to one returned row for the incoming-only case.

No-Observability-Change: this changes only the internal read-query envelope. It
adds no metric, span, log field, status field, worker, queue, retry, runtime
knob, or graph write. Operators retain the existing workload materialization
completion log's `dependency_reconcile_duration_seconds` and
`dependency_write_row_count`, plus the existing Neo4j reader query spans and
duration instrumentation, to distinguish lookup latency from an empty or
written dependency row set.
