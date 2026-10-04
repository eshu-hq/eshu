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
  span is a child of whatever span is active on the request context (the API
  server span or the query handler span) and the stage histogram sample uses
  the request context.
- `db.StageTimings` in `go/internal/storage/postgres/db`: a fixed-size,
  goroutine-safe accumulator of two atomic counters per stage for the four
  reader stages. `Access` adds each observed reader stage to the accumulator on
  the request context, with or without an observer.
- Four `float64` attributes on the `impact_findings_query` completion log line,
  `reader_borrow_seconds`, `reader_identity_seconds`, `reader_replay_seconds`,
  and `business_query_seconds`. Each is the SUM of that stage across every
  guarded-reader operation inside the findings read (a stage can run more than
  once per request). They appear only when the guarded reader recorded a stage.

The reader stages time the borrow, the identity check, the replay fence, and
the database call that starts the business query. Row streaming and scanning
happen afterward in the row cursor and are not timed, so the four sums can be
well below `duration_seconds`; the remainder is row consumption, decoding, and
handler-side work.

Three behavior consequences are recorded rather than hidden. Stage spans now
follow the request's sampling decision instead of being sampled as independent
roots, so `postgres.reader_access` volume tracks sampled requests. Reader
stage spans are children of whatever span is active on the request context (the
API server span or the query handler span); the writer-checkpoint span is a
child of the API server span and stays a root on MCP, which has no server span
when the checkpoint is taken. The histogram sample is recorded with the request
context, so for a sampled request the SDK's default trace-based exemplar filter
(`exemplar.TraceBasedFilter` in the pinned `go.opentelemetry.io/otel/sdk/metric`
v1.45.0, with nothing in the repo setting `OTEL_METRICS_EXEMPLAR_FILTER`)
attaches a trace exemplar; set `OTEL_METRICS_EXEMPLAR_FILTER` to change it.

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

The existing `BenchmarkReaderQueryStartRecordedRequest` never calls the
observer, so it cannot measure this path; `BenchmarkReaderQueryObserve` is new
and drives the fenced `QueryContext`. The figures below come from a quiet remote
Linux host. The BEFORE arms ran on a clean checkout of #7572's base `4529a8f9`
with only the two benchmark fixture files copied in (the base has no
`ContextObserver` symbol; the AFTER binary has four). The AFTER arms ran on the
reviewed head of #7572 (`d67c4ac8`, whose tree equals the squash-merge commit
`6e2307c1c` on `main`). Both checkouts were fetched by commit from Git, and
both are compiled test binaries (`go1.26.6 linux/amd64`, picked from `go.mod`),
run in 10 interleaved rounds with the order rotated, 5 repetitions each at
`-test.benchtime=20000x -test.benchmem`, GOMAXPROCS 16: 50 samples per arm. Host:
AMD EPYC 9R14, 16 CPUs, 123 GiB, Linux 6.17; the remote-validation profile
labels it r7a.4xlarge class (the instance type itself was not read from the
metadata service). Load average was 0.24 before the run and peaked at 2.50 during it,
which includes the build of the two binaries. An earlier run of the same
benchmarks on a laptop under a load average of 9 to 14 (Go 1.27.1, Apple M4 Pro,
base `791078e81`) is superseded by this one and not used; its "inside the noise"
reading did not survive a quiet host.

```sh
go test -c -o before.test ./internal/runtime/postgres/   # in the base worktree
go test -c -o after.test ./internal/runtime/postgres/    # on this branch
./before.test -test.run XXX -test.bench 'ReaderQueryObserve$' -test.benchtime=20000x -test.count=5
./after.test  -test.run XXX -test.bench 'ReaderQueryObserve$' -test.benchtime=20000x -test.count=5
./after.test  -test.run XXX -test.bench 'ReaderQueryObserveAccumulator' -test.benchtime=20000x -test.count=5
```

## Results

This is a hermetic callback-cost proof on a quiet host. It is not a deployed
endpoint p95. It excludes a network, real PostgreSQL, and exporter I/O, and the
per-query fixed cost of `database/sql` is part of every number. Absolute numbers
do not compare with any run on another machine or Go version; only the BEFORE to
AFTER comparison on this host is valid.

Per-query time, ns/op, 50 samples per arm, median with the interquartile range
in parentheses. Rounds slower counts the interleaved rounds in which the AFTER
median was above the BEFORE median.

| Arm | Before | After | Delta | Rounds slower | B/op, allocs/op before = after |
| --- | --- | --- | --- | --- | --- |
| `nil_observer` | 5887 (5827-5957) | 5974 (5938-6056) | +86 ns (+1.5%) | 9/10 | 2856, 59 |
| `legacy_observer` | 6048 (5858-6106) | 6117 (6028-6179) | +70 ns (+1.1%) | 9/10 | 2856, 59 |
| `otel_no_request_span` | 16918 (16728-17027) | 17358 (17192-17499) | +439 ns (+2.6%) | 10/10 | 11176, 123 |
| `otel_request_span` | 19045 (18700-19170) | 19585 (19462-19709) | +540 ns (+2.8%) | 10/10 | 12912, 134 |

