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

## Fact Batch Commits

`eshu_dp_fact_batches_committed_total` counts streaming multi-row fact commits
to Postgres.

Use it to answer:

- Is fact persistence still moving?
- Did commit throughput drop while parsing stayed steady?
- Did a new repository add unusually high batch volume?

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

`eshu_dp_gomemlimit_bytes` exposes the `GOMEMLIMIT` value configured at startup.

Use it with container RSS from cAdvisor, Docker, or your runtime platform:

- If the gauge is `0`, the runtime did not detect or receive a memory limit.
- If RSS approaches `GOMEMLIMIT`, reduce concurrency, increase memory, or tune
  the ratio.
- If RSS is much higher than `GOMEMLIMIT`, investigate non-heap memory such as
  stacks, mmap, or native allocations.

## Decision Tree

```text
Is it OOMing?
  YES -> Check eshu_dp_gomemlimit_bytes.
         0? Set GOMEMLIMIT or fix cgroup detection.
         Near container limit? Increase memory or reduce concurrency.
         RSS much higher than GOMEMLIMIT? Investigate non-heap memory.

Is ingestion slow?
  YES -> Check eshu_dp_fact_batches_committed_total rate.
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
