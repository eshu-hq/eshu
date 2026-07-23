# #5644 deterministic service-story / workload-context ordering

Issue #5644 removes result-order non-determinism from the service-story and
workload-context read path. Go map iteration is randomized, so collections that
were collected into maps and then truncated could emit different survivor sets
and different derived prose across identical backend responses. This change
sorts each in-scope collection by a total-order identity key **before**
truncation and before any prose derivation, so the same backend rows always
produce a byte-identical payload.

In-scope collections and their total-order sort keys:

- Runtime instances — `(environment, instance_id)`; `instance_id` is unique.
- Attached platforms — `(instance_id, platform_name, platform_id, platform_kind)`;
  the four-tuple is the Cypher aggregation key.
- Runtime/platform topology edges — retained to only the surviving instances,
  then ordered by relationship type, source, target, evidence source, reason,
  and confidence.
- Provisioned platforms — `(platform_name, platform_id, source_id, target_id)`;
  the four-tuple is the dedup key, so the order key is a total order.
- Consumer repositories — `(score DESC, display_name, repo_id)`; `repo_id` is
  unique.

Out of scope and routed to follow-up #5720: the `dependents` and
`provisioning_source_chains` arrays. They are not part of #5644's acceptance
criteria and are unchanged here.

## Backend, input shape, and terminal counts

The read path and queries are unchanged from #5272. The
`hot-cypher.yaml` / `query-source-coverage.yaml` manifests record a
`source_sha256` update for the three touched functions
(`fetchWorkloadRuntimeTopology`, `fetchWorkloadPlatformResult`,
`fetchProvisionedPlatformResult`) with **`cypher_sha256` unchanged**, proving no
query text, predicate, returned column, or backend ordering key changed. Backend
is NornicDB (Neo4j-compatible) as pinned in the repo. Each runtime-instance,
topology-edge, provisioned-platform, and consumer-repository collection is
bounded by `contextStoryItemLimit = 50` with a `queryLimit = 51` sentinel; the
attached-platform collection uses
`workloadPlatformEdgeLimit = contextStoryItemLimit * contextStoryItemLimit = 2500`
with a `queryLimit = 2501` sentinel. All sorts therefore operate on at most
2,501 rows per collection, not 51.

### Backend selection contract (bounded `ORDER BY ... LIMIT`)

Each bounded read declares `ORDER BY <total-order-tuple> LIMIT $sentinel` and
then re-sorts and truncates the returned rows in Go. That in-memory truncation
is only correct if the backend's `LIMIT` selects a **deterministic** candidate
subset that already contains the true lexicographic top-N — an `ORDER BY` that
merely ordered *delivery* of an arbitrary subset would let the survivor set vary
across identical calls once the distinct candidate count exceeded the sentinel.
The in-memory tests cannot observe this, because they shuffle a fixed,
already-selected row set.

`service_story_determinism_nornicdb_live_test.go` closes that gap against a live
NornicDB. Both plan shapes are driven through their **real production
functions at their real sentinels**, and in both cases the expected top-N is
computed in the test from *every seeded row*, not from the subset the backend
chose to return:

- Plain scan — `fetchWorkloadRuntimeTopology`, sentinel 51: seeds 120 distinct
  `WorkloadInstance` rows for one workload and asserts over 25 repeated calls
  that the surviving 50 are stable and exactly equal to the independent
  lexicographic top-50.
- Aggregating (`WITH ... collect() ... ORDER BY ... LIMIT`) —
  `fetchWorkloadPlatformResult`, sentinel 2501: seeds
  `contextStoryItemLimit` instances × `contextStoryItemLimit + 1` platforms =
  2,550 distinct `RUNS_ON` edges and asserts over 5 repeated calls that the
  surviving 2,500 are stable and equal to the independent lexicographic
  top-2,500. 2,550 over a 2,501 sentinel is the true production ceiling, since
  the query restricts `i.id IN $instance_ids` to the already-truncated topology.

Rows are inserted in **reverse** lexicographic order in both cases, so a backend
returning scan order instead of sorted order would yield a demonstrably wrong
survivor set rather than an accidentally-passing one.

Result: on the pinned NornicDB (v1.1.11), bounded `ORDER BY ... LIMIT` selects
the lexicographically-correct top-N deterministically for both plan shapes at
production cardinality. The Go-level sort remains as defense in depth against
delivery-order variance within the returned subset.

