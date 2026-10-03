# Reader stage attribution cost proof for #7545

## Scope

Before this change an operator could not say which guarded-reader stage
(`reader_borrow`, `reader_identity`, `reader_replay`, `business_query`) made one
slow impact-findings call. `otelObserver.Observe` recorded the stage histogram
and started every `postgres.reader_access` span from `context.Background()`, so
each stage span was an orphan root, and the `Observer` interface carried no
context. #7545 reported 1,439 ms in `impact_findings_query` with the mechanism
unattributed.

The change adds three things and no SQL, pool, timeout, or query change:

- `ContextObserver`, an optional interface beside `Observer` in
  `go/internal/runtime/postgres`. `Access` calls `ObserveContext` with the
  request context when the observer implements it and the legacy `Observe`
  otherwise. `otelObserver` implements it, so each `postgres.reader_access`
  span is a child of the request span and the stage histogram sample uses the
  request context.
- `db.StageTimings` in `go/internal/storage/postgres/db`: a fixed-size,
  goroutine-safe accumulator of two atomic counters per stage for the four
  reader stages. `Access` adds each observed reader stage to the accumulator on
  the request context, with or without an observer.
- Four `float64` attributes on the `impact_findings_query` completion log line,
  `reader_borrow_seconds`, `reader_identity_seconds`, `reader_replay_seconds`,
  and `business_query_seconds`. Each is the SUM of that stage across every
  guarded-reader operation inside the findings read (a stage can run more than
  once per request). They appear only when the guarded reader recorded a stage.

One behavior consequence is recorded rather than hidden: stage spans now follow
the request's sampling decision instead of being sampled as independent roots,
so `postgres.reader_access` volume tracks sampled requests.

## Method

`BenchmarkReaderQueryObserve` runs one fenced `QueryContext` through the real
`Access` and `fencedQueryer` over an in-process fake `database/sql` driver that
answers the identity check, the replay fence, and one business row, so every
iteration pays all four stage observations. Request spans and the stage spans
end through a synchronous discarding exporter, and the histogram uses a manual
metric reader, so the OpenTelemetry SDK cost is real and no memory accumulates.
`BenchmarkReaderQueryObserveAccumulator` repeats two arms with a
`db.WithStageTimings` context created per request, so the accumulator cost is
the difference from the same arm without it.

