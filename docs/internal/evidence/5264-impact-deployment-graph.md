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
When runtime-topology or deployment-source limit metadata is missing or fails
validation, the console preserves those bounded counts but reports
`completeness unverified`; it reserves `complete within bounds` for responses
whose upstream completeness metadata is present and internally consistent.

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
       collect(DISTINCT properties(runsOn)) as platform_edges
ORDER BY instance_id, platform_name, platform_id
LIMIT $platform_edge_limit
```

The direct query groups the distinct `RUNS_ON` property maps by exact endpoints;
the response mapper then selects a deterministic evidence map so confidence,
rationale, and provenance stay attached to those endpoints. The unambiguous
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
fingerprints include the deterministic sentinel limits. Static query-plan
regression and the backend-executed B-7 result are recorded below. The opt-in
`ESHU_QUERYPLAN_PROFILE_LIVE=1` suite was not run for this correctness change,
so this note makes no live PROFILE claim.

The exact static query-plan and performance-evidence commands run after the
review corrections were:

```bash
# From the go/ directory:
GOCACHE=$PWD/.gocache-build5264 go test ./internal/queryplan -count=1
GOCACHE=$PWD/.gocache-build5264 go test ./internal/query \
    -run '^(TestHandlerQueryplanManifestBindsProductionBuilders|TestLegacyQueryplanManifestBindsProductionQueries)$' \
    -count=1
# From the repository root:
bash scripts/test-verify-query-plan-regression.sh
bash scripts/test-verify-performance-evidence.sh
ESHU_PERFORMANCE_EVIDENCE_BASE=2090ca6005b56e176c3bf1e7dbcb1c51c9214782 \
  bash scripts/verify-performance-evidence.sh
```

Results after the final rebase: `internal/queryplan` passed in 0.609 seconds,
the production-builder binding tests passed in 1.029 seconds, the query-plan
script contract reported
`test-verify-query-plan-regression: pass`, the performance-evidence verifier
tests passed, and the branch verifier reported that benchmark and observability
markers were present for the hot-path changes. These are static source/query
contract and evidence-policy results only; no live PROFILE command was run.

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
`origin/main@e8e3928b97c20335bf60cd47ea28a9ce884e9cf7`, the full gate was rerun
at exact implementation head `0a3952c491a16c3e2fae5140f57efcd7c24100e9` on
isolated ports:

```bash
ESHU_POSTGRES_PORT=16534 \
NEO4J_BOLT_PORT=8789 \
NEO4J_HTTP_PORT=8577 \
GATE_API_PORT=19082 \
GATE_MCP_PORT=19093 \
GATE_DRAIN_TIMEOUT=10m \
GATE_BUDGET_SECONDS=600 \
bash scripts/verify-golden-corpus-gate.sh
```

The post-rebase run completed in 35 seconds with **420 pass, 0
required-fail, and 0 advisory-warn**. Its phase timings were 3 seconds for
bootstrap, 20 seconds for collector replay, 7 seconds for the first drain, 5
seconds for maintenance drains, and 5 seconds for graph/query checks. The
isolated containers, network, and volumes were removed by the gate; the
retained development stack was not changed.

After the review fixes that reject deployment-source relationships without
canonical endpoints and preserve bounded runtime-topology metadata, the full
gate was rerun at exact implementation head
`5ad14292b0922ca7fbfcc5795f82b902fa8f27b8` on fresh isolated ports:

```bash
ESHU_POSTGRES_PORT=16535 \
NEO4J_BOLT_PORT=8790 \
NEO4J_HTTP_PORT=8578 \
GATE_API_PORT=19083 \
GATE_MCP_PORT=19094 \
GATE_DRAIN_TIMEOUT=10m \
GATE_BUDGET_SECONDS=600 \
bash scripts/verify-golden-corpus-gate.sh
```

The post-fix run completed in 34 seconds with **420 pass, 0 required-fail,
and 0 advisory-warn**. Its phase timings were 3 seconds for bootstrap, 20
seconds for collector replay, 6 seconds for the first drain, 5 seconds for
maintenance drains, and 3 seconds for graph/query checks. The gate removed its
isolated containers, network, and volumes; the retained development stack was
not changed.

After rebasing onto
`origin/main@2090ca6005b56e176c3bf1e7dbcb1c51c9214782` and resolving the B-12
snapshot by retaining both the merged global-search shape and the deployment
topology shape, the gate was rerun at exact implementation head
`500668e063eb0c085fa44dfdc5e51fe6c717a1fe` after the production response and
both OpenAPI fragments were corrected to expose
`topology_basis=provisioning_fallback`:

```bash
ESHU_POSTGRES_PORT=16537 \
NEO4J_BOLT_PORT=8792 \
NEO4J_HTTP_PORT=8580 \
GATE_API_PORT=19085 \
GATE_MCP_PORT=19096 \
GATE_DRAIN_TIMEOUT=10m \
GATE_BUDGET_SECONDS=600 \
bash scripts/verify-golden-corpus-gate.sh
```

The final contract run completed in 35 seconds with **421 pass, 0
required-fail, and 0 advisory-warn**. Its phase timings were 3 seconds for
bootstrap, 20 seconds for collector replay, 6 seconds for the first drain, 6
seconds for maintenance drains, and 3 seconds for graph/query checks. Both
HTTP shapes passed. The gate removed its isolated containers, network, and
volumes; the retained development stack was not changed.

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

The retained-browser run on application head `4813a48b68` is also superseded
because later review fixes changed completeness normalization and presentation.
Its counts remain diagnostic context, not final merge-readiness evidence.

The retained-browser run on application head `09be7fea10` is superseded by the
final fail-closed completeness corrections. Its valid-path counts remain
diagnostic context, not the final merge-readiness browser proof.

The final retained-browser run used the authenticated console at
`http://127.0.0.1:5194/impact?kind=service&target=api-node-boats` against
application head `fa29dff4cec182a3c3de8d7e0c07a85a9e61f7cf`. The Vite process
was verified to be serving directly from this feature worktree after the final
rebase, while the retained API response shape was independently proven by the
exact backend B-7 run above. The post-B-7 implementation delta is confined to
console normalization, completeness propagation, and their regression tests;
it does not change the API or graph-query contract. The rendered deployment
section agreed with the
retained API tuple proof: six workload
instances, nine `RUNS_ON` relationship type/source/target tuples, 14 deployment
sources, six cloud resources, and one Kubernetes resource. The UI grouped the
six instances by their `environment` attributes and rendered Kubernetes and
ECS as child platform placements. It rendered one `api-node-boats` workload,
not one service node per platform. The three shared `bg-*` instances each
showed distinct Kubernetes and ECS placements; the remaining three instances
showed their Kubernetes placements.