Run:

```bash
ESHU_SERVICE_STORY_DETERMINISM_NORNICDB_LIVE=1 \
ESHU_NEO4J_URI=bolt://localhost:37687 \
go test ./internal/query -run TestServiceStoryTruncationSelectionIsDeterministicLiveNornicDB -count=1 -v
```

## Performance and regression

No-Regression Evidence: the only added work is an in-memory `sort.Slice` over each already-bounded
collection: O(N log N) with N ≤ 51 for the runtime-instance, topology-edge,
provisioned-platform, and consumer-repository collections and N ≤ 2,501 for the
attached-platform collection, executed once per request on data already resident
in memory. No new graph query, round trip, allocation tier, or Postgres access is
introduced (`cypher_sha256` unchanged confirms the query shape is identical). The
sibling #5272 note measured this exact read path
against the retained corpus (896 repositories, 70 workloads, 222 workload
instances, 980,689 nodes, 1,579,055 relationships, 6,209,212 facts) and reported
a service-story HTTP median of 2.560 s.

The worst-case sort is measured rather than assumed.
`BenchmarkSortWorkloadPlatformRowsWorstCase` sorts a reverse-ordered 2,501-row
attached-platform slice shaped to the **production invariant**: at most
`contextStoryItemLimit` (50) distinct `instance_id`s, because
`fetchWorkloadPlatformResult` restricts `i.id IN $instance_ids` to the
already-truncated topology. Each instance id therefore repeats ~50 times,
`platform_name` repeats within an instance, and a share of rows carry an empty
`platform_id`, so the comparator is driven through its `platform_name`,
`platform_id`, and `platform_kind` tiebreakers instead of resolving on the first
key. `TestWorstCaseWorkloadPlatformRowsExerciseEveryComparatorKey` asserts that
distribution, so the benchmark cannot silently regress to an all-distinct
first-key input that would measure the cheapest path while claiming otherwise.

```
cpu: Apple M5 Max
BenchmarkSortWorkloadPlatformRowsWorstCase-18   200   765251 ns/op   56 B/op   2 allocs/op
BenchmarkSortWorkloadPlatformRowsWorstCase-18   200   777752 ns/op   56 B/op   2 allocs/op
BenchmarkSortWorkloadPlatformRowsWorstCase-18   200   794508 ns/op   83 B/op   2 allocs/op
BenchmarkSortWorkloadPlatformRowsWorstCase-18   200   760831 ns/op   56 B/op   2 allocs/op
BenchmarkSortWorkloadPlatformRowsWorstCase-18   200   763498 ns/op   56 B/op   2 allocs/op
```

Run:

```bash
go test ./internal/query -run '^$' -bench BenchmarkSortWorkloadPlatformRowsWorstCase -benchtime 200x -count=5
```

At ~0.78 ms and 2 allocations, the worst-case per-request sort is ~0.03% of the
2.560 s service-story baseline, so the median is unchanged within error. The
smaller collections' ≤51-element sorts stay below measurement noise.

These figures are from an otherwise-idle machine. An earlier revision of this
note cited ~1.1–1.5 ms for the same fixture, measured while several test and
build jobs were running concurrently; that number reflected machine load, not
sort cost. Re-measure on a quiet machine before treating any change here as a
regression. Truncation now derives its `truncated` flag from the distinct collected
count versus the limit rather than a mid-walk artifact, so the flag is exact.

Benchmark Evidence: `service_story_determinism_test.go` and
`workload_provisioned_platform_contract_test.go` exercise the production
functions with shuffled backend row orders and SHA256-hash the full payloads,
asserting byte-identical output and an order-independent survivor set above the
limit. `service_story_determinism_nornicdb_live_test.go` extends that to the
live backend, proving the bounded `ORDER BY ... LIMIT` candidate selection is
itself deterministic and lexicographically correct above the sentinel (see the
"Backend selection contract" section above). Together these are the tracked,
repeatable proofs that the survivor set is stable and correct for any input
permutation.

## Observability

No-Observability-Change: no metric, span, log field, or status code changes. The service-story and
workload-context reads keep their existing telemetry; only the in-memory
ordering of already-returned rows changes, and no operator-facing signal is
added or renamed.
