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

The B-12 `trace_deployment_chain` MCP shape requires the non-empty deep path
`instances[].platforms[].platform_id` for the deterministic `api-svc` corpus.
This keeps the API, MCP, golden trace, and console identity contract aligned.
