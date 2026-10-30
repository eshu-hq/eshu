# Streaming And Memory

Large repositories stress fact streaming, Postgres commit throughput, graph
projection, and Go heap behavior. Use this page when ingestion is slow or a
runtime is near its memory limit.

## Why These Metrics Exist

Without streaming and memory telemetry, operators cannot tell the difference
between:

- a generation that is slow because it has hundreds of thousands of facts and a
  generation that is stuck
- memory pressure from one outlier repository and systemic garbage-collector
  misconfiguration
- a `GOMEMLIMIT` value that is too low and thrashing garbage collection versus
  one that is too high and risks out-of-memory termination

## Fact Commits

`eshu_dp_facts_committed_total` counts facts persisted to Postgres, recorded
after the commit returns without error (`go/internal/collector/service.go:438`).

Use it to answer:

- Is fact persistence still moving?
- Did commit throughput drop while parsing stayed steady?
- Did a new repository add unusually high fact volume?

This section previously named `eshu_dp_fact_batches_committed_total`. That
counter was registered but never emitted, so the queries here returned nothing.
It is removed (#5548); counting batches rather than facts would need the batch
count plumbed back through the committer interface, which does not report one.

Compare it with `eshu_dp_repo_snapshot_duration_seconds` and
`eshu_dp_postgres_query_duration_seconds` to decide whether the bottleneck is
the producer, the commit path, or Postgres itself.

## Generation Fact Count

`eshu_dp_generation_fact_count` shows fact-count distribution per scope
generation.

Use it to detect:

- outlier repositories in high fact-count buckets
- parser changes that emit duplicate or unexpectedly low fact volume
- fleet shape changes that affect worker and memory sizing

## Go Memory Limit

`eshu_dp_gomemlimit_bytes` exposes the Go runtime's effective memory limit,
read at each scrape. It covers a limit from the `GOMEMLIMIT` environment
variable and one derived from the container cgroup. `telemetry.NewProviders`
registers it, so every runtime exports it. Before #9567 an env-set limit read
`0`, and only five runtimes registered the gauge (not the reducer).

Every runtime's `/metrics` also carries the Go runtime and process series:
`go_memstats_heap_alloc_bytes`, `go_memstats_heap_inuse_bytes`,
`go_memstats_heap_released_bytes`, `go_memstats_sys_bytes`,
`go_gc_duration_seconds`, and `process_resident_memory_bytes` on Linux. Compare
heap alloc against `eshu_dp_gomemlimit_bytes` and the container limit to tell a
heap burst from non-heap growth.

Use it with container RSS from cAdvisor, Docker, or your runtime platform:

- If the gauge is `0`, the runtime did not detect or receive a memory limit.
- If RSS approaches `GOMEMLIMIT`, reduce concurrency, increase memory, or tune
  the ratio.
- If RSS is much higher than `GOMEMLIMIT`, investigate non-heap memory such as
  stacks, mmap, or native allocations.

## Repository Catalog Loads

Several reducer domains read the cross-scope repository catalog
(`ListActiveRepositoryFacts`) once per intent. Two histograms record each load,
labeled by the closed `domain` set (the five consumer domains, or `unknown`):

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_repository_catalog_load_rows` | histogram | Rows one catalog load returned. |
| `eshu_dp_repository_catalog_load_payload_bytes` | histogram | Raw payload bytes one catalog load scanned before JSON decode. |

Multiply the per-load bytes by the load rate and the worker count to estimate
how much of the reducer's allocation rate the catalog explains. Since #9567 the
catalog payload leaves out `imports_map`, `git_refs`,
`delta_relative_paths`, and `delta_deleted_relative_paths`, so a load should
scan a few hundred bytes per repository. A per-load size near the full
repository payload (tens of KB per row) means the projection regressed. Only
an object payload is projected; a scalar or JSON-null payload passes through
unchanged, as it did before #9567.

Performance Evidence: in the QA environment the reducer was OOM-killed three
times while `container_image_identity` and `supply_chain_impact` reloaded the
catalog per intent (799 facts, about 94 KB mean payload, about 71 MiB of JSON;
#9567). A local shim driving the real handlers with 8 workers measured peak
HeapAlloc of 1,426 MiB before and 142 MiB after the projection (median of 3,
mixed wave), and 227.5 vs 75.8 MiB allocated per intent, with byte-identical
decisions and findings. On a QA-shaped native PostgreSQL 18 seed (799 facts,
95.5 KB mean payload) `EXPLAIN (ANALYZE, BUFFERS, SERIALIZE)` of the shipped
statement against the unprojected read, 12 interleaved runs, gave median
execution of 122.9 to 108.5 ms for page 1 and 75.3 to 45.7 ms for page 2, with
46.8 MB to 0.3 MB serialized per page. The plan keeps the same scans, joins,
and index, plus a per-row Result node for the lateral read. The object-only
guard reads the payload once through a lateral identity path (`#> '{}'`);
guarding with `fact.payload` in both the type check and the subtraction
detoasted it twice and made page 1 69% slower than the unprojected read in its
own round. The projection does not explain the 4 to 11 GB live heap QA showed,
so it is not proven to meet the 50% peak target on its own.

Observability Evidence: the two histograms above, recorded at the store for
every load.

## Decision Tree

```text
Is it OOMing?
  YES -> Check eshu_dp_gomemlimit_bytes.
         0? Set GOMEMLIMIT or fix cgroup detection.
         Near container limit? Increase memory or reduce concurrency.
         RSS much higher than GOMEMLIMIT? Investigate non-heap memory.

Is ingestion slow?
  YES -> Check eshu_dp_facts_committed_total rate.
         Dropping? Check Postgres latency and contention.
         Steady but slow? Check eshu_dp_generation_fact_count for outliers.
         Zero? Check collector and parser spans for a blocked producer.

Are facts missing after ingestion?
  YES -> Compare eshu_dp_generation_fact_count with expected source size.
         Lower than expected? Check parser skip behavior and file counts.
         Higher than expected? Check duplicate emission and commit dedupe.
```

Do not raise timeouts or worker counts until you classify whether the evidence
points to source size, parser cost, Postgres, graph writes, memory pressure,
stale images, missing schema, or a real timeout-budget problem.
