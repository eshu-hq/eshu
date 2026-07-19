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
- exact `relationship_type`, `source_id`, and `target_id` values for topology
  edges.

The subject backbone remains explicit: the source repository `DEFINES` the
selected workload and every returned runtime instance is connected to that
workload by its exact `INSTANCE_OF` edge. The console validates those endpoints
against the selected `repo_id`, `workload_id`, and returned `instance_id`
values; mismatched subject edges are omitted and disclosed.

Direct placement renders only returned `RUNS_ON` instance-to-platform edges.
Repository provisioning fallback renders its two returned relationships:
`PROVISIONS_DEPENDENCY_FOR` to the service repository and
`PROVISIONS_PLATFORM` to the platform, in a separate top-level provisioned
platform group rather than copying the platform beneath every instance. It
never becomes an inferred `RUNS_ON`. The instance `environment` value remains
structured evidence and is not rendered as a canonical node or relationship.
Its human-readable inspect control pivots to the canonical runtime-instance
node that owns the environment attribute.

Rows without the required canonical key are omitted from the topology and
reported in the graph-composition limitations. Distinct IDs with the same label
remain distinct nodes. Repeated observations of the same canonical ID are
deduplicated and counted. Cloud and Kubernetes evidence remains in structured
evidence groups when the trace does not supply exact topology endpoints.

The graph reports input, rendered, duplicate, and omitted node/edge counts. It
is deterministically capped at 60 nodes and 120 edges; exceeding either cap sets
`truncated=true` and reports the omitted cardinality.

An ambiguous change-surface target does not select a ready deployment trace:
the graph remains in change-surface mode and states that topology was withheld
because the service identity is ambiguous. Trace evidence rows whose canonical
IDs fall outside the bounded graph are labeled `Outside bounded graph` instead
of rendering an inspect control that cannot select a node.

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
ORDER BY instance_id, platform_name, platform_id
```

The direct query also projects `properties(runsOn)` so confidence, rationale,
and provenance stay attached to the exact endpoints. The unambiguous
repository fallback remains one indexed repository-anchored read and returns
the distinct properties and endpoints of both provisioning relationships.
Focused tests prove exact direct and fallback relationship families, endpoints,
provenance, direct precedence, and ambiguous-fallback withholding.

Deployment-source expansion is bounded at the graph reads: each relationship
family orders deterministically and reads at most 51 rows for a 50-row response
limit. The merged response reports its limit, returned and observed counts,
per-family observed counts, ordering, truncation, and whether the observed
count is only a lower bound. A 51-row failing-first regression proves the
sentinel behavior and prevents an unbounded downstream GitOps expansion.

No-Observability-Change: the backend read retains the shared `GraphQuery.Run`
adapter, `neo4j.query` spans, query-duration metrics, service-query stage timer,
and row-count logging. The console adds in-band composition metadata only. No
metric, span, log field, graph write, queue behavior, worker, retry, cache,
runtime knob, or API call was added.

After rebasing over the production query-plan guardrail, the exact
`QP-SVC-RUNS-ON` production source and Cypher fingerprints were refreshed for
the additive endpoint and provenance projections. The deployment-source query
fingerprints include the deterministic sentinel limits. Final query-plan
regression and live-profile results are recorded below after the exact branch
gate is rerun.

## Golden contract

The existing B-12 `trace_deployment_chain` MCP shape remains anchored to
`api-svc`, preserving its broad service-evidence response contract. A separate
authenticated HTTP golden shape calls
`POST /api/v0/impact/trace-deployment-chain` for the corpus's positive runtime
fixture, `deployable-config`, and requires:

- `data.instances[].platforms[].platform_id`;
- `data.instances[].platforms[].topology_basis`;
- `data.instances[].platforms[].topology_edges[].relationship_type` pinned to
  `RUNS_ON` for the direct fixture;
- `data.instances[].platforms[].topology_edges[].source_id` and `target_id`;
- `data.deployment_sources[].relationship_type`;
- `data.deployment_sources[].source_id`;
- `data.deployment_sources[].target_id`;
- deployment-source limit, returned-count, and truncation fields;
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
run. The inspected NornicDB image was bound to RepoDigest
`sha256:f9250ff06e7bd311f7b6137dbb53773df77540af4929091dbcb7d5dfaeea137a`
and OCI source revision
`1492458852588c884c32f70d27ea2ee07086769c`; the tag below is the human-readable
alias for that inspected image. The clean NornicDB v1.1.11 gate command was:

```bash
NORNICDB_IMAGE=tianthyss/nornicdb-cpu-bge:v1.1.11 \
ESHU_POSTGRES_PORT=25432 \
NEO4J_BOLT_PORT=17687 \
NEO4J_HTTP_PORT=17474 \
GATE_API_PORT=28080 \
GATE_MCP_PORT=28091 \
scripts/verify-golden-corpus-gate.sh
```

Result: **420 pass, 0 required-fail, 0 advisory-warn** in 36 seconds. The gate
reported bootstrap in 3 seconds, collector replay in 21 seconds, first drain in
6 seconds, maintenance drains in 6 seconds, and graph/query checks in 4
seconds. Both the authenticated positive HTTP topology shape and the existing
MCP trace shape passed.

After rebasing onto
`origin/main@1e697fd22c3904930bb0e1fa2b20371fa3580f20`, the full gate was rerun
at exact implementation head `c8778875bf207c94ffe8f381786316785895f51b` on
isolated ports:

```bash
ESHU_POSTGRES_PORT=16533 \
NEO4J_BOLT_PORT=8788 \
NEO4J_HTTP_PORT=8576 \
GATE_API_PORT=19081 \
GATE_MCP_PORT=19092 \
GATE_DRAIN_TIMEOUT=10m \
GATE_BUDGET_SECONDS=900 \
bash scripts/verify-golden-corpus-gate.sh
```

The post-rebase run completed in 33 seconds with **420 pass, 0
required-fail, and 0 advisory-warn**. Its phase timings were 2 seconds for
bootstrap, 20 seconds for collector replay, 6 seconds for the first drain, 5
seconds for maintenance drains, and 3 seconds for graph/query checks. The
isolated containers, network, and volumes were removed by the gate; the
retained development stack was not changed.

Replay coverage also passed:

```bash
cd go && go test ./conformance ./internal/replay/... \
  ./cmd/replay-coverage-gate ./internal/replaycoverage -count=1
