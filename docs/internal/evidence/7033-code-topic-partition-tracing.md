# #7033 partition-level code-topic tracing

## Claim boundary

This change is diagnostic-only. It adds a bounded child span to each executed partition of the existing four-session PostgreSQL code-topic read. It does not change query predicates, candidate caps, ordering, snapshot ownership, API/MCP responses, or the PostgreSQL reader route. It does not claim that ops-qa meets the strict `<1 s` endpoint budget or that the read scales to 100,000 repositories. #7033 remains open.

## Why this signal is needed

The merged four-session read records the maximum wall time across concurrent probes, but not each partition's complete duration. On ops-qa's earlier image, 12 existing `postgres.query` traces recorded aggregate probe durations of 740–864 ms and assembly durations of 72–87 ms. A later, aborted diagnostic block on image source `5d77d695c0eda7f540a6a157416e883f3318e7be` returned three HTTP 200 responses; three temporally matching traces had aggregate probe durations of 881, 786, and 809 ms and assembly durations of 89, 89, and 82 ms. Those traces do not embed a request ID or image digest, so their precise request/image mapping is not established. The block had a 4m16s admission-monitoring gap and stopped after one timed call when writer CPU reached 12.001 of 12 cores; no endpoint median or before/after improvement can be inferred. A private sanitized artifact retains the samples and trace extracts under run ID `eshu7033-20261005-NAsTAI`.

The ops-qa reader's `pg_stat_statements` extension and view are absent. Query-wrapper timing ends when `QueryContext` returns, before all rows are consumed. Neither existing signal identifies the slowest of the four partitions. The read-only catalog proof is retained with the same private run ID.

## Theory and overhead screen

The predeclared overhead objective was less than 1 ms per four-partition request, far below the observed 48.546 ms gap between the earlier deployed 1.048546 s median and the strict budget. A scratch shim invoked the existing `codetopicparallel.Investigate` path through four fake snapshot readers, each returning 2,000 rows, and confirmed that child spans can be exported with the pinned OpenTelemetry SDK's batch processor. That integration-shaped run was noisy and is not an overhead acceptance result.

A separate isolated Go benchmark kept four goroutines and 2,000 loop steps per partition identical across variants, adding only four child span starts, bounded attributes, row counts, outcomes, and ends. It used the pinned SDK and production-default nonblocking batch processor with a no-op exporter. Three process runs reversed variant order; each variant ran twice per process for at least 300 ms per run. The host load average at launch was 0.43 on 12 logical CPUs, with no peer Go gate observed. `ForceFlush` ran outside the timed operation.

| Metric per simulated request | No child spans | Four sampled child spans | Four unsampled child spans |
| --- | ---: | ---: | ---: |
| Six-run `ns/op` range | 4,771–4,970 | 13,021–13,718 | 8,831–9,047 |
| `B/op` range | 368–369 | 6,354–6,357 | 2,128–2,129 |
| `allocs/op` | 9 | 49 | 37 |

Benchmark Evidence: sampled in-process average overhead was approximately 8–9 µs and 6 KiB / 40 allocations per simulated four-partition request; unsampled overhead was approximately 4 µs. The three benchmark processes exported exactly as many sampled spans as they started: 290,128, 293,696, and 293,372 respectively, with no drops observed under the no-op exporter. These are average microcosts, not p95/p99 endpoint results or proof of behavior with a slow OTLP transport. A separate stalled-exporter test must prove business requests do not block when the span queue fills.

The scratch command was `go test ./.scratch_7033_trace/overhead_test.go -run '^$' -bench '^BenchmarkTraceCost$' -benchtime=300ms -count=2 -benchmem`, with `ESHU7033_BENCH_ORDER` set to `baseline,sampled,unsampled`, `unsampled,sampled,baseline`, then `sampled,baseline,unsampled`. The untracked scratch harness is not part of this PR; its raw output and code are retained in the private local validation artifact.

## Post-change production-path no-regression screen