The response reported no exact repository-level provisioning topology for this
anchor, and the UI disclosed that absence instead of inventing a fallback.
Every placement outside the bounded change-surface graph was labeled `Outside
bounded graph` and exposed no misleading inspect control. The graph composition
reported 26 of 26 nodes and 25 of 25 edges within the 60/120 bounds, marked
`complete within bounds`; it did not display `completeness unverified` because
both required limit metadata blocks were present and internally consistent.
The deployment-source summary displayed all 14 sources; the API
reported 14 of 14 with `truncated=false`, so no truncation warning was required.
Selecting `endpoint:0d0c68e49c7ebb12a6332c16` in the rendered graph updated the
visible Selected entity panel to that canonical endpoint ID and showed its
`DIRECT_IMPACT -> workload:api-node-boats` edge, proving click-to-visible
completion in the signed-in retained session. The browser console contained no
warnings or errors during this exact-head proof.

The same signed-in anchor also closed the interactive-read acceptance check.
The primary route-ready metric starts immediately before browser reload and
ends when the exact `Graph composition evidence` card is visible with the
`complete within bounds` label. Five same-anchor reloads measured **1461 ms,
1870 ms, 1467 ms, 1548 ms, and 1437 ms**; the observed median was **1467 ms**
and the observed p95/max was **1870 ms**. That is inside the capability
matrix's **4000 ms local-full-stack p95** contract for
`platform_impact.deployment_chain`. The card's shipped
`compositionDurationMs` readout measured **0.100 ms, 0.000 ms, 0.100 ms,
0.000 ms, and 0.100 ms**; the observed p95/max was **0.100 ms**, inside a
single 16.7 ms frame budget. These are warm retained-data measurements on this
local machine, not a hardware-independent product guarantee or a comparison to
the earlier 0.357 s API-only measurement.
