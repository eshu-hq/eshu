# Code-Topic Probe Traces

The unscoped 16-term `investigate_code_topic` read can split its PostgreSQL candidate probe across four sessions that share one snapshot. Its parent `postgres.query` span reports total probe and assembly time. Each executed partition adds a sampled `query.code_topic_partition` child span so an operator can see which concurrent probe sets the wait time.

| Attribute | Meaning |
| --- | --- |
| `code_topic.partition` | Closed ordinal `0` through `3` for the executed partition. It is not a repository or reader identity. |
| `code_topic.partition_rows` | Successfully scanned probe rows in that partition, bounded by the existing per-term candidate caps. |
| `code_topic.partition_outcome` | Closed result category: `ok`, `error`, `canceled`, or `deadline`. |

The child duration starts before the partition's `QueryContext` and ends after row iteration, scan, error check, and cursor close. Compare all four child durations with the parent's reservation wait, aggregate probe duration, and assembly duration. The slowest child—not the sum of four concurrent children—is the relevant contribution to aggregate probe wall time. No term, SQL text, parameter, repository ID, host, DSN, credential, or raw error is added to the child attributes.

These spans are diagnostic. Sampling or a full nonblocking exporter queue can omit a child; absence is not proof that a partition did not run. A request that takes the single-statement fallback has no partition children. Follow the parent execution-mode and fallback attributes before interpreting a trace. Span timing does not establish a query result, an endpoint latency improvement, or the `<1 s` budget; check the complete API/MCP response and timed request separately.

The initial #7033 overhead shim measured about 8–9 microseconds of sampled in-process work per four-partition request and about 6 KiB / 40 allocations more than its control. That is a local instrumentation-cost screen with a no-op batch exporter, not deployed latency or 100,000-repository proof. The internal record at `docs/internal/evidence/7033-code-topic-partition-tracing.md` states the exact proof boundary.