Deltas are computed from the unrounded medians, so a delta can differ by 1 ns
from the difference of the rounded columns. Allocation counts and bytes per query
are identical before and after on every arm, so re-parenting the stage span and passing the context add no allocation to
the observed path. The OpenTelemetry arms are measurably slower: their
interquartile ranges do not overlap and the AFTER median was higher in all 10
rounds. Each fenced query makes four stage observations, so the otel delta is
about 110 to 135 ns per observation. The nil and legacy arms moved by 70 to
86 ns: their interquartile ranges overlap, but 9 of 10 rounds were slower, so the
shift is small and probably real. The cause of the otel delta is a hypothesis only: it fits the
extra work the change does (the request context reaches the histogram sample, a
child stage span is started from it, and the observer pays one `context.Value`
lookup and a type assertion per observation). It was not separated from
code-layout differences between two compiled binaries, and no profile was taken.

The accumulator arms (AFTER only), same units, compared with the same arm without
the accumulator:

| Arm | ns/op without, with | Delta | B/op | allocs/op | Rounds slower |
| --- | --- | --- | --- | --- | --- |
| `acc+nil_observer` | 5974, 6216 | +242 ns | +112 | +2 | 10/10 |
| `acc+otel_request_span` | 19585, 19799 | +214 ns | +112 | +2 | 8/10 |

Creating the accumulator and its context value adds two allocations and about
112 bytes per request. That is one fixed cost per findings read, not per stage
observation: the four `Add` calls per query are two atomic additions each and
allocate nothing. Against a findings read whose measured stage time in #7545 was
1,439 ms, the largest per-query delta here (+540 ns for the observer change, a
further +214 to +242 ns for the accumulator) is under one part in a million of
the signal it attributes. Every observer arm moved by less than 3% and the
accumulator arms by 1.1% (`acc+otel_request_span`) and 4.1%
(`acc+nil_observer`), all well under the 10% line that would call for a profile. A request with no accumulator pays one
`context.Value` lookup.

Performance Evidence: on the fenced guarded-reader `QueryContext` path with the in-process fake driver, on a quiet 16-CPU Linux host (50 interleaved samples per arm, `go1.26.6`), passing the request context to the observer and re-parenting the `postgres.reader_access` span changed no allocation count or byte count (59, 59, 123, 134 allocs/op before and after) and raised median ns/op by +86 ns (+1.5%) on `nil_observer`, +70 ns (+1.1%) on `legacy_observer`, +439 ns (+2.6%) on `otel_no_request_span` and +540 ns (+2.8%) on `otel_request_span`, with the otel arms slower in 10 of 10 rounds; the per-request `db.StageTimings` accumulator adds two allocations, 112 B and 214 to 242 ns once per request. Hermetic callback cost only, not a deployed endpoint p95.

No-Regression Evidence: allocs/op and B/op per guarded query are identical before and after on all four observer arms, every observer arm's median moved by less than 3% and the accumulator arms by 1.1% and 4.1% (all below the 10% profiling line), legacy `Observer` implementations still receive every `Observe` call (`TestLegacyObserverStillReceivesEveryStage`), and the no-accumulator path costs one `context.Value` lookup. The otel arms are measurably slower, by 2.6% and 2.8%; that cost is accepted as the price of request-parented spans and per-stage attribution and is recorded, not hidden.

## Observability

Observability Evidence: the `supply chain query stage completed` log line for stage `impact_findings_query` carries `reader_borrow_seconds`, `reader_identity_seconds`, `reader_replay_seconds`, and `business_query_seconds` on every guarded read with no sampling, and reader `postgres.reader_access` spans are children of the span active on the request context in the trace; proven by `TestImpactFindingsStageCompletionCarriesReaderStageSeconds`, `TestReaderStageSpansAreChildrenOfTheRequestSpan`, and `TestGuardedQueryFillsRequestStageTimings`.

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
- Swapping two slots in `requestReaderStage` (identity with replay, and borrow
  with business query), applied through a `go test -overlay` copy and never to
  the real file, turned `TestRequestStageTimingsKeepEachStageInItsOwnSlot` red;
  it gives one stage a 25 ms fake-driver delay per case and requires that slot
  to carry it while every other slot stays below it. The real tree passes.
- Dropping the attributes from the findings stage line turned
  `TestImpactFindingsStageCompletionCarriesReaderStageSeconds` red. That test
  records through a store stub because `query` must not import
  `runtime/postgres`; the `Access`-to-accumulator link is the runtime test above.

## Unchecked

Whether ops-qa exports traces, whether its log pipeline keeps the new
attributes, and the deployed share of `impact_findings_query` time by stage are
NOT_CHECKED. This change makes the next slow call attributable; it does not
explain the 1,439 ms in #7545 and does not close it.
