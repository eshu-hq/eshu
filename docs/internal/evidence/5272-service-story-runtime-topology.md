# #5272 service-story runtime-topology anchoring

Issue #5272 opened on the theory that request-time file and repository
enrichment was the seconds-scale story/dossier bottleneck. Retained-data
measurements disproved that theory. The dominant cost was a connected
runtime-topology query inside the existing `instance_lookup` stage.

This change reorders that one connected pattern so the textual traversal starts
from the selective `WorkloadInstance.workload_id` predicate and its existing
index. It does not add a cache or materialized story projection, and it does not
change any predicate, returned column, ordering key, result limit, response
field, or authorization rule.

## User-visible before and after

The comparison used baseline and candidate API/MCP binaries against the same
retained Postgres and NornicDB stores. The fixed corpus contained 896
repositories, 70 workloads, 222 workload instances, 980,689 graph nodes,
1,579,055 relationships, and 6,209,212 facts. The candidate schema bootstrap
completed all 314 registered objects in 1,183 seconds (19 minutes 43 seconds),
including the candidate marker and every required query-plan object.

Each row below compares the same statistic, sample surface, selectors, and
storage state before and after the query reorder.

| User action and statistic | Samples | Before | After | Result |
| --- | ---: | ---: | ---: | ---: |
| First uncached runtime-topology read, median | 20 selectors | 1.580 s | 0.012 s | about 130x faster |
| Open a service story through HTTP, mean | 20 selectors | 3.907 s | 2.404 s | 38.5% lower |
| Open a service story through MCP, successful-call mean | 13 selectors | 4.388 s | 2.912 s | 33.6% lower |
| Investigate a service through HTTP, mean | 20 selectors | 2.976 s | 0.905 s | 69.6% lower |
| Investigate a service through MCP, median | 5 selectors | 3.605 s | 2.083 s | 42.2% lower |
| Open a repository story through HTTP, mean | 20 selectors | 0.324 s | 0.276 s | no regression |
| Open a repository story through MCP, median | 5 selectors | 0.174 s | 0.180 s | no material change |

The API service-story comparison produced 20 valid paired envelopes. The
structured response was equal in all 20 pairs: 17 pairs were byte-for-byte
exact after canonicalization, while three differed only in derived prose
affected by the pre-existing ordering defect tracked in #5644. Repository
stories and service investigations were exact in all 20 API pairs.

The MCP service-story sweep had the same outcome for every selector: 13 paired
successes and seven paired `mcp_response_over_budget` refusals, with no
baseline/candidate asymmetry. Every paired response was exact. The repository
and investigation supplements were exact in all five pairs. Their identical
payload ranges were 39,399-114,220 bytes for repository stories and
5,795-7,750 bytes for investigations.

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

The retained backend was `eshu-nornicdb-pr261:149245885258`. The representative
raw-query gate ran 20 anonymous selectors three times each. All 60 populated
baseline/candidate pairs returned equal canonical value sets. The first-use
median fell from 1.579731 seconds to 0.012149 seconds, about 130 times faster.
Trials two and three were sub-millisecond on both binaries after cache warmup,
so the pooled timing is not used as the first-read performance claim.

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

The 60-pair raw gate found nine row-order permutations with equal value sets.
The 20-pair API gate found three derived-prose differences with equal structured
truth. Both are manifestations of the independent pre-existing ordering defect
tracked as #5644, a direct child of epic #5267. This performance change neither
hides nor expands that defect.

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

## Rebase carry-forward

The representative replay exercised reviewed candidate commit `419b610f22` on
base `c25b80acae`. The branch was then rebased over #5669 and #5673. Those
changes affect documentation/fixtures and Terraform-state projection; they do
not touch the runtime-topology query, its query-plan binding, the service-story
API/MCP readers, or the fixed retained corpus. The query patch remained
identical, and focused verification was rerun after the rebase.

## Focused verification

```text
cd go
go test ./internal/query -run '^(TestFetchWorkloadRuntimeTopologyStartsFromIndexedWorkloadInstance|TestFetchWorkloadDeploymentTopologyReturnsStructuredEmptyLimits|TestFetchWorkloadRuntimeTopologyReturnsObservedIdentityEdges|TestLegacyQueryplanManifestBindsProductionQueries)$' -count=1
go test ./internal/queryplan -run '^TestHotCypherManifestCoversEveryProductionQueryCall$' -count=1
```

No service, repository, host, account, credential, or retained identifier is
recorded in this evidence note.
