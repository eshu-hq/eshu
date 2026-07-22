# #5272 service-story runtime-topology anchoring

Issue #5272 opened on the theory that request-time file and repository
enrichment was the seconds-scale story/dossier bottleneck. Current retained
data disproved that theory. The dominant cost is a connected runtime-topology
query inside the existing `instance_lookup` stage.

This change reorders that one connected pattern so the textual traversal starts
from the selective `WorkloadInstance.workload_id` predicate and its existing
index. It does not add a cache or materialized story projection, and it does not
change any predicate, returned column, ordering key, result limit, response
field, or authorization rule.

## User-visible before and after

The endpoint comparison used two current-main API binaries against the same
retained Postgres and NornicDB data. The baseline binary was commit
`b7c3502b71`; the candidate used the same commit plus the one traversal-order
change in this pull request. Calls alternated which binary ran first for each of
20 anonymous exact service IDs.

| User action | Before | After |
| --- | ---: | ---: |
| Open a service story, 20-service median | 2.950 s | 1.637 s |
| Fastest service story | 0.819 s | 0.118 s |
| Slowest service story | 4.573 s | 3.291 s |
| Successful responses | 20 of 20 | 20 of 20 |
| Payload range | 29,768-239,507 bytes | 29,768-239,507 bytes |

The median fell by 1.313 seconds, or 44.5%. Payload size did not change.

`get_repo_story` does not execute the changed runtime-topology query. Two
first-use crossover samples stayed in the same small range: 0.075-0.104 seconds
for the baseline and 0.069-0.099 seconds for the candidate, with identical
51,694-byte and 105,000-byte payloads. This is an explicit no-regression result,
not a claimed repository-story speedup.

`investigate_service` does execute the changed workload-context path. Two
first-use crossover samples were 1.237 and 1.665 seconds on the baseline and
1.114 and 0.067 seconds on the candidate. Both pairs returned HTTP 200 with
identical payload sizes and canonical payload hashes.

## Attribution and rejected materialization theory

The initial retained baseline sampled 20 deterministic exact service IDs from
76 available services:

- all 20 returned HTTP 200;
- endpoint latency was 1.067-3.413 seconds with a 1.809-second median;
- `instance_lookup` was the dominant stage at a 1.155-second median;
- uncorrelated cloud-resource candidates were 388 ms median;
- documentation overview was 95 ms median;
- service-evidence content was 18.6 ms median;
- provisioning-source chains were 1.8 ms median.

A disposable Postgres shim then tested the original file-content theory on
5,000 files and 200 service-evidence candidates. Batching preserved exact
output and reduced 201 round trips to two, but only moved the stage from
81.794 ms to 58.852 ms. Recovering 22.942 ms cannot explain or fix a
1.809-second endpoint median, so projection-time materialization and a
request-time cache were rejected for this leaf.

The same-head endpoint comparison kept the other major stages effectively
flat. The complete `instance_lookup` stage, which includes the changed query
and the unchanged platform attachment read, moved from a 2,500.1 ms median to
1,136.1 ms. Documentation overview was 160.4 ms versus 154.7 ms, uncorrelated
cloud candidates were 397.8 ms versus 386.4 ms, and service-evidence content
was 25.7 ms versus 30.7 ms.

## Query theory proof

The baseline production query began with:

```cypher
MATCH (repo:Repository)-[defines:DEFINES]->(w:Workload)<-[instanceOf:INSTANCE_OF]-(i:WorkloadInstance)
WHERE i.workload_id = $workload_id
```

The candidate preserves the same connected path in the opposite textual order:

```cypher
MATCH (i:WorkloadInstance)-[instanceOf:INSTANCE_OF]->(w:Workload)<-[defines:DEFINES]-(repo:Repository)
WHERE i.workload_id = $workload_id
```

The retained backend was `eshu-nornicdb-pr261:149245885258`. Across the same
20 anonymous services, alternating which query ran first:

| Exact query shape | Range | Median | Canonical row sets |
| --- | ---: | ---: | ---: |
| Repository-first baseline | 604-1,060 ms | about 750 ms | 20 of 20 |
| WorkloadInstance-first candidate | 4-13 ms | about 10 ms | 20 of 20 |

The candidate was about 75 times faster at the median. The exact row sets
matched for populated three-, five-, and six-row results and valid zero-row
results. The output-preserving proof covers every returned scalar and
relationship-property map, not only row counts.

## Accuracy and ordering finding

The production result contract remains:

- the same repository, workload, and workload-instance predicates;
- the same `DEFINES` and `INSTANCE_OF` relationship properties;
- ordering by repository, workload, environment, and instance identity;
- the same 51-row sentinel for a public limit of 50;
- fail-closed behavior for scoped tokens because workload instances do not yet
  carry canonical repository ownership.

A representative rich service had repository, deployment, four or more
environments, API-surface, dependency, and populated runtime evidence. Three
same-head baseline/candidate pairs returned the same 91,023-byte canonical
payload.

The wider 20-service audit also exposed an independent pre-existing ordering
defect: repeated calls can permute runtime/platform and consumer-repository
arrays, changing derived prose while preserving the same values. Sorting those
collections by stable identity made the value sets identical. That defect is
tracked separately as #5644, a direct child of epic #5267; it is not hidden in
this performance change.

## Query-plan and observability evidence

`QP-SVC-RUNTIME-TOPOLOGY` now binds the exact production query and source
symbol. It requires:

- the `WorkloadInstance.workload_id` anchor;
- the `workload_instance_workload_id`, `workload_id`, and `repository_id`
  schema objects;
- the `$instance_limit` bound and deterministic ordering;
- no `AllNodesScan`, `CartesianProduct`, or `UnboundedExpand`.

The production callsite is promoted from a typed non-hot disposition to that
registered hot query. Any source or exact-query drift now fails the query-plan
gate.

Observability Evidence: no new telemetry is required. The existing
`service_query.stage` record for `instance_lookup` exposed the bottleneck
and measured the result. Existing `neo4j.query` spans retain statement timing,
and the HTTP request metric retains endpoint latency and errors. The change adds
no route, response field, metric series, label, log field, graph write, queue,
worker, or runtime knob.

## Focused verification

```text
cd go
go test ./internal/query -run '^(TestFetchWorkloadRuntimeTopologyStartsFromIndexedWorkloadInstance|TestFetchWorkloadDeploymentTopologyReturnsStructuredEmptyLimits|TestFetchWorkloadRuntimeTopologyReturnsObservedIdentityEdges|TestLegacyQueryplanManifestBindsProductionQueries)$' -count=1
go test ./internal/queryplan -run '^TestHotCypherManifestCoversEveryProductionQueryCall$' -count=1
```

No service, repository, host, account, credential, or retained identifier is
recorded in this evidence note.