A second scratch benchmark invoked the actual `codetopicparallel.Investigate` function on exact base `d6cec878030e` and candidate `b5c9863e400b` source, with the same 16 terms, candidate cap 250, four fake snapshot readers, 2,000 rows each, zero-row assembly response, Go 1.26.6, and sampled OpenTelemetry batch exporter. The 113-line harness differed only in the candidate's required attempt argument (`0`). No database or storage state changed. Both binaries were built before timing, pinned to CPUs 4–7 with `GOMAXPROCS=4`, and run in B/C/C/B, C/B/B/C, B/C/C/B order with six one-second Go benchmark observations per version. No peer Go process ran during measurement. Full commands, harness hashes, and all twelve `ns/op`, `B/op`, and allocation results are retained under the same private run ID in `trace-overhead-scratch/production-benchmark-results.md`.

| Actual function, fixed fake corpus | Base | Candidate | Difference |
| --- | ---: | ---: | ---: |
| Median `ns/op` | 7,843,377.5 | 7,915,361.5 | +71,984 (+0.072 ms; +0.92%) |
| Median `allocs/op` | 16,247 | 16,291 | +44 |
| Median `B/op` | 14,036,250.5 | 14,045,674.5 | +9,424 |

No-Regression Evidence: the observed production-function median difference is below the predeclared 1 ms/request instrumentation-cost gate. Per-run timing ranges overlap, so this is not a precise tail-cost estimate or an endpoint speedup. The fake readers exercise production partition scanning and JSON assembly but do not simulate PostgreSQL I/O, deployment load, Neo4j, or 100,000 repositories. The earlier isolated 8–9 µs span-construction screen and this end-to-end local function screen measure different work and should not be subtracted from each other.

## Rejected query hypotheses

| Candidate | Fixed-corpus result | Disposition |
| --- | --- | --- |
| Split entity-name/source branches under deterministic `(repo_id, relative_path, entity_id)` order | Correct top-250 identities on four common terms, but slower on every term; `service` about 317 ms ordered-OR versus 1,166 ms split on a preserved 2.808-million-entity PostgreSQL 18.6 corpus | Rejected; no code written |
| Existing OR plus `ORDER BY entity_id` using the primary-key index | Prepared `service` probe 29.3805 ms unordered baseline versus 62.094 ms ordered, with about 14.8 times the shared-buffer hits | Rejected; no code written |

Neither SQL screen is a deployed endpoint comparison. Adding a deterministic cap order would intentionally change capped membership; no such production behavior change is part of this PR. The preserved test database was stopped after each screen. The current long pole is the aggregate probe, but the live slow partition remains unknown.

## Signal and acceptance

Observability Evidence: a recording `postgres.query` parent in `parallel_shared_snapshot` mode gets one `query.code_topic_partition` child for each executed partition. Each child records attempt 0–1, partition ordinal 0–3, successful row count, and `ok`/`error`/`canceled`/`deadline` outcome. A reader-member retry can emit up to eight children under one parent. Duration covers query return, row iteration and scanning, row error check, and cursor close; a cursor-close failure marks the child `error` while retaining the prior API error behavior. No terms, SQL, repository IDs, connection details, credentials, or raw error payloads are span attributes. The single-statement fallback emits no partition child.

Focused production-path tests with Go 1.26.6 verified delayed cursor-close timing, partial rows, query/scan/row errors, in-flight cancellation and deadline, exactly-once cursor cleanup, parent/attempt/ordinal and scope attribution, the real caller's whole-read member-loss retry, concurrent requests, and request completion while the batch exporter stalls. `go test ./internal/query ./internal/query/codetopicparallel ./internal/telemetry -count=1` and focused `go test -race` on the query and partition packages both exited 0. The telemetry-coverage gate and strict docs build also exited 0 before this evidence update and must be rerun on the final diff. The independently reviewed promotion gate remains required before publication. A separate reviewed deployment and organic-traffic trace are needed before the new signal can identify a live partition. A complete resource-qualified endpoint measurement is still needed to decide #7033's `<1 s` acceptance.