The BEFORE arms ran on a throwaway detached worktree at the merge base
`791078e81` (the merge of #7565) using the same two benchmark fixture files
copied in. The AFTER arms ran on this branch. Both are compiled test binaries,
run in four interleaved rounds (before, after, accumulator) of five
`-test.count` repetitions at `-test.benchtime=20000x`, giving 20 samples per
arm. Go 1.27.1, darwin/arm64, Apple M4 Pro, 12 cores. A different heavy gate
was running on the host: the load average was 9 to 14 during the runs.

```sh
go test -c -o before.test ./internal/runtime/postgres/   # in the base worktree
go test -c -o after.test ./internal/runtime/postgres/    # on this branch
./before.test -test.run XXX -test.bench 'ReaderQueryObserve$' -test.benchtime=20000x -test.count=5
./after.test  -test.run XXX -test.bench 'ReaderQueryObserve$' -test.benchtime=20000x -test.count=5
./after.test  -test.run XXX -test.bench 'ReaderQueryObserveAccumulator' -test.benchtime=20000x -test.count=5
```

## Results

This is a hermetic callback-cost proof on a laptop under load. It is not a
deployed endpoint p95, it excludes a network, real PostgreSQL, and exporter I/O,
and the per-query fixed cost of `database/sql` is part of every number.

Per-query time, ns/op, 20 samples per arm: p50, then p25 to p75, then min to max.

| Arm | Before p50 (p25-p75) | After p50 (p25-p75) | Allocs/op before / after |
| --- | --- | --- | --- |
| `nil_observer` | 2752 (2603-3069) | 2538 (2495-2619) | 57-58 / 57-58 |
| `legacy_observer` | 2722 (2580-2883) | 2604 (2570-2660) | 57-58 / 57 |
| `otel_no_request_span` | 7976 (7787-8129) | 7886 (7789-8004) | 121 / 121 |
| `otel_request_span` | 9018 (8784-9172) | 9235 (8988-9407) | 132 / 132 |

Min to max spread: `nil_observer` 2501-6971 before and 2450-2812 after;
`otel_request_span` 8617-9461 before and 8867-10604 after. The p50 differences
(a 214 ns lower and a 217 ns higher value on arms with 2.5 to 9 microsecond
medians) are inside the interquartile ranges and the host noise. No delta
smaller than that noise is claimed. The allocation counts per query are
identical before and after on every arm, so re-parenting the stage span and
passing the context add no allocation to the observed path. The 57 versus 58
allocations flip appears in both the before and after runs of the same arm and
comes from `database/sql`, not from this change.

The accumulator arms (after only), same units:

| Arm | p50 (p25-p75) | Allocs/op | B/op | Same arm without accumulator |
| --- | --- | --- | --- | --- |
| `acc+nil_observer` | 2772 (2639-3007) | 59-60 | 2984 | 2538, 57-58 allocs, 2872 B |
| `acc+otel_request_span` | 9372 (8994-9920) | 134 | 13038 | 9235, 132 allocs, 12926 B |

Creating the accumulator and its context value adds two allocations and about
112 bytes per request. That is one fixed cost per findings read, not per stage
observation: the four `Add` calls per query are two atomic additions each and
allocate nothing. The `acc+nil_observer` arm, the one with the smallest floor,
shows about 0.1 to 0.25 microseconds more per request with a p25 just above the
p75 of the arm without it; the `acc+otel_request_span` arm overlaps its
counterpart. Against a findings read whose measured stage time in #7545 was
1,439 ms, a fixed sub-microsecond, two-allocation cost is five orders of
magnitude below the signal it attributes. A request with no accumulator pays one
`context.Value` lookup.

Performance Evidence: on the fenced guarded-reader `QueryContext` path with the in-process fake driver, passing the request context to the observer and re-parenting the `postgres.reader_access` span changed no allocation count (57-58, 57-58, 121, 132 allocs/op before and after) and moved p50 ns/op by less than the interquartile noise (otel_request_span 9018 to 9235, nil_observer 2752 to 2538); the per-request `db.StageTimings` accumulator adds two allocations and 112 B once per request. Hermetic callback cost only, not a deployed endpoint p95.

No-Regression Evidence: allocs/op per guarded query are identical before and after on all four observer arms, legacy `Observer` implementations still receive every `Observe` call (`TestLegacyObserverStillReceivesEveryStage`), and the no-accumulator path costs one `context.Value` lookup.

## Observability

Observability Evidence: the `supply chain query stage completed` log line for stage `impact_findings_query` carries `reader_borrow_seconds`, `reader_identity_seconds`, `reader_replay_seconds`, and `business_query_seconds` on every guarded read with no sampling, and `postgres.reader_access` spans are children of the request span in the trace; proven by `TestImpactFindingsStageCompletionCarriesReaderStageSeconds`, `TestReaderStageSpansAreChildrenOfTheRequestSpan`, and `TestGuardedQueryFillsRequestStageTimings`.

Closed sets: the accumulator holds four stages and ignores any other value; the
attribute keys are fixed; no SQL text, error text, endpoint, or identifier enters
the line or the span. The stage histogram `eshu_dp_postgres_reader_stage_duration_seconds`
keeps its `role`, `stage`, and `outcome` labels and its buckets.

## Mutation checks

- Reverting only the span parent (`o.tracer.Start(ctx, ...)` back to
  `context.Background()`) turned `TestReaderStageSpansAreChildrenOfTheRequestSpan`
  red for all four stages with an orphan-root message.
- Removing the accumulator `Add` in `Access.observe` turned
  `TestGuardedQueryFillsRequestStageTimings` red for the nil, legacy, and otel
  observers.
- Dropping the attributes from the findings stage line turned
  `TestImpactFindingsStageCompletionCarriesReaderStageSeconds` red. That test
  records through a store stub because `query` must not import
  `runtime/postgres`; the `Access`-to-accumulator link is the runtime test above.

## Unchecked

Whether ops-qa exports traces, whether its log pipeline keeps the new
attributes, and the deployed share of `impact_findings_query` time by stage are
NOT_CHECKED. This change makes the next slow call attributable; it does not
explain the 1,439 ms in #7545 and does not close it.
