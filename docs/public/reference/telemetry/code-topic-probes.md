# Code-Topic Probe Traces

The unscoped 16-term `investigate_code_topic` read can split its PostgreSQL candidate probe across four sessions that share one snapshot. Its parent `postgres.query` span reports total probe and assembly time. Each executed partition starts a `query.code_topic_partition` child span; only retained sampled traces show it.

| Attribute | Meaning |
| --- | --- |
| `code_topic.attempt` | Closed retry ordinal `0` or `1`; a reader-member loss can restart the four-partition read. |
| `code_topic.partition` | Closed ordinal `0` through `3` for the executed partition. It is not a repository or reader identity. |
| `code_topic.partition_rows` | Successfully scanned probe rows in that partition, bounded by the existing per-term candidate caps. |
| `code_topic.partition_outcome` | Closed result category: `ok`, `error`, `canceled`, or `deadline`. |
| `code_topic.scoped_one_term` | Boolean on the parent `postgres.query` span. `true` when the single statement ran the repository-scoped one-term shape (one repository, one term, no language filter), which hides the term from the entity probe with a `MATERIALIZED` terms CTE (#7246). `false` for every other shape. It carries no term, repository, or SQL text. |

The child duration starts before the partition's `QueryContext` and ends after row iteration, scan, error check, and cursor close. Compare the four child durations within each attempt. The parent's reservation fields reflect its last attempt, while probe and assembly durations appear only for a completed attempt; parent wall time can also include a failed first attempt. A member-loss retry can produce up to eight children under one parent. The slowest child in an attempt—not the sum of four concurrent children—is the relevant probe wait. A cursor-close failure marks the child `error` without changing the existing API response rule. No term, SQL text, parameter, repository ID, host, DSN, credential, or raw error is added to the child attributes.

These spans are diagnostic. Sampling or a full nonblocking exporter queue can omit a child; absence is not proof that a partition did not run. A request that takes the single-statement fallback has no partition children. Follow the parent execution-mode and fallback attributes before interpreting a trace. Span timing does not establish a query result, an endpoint latency improvement, or the `<1 s` budget; check the complete API/MCP response and timed request separately.

The initial #7033 overhead shim measured about 8–9 microseconds of sampled in-process work per four-partition request and about 6 KiB / 40 allocations more than its control. That is a local instrumentation-cost screen with a no-op batch exporter, not deployed latency or 100,000-repository proof. The internal record at `docs/internal/evidence/7033-code-topic-partition-tracing.md` states the exact proof boundary.

A matched local benchmark of the production `Investigate` function on a fixed 8,000-row fake corpus measured median 7.843 ms before and 7.915 ms after (+0.072 ms) across six interleaved observations per version. The per-run ranges overlap. This clears the 1 ms/request tracing-cost screen, not the deployed `<1 s` endpoint budget.