scripts/test-verify-replay-coverage-gate.sh
scripts/verify-replay-coverage-gate.sh --blocking
```

The blocking gate reported 386 pass, 0 required-fail, and 17 existing advisory
coverage gaps; the blocking coverage threshold passed.

## Retained API tuple proof

The exact branch API was run against the retained Postgres and NornicDB stores
without rebuilding or replacing the retained stack. Three consecutive
authenticated `api-node-boats` deployment-trace calls returned HTTP 200 and the
same 180,016-byte response in 0.828, 0.193, and 0.183 seconds. The response
contained six canonical workload instances, nine unique direct `RUNS_ON`
relationship type/source/target tuples, both ECS and Kubernetes platforms, and
14 of 14 deployment-source relationships with `truncated=false`.

A read-only graph query over the same six instance IDs returned each
`environment` property and no instance-to-`Environment` relationship or
environment node endpoint. The console therefore keeps those six environment
values as instance attributes and does not manufacture environment nodes,
relationships, or inspect controls. No unambiguous repository-provisioning
fallback exists for this retained anchor; the direct-placement proof is the
applicable live path. Focused tests cover the fallback relationship family;
the golden fixture pins the direct `RUNS_ON` family.

## Console bundle performance

The first production build after the two upstream merges measured the eager
main chunk at **728.8 KiB**, above the blocking **726.0 KiB** budget. Raising
the threshold was rejected. The Impact route now uses the same route-level
`React.lazy` boundary as other specialized console workbenches and exposes a
visible `Loading impact` state while its route chunk loads.

On the same rebased worktree and build command, the eager main chunk fell to
**696.6 KiB** and the final async `ImpactPage` chunk measured **36.8 KiB**. This
is a 32.2 KiB reduction in first-load JavaScript while preserving the complete
Impact route behind its explicit loading boundary. `npm run console:build`
reported all 81 emitted chunks within their existing budgets; no threshold or
budget configuration changed. A failing-first route regression proved the
Impact implementation was eager before the split and green after the lazy
boundary.

## Authenticated retained-browser proof

The earlier browser run on head `a4fc312020` is invalidated: it preceded the
exact topology-edge contract and therefore included relationships that the
mapper no longer synthesizes. It is retained only as historical diagnostic
context and is not merge-readiness evidence.

Final proof must use the authenticated retained browser on the exact reviewed
head and compare API relationship type/source/target tuples with the rendered
DOM. It must cover direct placement, provisioning fallback when present,
environment-as-attribute behavior, source-limit disclosure, graph bounds, and
click-to-visible completion. Those results are pending and will replace this
paragraph before the branch is pushed.
