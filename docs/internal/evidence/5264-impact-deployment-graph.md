# Impact deployment graph evidence (#5264)

## Accuracy contract

The Impact console composes deployment topology only from the existing
`POST /api/v0/impact/trace-deployment-chain` response. It does not translate a
service name into repository blast-radius semantics and does not join evidence
by a display label.

Topology identity uses the response's backend-owned keys:

- `repo_id` for source and deployment-config repositories;
- `workload_id` for the service workload;
- `instance_id` for materialized runtime instances;
- `platform_id` for ECS and Kubernetes Platform nodes; and
- `Environment.name`, the graph schema's merge key, for environment nodes.

Rows without the required canonical key are omitted from the topology and
reported in the graph-composition limitations. Distinct IDs with the same label
remain distinct nodes. Repeated observations of the same canonical ID are
deduplicated and counted. Cloud and Kubernetes evidence remains in structured
evidence groups when the trace does not supply exact topology endpoints.

The graph reports input, rendered, duplicate, and omitted node/edge counts. It
is deterministically capped at 60 nodes and 120 edges; exceeding either cap sets
`truncated=true` and reports the omitted cardinality.

## Backend read shape

The workload-context platform lookup remains one bounded read anchored by the
exact `WorkloadInstance.id` batch:

```cypher
MATCH (i:WorkloadInstance)-[runsOn:RUNS_ON]->(p:Platform)
WHERE i.id IN $instance_ids
RETURN i.id as instance_id,
       p.id as platform_id,
       p.name as platform_name,
       p.kind as platform_kind,
       runsOn.confidence as platform_confidence,
       runsOn.reason as platform_reason,
       properties(runsOn) as platform_edge
ORDER BY instance_id, platform_name
```

No-Regression Evidence: the OLD and NEW shapes have identical labeled anchors,
`WHERE`, traversal, relationship family, parameters, ordering, row cardinality,
and one-Bolt-call fan-out. NEW adds only the scalar `p.id AS platform_id`
projection required to preserve canonical Platform identity in the existing
response. Focused tests prove the exact instance-ID batch, one graph call,
canonical ID propagation for direct `RUNS_ON` and unambiguous provisioned
platform fallback, OpenAPI shape, same-label/different-ID preservation,
duplicate and missing-identity accounting, and the 60-node/120-edge bounds. No
local NornicDB-New checkout is present, so no live PROFILE is claimed; a planner
comparison is not load-bearing because the MATCH, WHERE, traversal, ORDER BY,
and result row set are unchanged.

No-Observability-Change: the backend read retains the shared `GraphQuery.Run`
adapter, `neo4j.query` spans, query-duration metrics, service-query stage timer,
and row-count logging. The console adds in-band composition metadata only. No
metric, span, log field, graph write, queue behavior, worker, retry, cache,
runtime knob, or API call was added.

## Golden contract

The existing B-12 `trace_deployment_chain` MCP shape remains anchored to
`api-svc`, preserving its broad service-evidence response contract. A separate
authenticated HTTP golden shape calls
`POST /api/v0/impact/trace-deployment-chain` for the corpus's positive runtime
fixture, `deployable-config`, and requires:

- `data.instances[].platforms[].platform_id`;
- `data.deployment_sources[].relationship_type`;
- `data.deployment_sources[].source_id`;
- `data.deployment_sources[].target_id`;
- `truth.level=derived`; and
- `truth.basis=hybrid`.

This split preserves the rich MCP fixture while proving canonical deployment
topology against the fixture that actually materializes a runtime instance. A
retained diagnostic run confirmed the exact graph chain before the final clean
proof:

```text
workload:deployable-config
  <- INSTANCE_OF - workload-instance:deployable-config:production
  - RUNS_ON -> platform:kubernetes:none:production:production:none
  - DEPLOYMENT_SOURCE -> repository:r_1f68383d (deployable-source)
```

The retained diagnostic containers and volumes were removed before the final
run. The clean NornicDB v1.1.11 gate command was:

```bash
NORNICDB_IMAGE=tianthyss/nornicdb-cpu-bge:v1.1.11 \
ESHU_POSTGRES_PORT=25432 \
NEO4J_BOLT_PORT=17687 \
NEO4J_HTTP_PORT=17474 \
GATE_API_PORT=28080 \
GATE_MCP_PORT=28091 \
scripts/verify-golden-corpus-gate.sh
```

Result: **419 pass, 0 required-fail, 0 advisory-warn** in 37 seconds.
Phase timings were bootstrap 3 seconds, collector replay 20 seconds, first
drain 9 seconds, maintenance drains 5 seconds, and graph/query checks 5
seconds. Both the authenticated positive HTTP topology shape and the existing
MCP trace shape passed.

Replay coverage also passed:

```bash
cd go && go test ./conformance ./internal/replay/... \
  ./cmd/replay-coverage-gate ./internal/replaycoverage -count=1
scripts/test-verify-replay-coverage-gate.sh
scripts/verify-replay-coverage-gate.sh --blocking
```

The blocking gate reported 386 pass, 0 required-fail, and 17 existing advisory
coverage gaps; the blocking coverage threshold passed.
