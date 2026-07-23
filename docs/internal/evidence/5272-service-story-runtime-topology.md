# #5272 service-story runtime-topology anchoring

Issue #5272 opened on the theory that request-time file and repository
enrichment was the seconds-scale story/dossier bottleneck. Retained-data
measurements disproved that theory. The dominant cost was a connected
runtime-topology query inside the existing `instance_lookup` stage.

This change reorders that one connected pattern so the textual traversal starts
from `WorkloadInstance` while preserving the selective `workload_id` predicate.
The retained NornicDB timing proves the traversal-order win, but its exposed
plan data does not prove a property-index seek. The change does not add a cache
or materialized story projection, and it does not change any predicate,
returned column, ordering key, result limit, response field, or authorization
rule.

## User-visible before and after

The comparison used baseline and candidate API/MCP binaries against the same
retained Postgres and NornicDB stores. The fixed corpus contained 896
repositories, 70 workloads, 222 workload instances, 980,689 graph nodes,
1,579,055 relationships, and 6,209,212 facts. The candidate schema bootstrap
completed in 349 seconds (5 minutes 49 seconds). Postgres migrations applied;
the graph fingerprint already matched, so all 318 graph statements were
skipped with no adoption or DDL replay. Corpus, graph, and queue counts were
unchanged.

Each row below compares the same statistic, sample surface, selectors, and
storage state before and after the query reorder.

| User action and statistic | Samples | Before | After | Result |
| --- | ---: | ---: | ---: | ---: |
| Runtime-topology read, median | 20 selectors | 1.533 s | 0.012 s | about 129x faster |
| Open a service story through HTTP, median | 20 selectors | 3.961 s | 2.560 s | 35.4% lower |
| Open a service story through MCP, successful-call median | 12 selectors | 3.929 s | 2.478 s | 36.9% lower |
| Investigate a service through HTTP, median | 20 selectors | 2.995 s | 0.922 s | 69.2% lower |
| Investigate a service through MCP, median | 20 selectors | 3.051 s | 0.943 s | 69.1% lower |
| Open a repository story through HTTP, median | 20 selectors | 0.141 s | 0.134 s | no material change |
| Open a repository story through MCP, successful-call median | 16 selectors | 0.139 s | 0.143 s | no material change |

The API comparison used a fresh HTTP connection for each call against the same
warm backend, with no application response cache. Service stories produced 20
valid pairs. Seventeen complete bodies were exact; all 20 had equal non-prose
evidence after normalizing only the derived `story` and
`answer_packet.summary` fields plus array order, the pre-existing defect tracked
in #5644. Repository stories and service investigations were exact in all 20
pairs. Baseline and candidate payload ranges were identical: 55,440-236,498
bytes for service stories, 30,136-162,459 bytes for repository stories, and
4,392-15,182 bytes for investigations.

The MCP sweep also used fresh HTTP connections and exact advertised argument
keys. Service stories had 12 paired successes and eight paired
`mcp_response_over_budget` refusals. Repository stories had 16 paired successes
and four paired refusals. Investigations succeeded in all 20 pairs. Every
baseline/candidate JSON-RPC pair was exact, with no outcome asymmetry. Identical
wire/extracted ranges were 1,578-260,340/590-123,760 bytes for service stories,
1,563-260,109/584-125,156 bytes for repository stories, and
10,032-32,730/4,638-15,428 bytes for investigations.

An authenticated candidate-console request through the `/eshu-api` proxy also
returned a valid service-story truth envelope from the candidate API.

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

The retained backend was `eshu-nornicdb-pr261:149245885258`. The final
exact-head raw-query gate ran 20 anonymous selectors with a balanced
baseline-first/candidate-first crossover. Seventeen pairs were byte-exact and
all 20 returned equal populated row and value sets after deterministic ordering.
Median query time fell from 1.532882 seconds to 0.011875 seconds, about 129
times faster.

The earlier same-head theory proof also showed the expected shape before the
full replay: a repository-first median of about 750 ms versus about 10 ms for
the WorkloadInstance-first query across 20 services, with exact populated and
zero-row value sets. The full-corpus replay supersedes that earlier timing
claim while confirming the same attribution.

## Accuracy and ordering finding

The production result contract remains:

- the same repository, workload, and workload-instance predicates;
- the same `DEFINES` and `INSTANCE_OF` relationship properties;
- ordering by repository, workload, environment, and instance identity;
- the same 51-row sentinel for a public limit of 50;
- fail-closed behavior for scoped tokens because workload instances do not yet
  carry canonical repository ownership.

The 20-pair API gate found three derived-prose differences with equal non-prose
structured evidence after array-order normalization. This is the independent
pre-existing ordering defect tracked as #5644, a direct child of epic #5267.
This performance change neither hides nor expands that defect.

## Query-plan and observability evidence

`QP-SVC-RUNTIME-TOPOLOGY` binds the exact production query and source symbol.
It requires:

- the `WorkloadInstance.workload_id` anchor;
- the `workload_instance_workload_id`, `workload_id`, and `repository_id`
  schema objects;
- the `$instance_limit` bound and deterministic ordering;
- no `AllNodesScan`, `CartesianProduct`, or `UnboundedExpand`.

The production callsite is promoted from a typed non-hot disposition to that
registered hot query. Any source or exact-query drift now fails the query-plan
gate.

Observability Evidence: no new telemetry is required. The existing
`service_query.stage` record for `instance_lookup` exposed the bottleneck and
measured the result. Existing `neo4j.query` spans retain statement timing, and
the HTTP request metric retains endpoint latency and errors. The change adds no
route, response field, metric series, label, log field, graph write, queue,
worker, or runtime knob.

## Exact source binding

The final replay used clean Git-derived binaries from base `7d098fb972` and
candidate `648f60cbb1`. The implementation and query-plan patch retained stable
patch ID `ed3a3584991c24f3579f98685bfc308336e2fa79` after the branch was rebased
over `bfa344d9e0`, whose SQL parser, fixture, and reducer changes do not touch
the measured query, API/MCP readers, schema, or retained corpus. Exact candidate
API and MCP images were retained with the full-corpus Postgres and NornicDB
stores as five healthy services; the console remained on the previously
validated image because this branch changes no console path.

## Focused verification

```text
cd go
go test ./internal/query -run '^(TestFetchWorkloadRuntimeTopologyStartsFromWorkloadInstanceTraversal|TestFetchWorkloadDeploymentTopologyReturnsStructuredEmptyLimits|TestFetchWorkloadRuntimeTopologyReturnsObservedIdentityEdges|TestLegacyQueryplanManifestBindsProductionQueries)$' -count=1
go test ./internal/queryplan -run '^TestHotCypherManifestCoversEveryProductionQueryCall$' -count=1
```

No service, repository, host, account, credential, or retained identifier is
recorded in this evidence note.
