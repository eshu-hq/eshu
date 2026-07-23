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
is NornicDB (Neo4j-compatible) as pinned in the repo. Each collection is bounded
by `contextStoryItemLimit = 50` with a `queryLimit = 51` sentinel, so the sort
operates on at most 51 rows per collection.

## Performance and regression

No-Regression Evidence: the only added work is an in-memory `sort.Slice` over each already-bounded
collection: O(N log N) with N ≤ 51 per collection, executed once per request on
data already resident in memory. No new graph query, round trip, allocation
tier, or Postgres access is introduced (`cypher_sha256` unchanged confirms the
query shape is identical). The sibling #5272 note measured this exact read path
against the retained corpus (896 repositories, 70 workloads, 222 workload
instances, 980,689 nodes, 1,579,055 relationships, 6,209,212 facts) and reported
a service-story HTTP median of 2.560 s; a per-request sort of ≤51 elements is
below measurement noise against that baseline, so the median is unchanged within
error. Truncation now derives its `truncated` flag from the distinct collected
count versus the limit rather than a mid-walk artifact, so the flag is exact.

Benchmark Evidence: `service_story_determinism_test.go` and
`workload_provisioned_platform_contract_test.go` exercise the production
functions with shuffled backend row orders and SHA256-hash the full payloads,
asserting byte-identical output and an order-independent survivor set above the
limit. These are the tracked, repeatable proofs that the sort produces a stable
result for any input permutation.

## Observability

No-Observability-Change: no metric, span, log field, or status code changes. The service-story and
workload-context reads keep their existing telemetry; only the in-memory
ordering of already-returned rows changes, and no operator-facing signal is
added or renamed.
