# Entity-context resolution attribution: cost and signals (#7212)

## Scope

This note covers the change that makes `GET /api/v0/entities/{entity_id}/context`
say how each request was answered. `(*Handler).GetEntityContext` now records
one `resolved_by` value (`anchor`, `fallback`, `content`, or `none`) and the
number of graph statements it sent (`statements_tried`) on the request span, and
counts the `resolved_by` value on a new counter. It does not change which
statements run, their order, their deadline, or any response body.

The motivation is in #7212: on a deployed environment, an entity-context call
for an id that is not in the graph runs the whole statement list, then the
content store, then answers 404, and nothing an operator could read separated
that true miss from a hit. The PROFILE output and the sweep figures that issue
quotes are context for why the signal is wanted. They are not proof of this
change, and no deployed measurement is claimed here.

## What changed

- `GetEntityContext` creates a request-local `entityContextResolution` and
  defers `recordEntityContextResolution`. The handler body stays in the one
  method so the query-plan callsite pins keep keying on it.
- The statement loop copies its statement count (`statementsTotal`) and the
  number of statements sent (`statementsTried`) into that record. The loop stops
  at the first row, so the answering statement is the last one tried: only the
  final, unlabeled `MATCH (e)` statement is `fallback`, any earlier one is
  `anchor`.
- A content-store answer sets `content`; a content miss that ends in the 404 sets
  `none`.
- The recorder writes two span attributes and adds 1 to
  `eshu_dp_entity_context_resolution_total{resolved_by}`. It adds no log line:
  the two existing warnings gain a `statements_tried` key.
- The counter is registered in `go/internal/telemetry` with the other
  instruments and documented in the metrics and traces references.

## Counting rules

- Counted: a request that reached the answer decision. That is a graph row (200,
  or the 404 a scoped grant turns a foreign row into), a content answer, or a
  true miss.
- Not counted, and no `resolved_by` on the span: a request that ends in an error
  (graph read error, spent deadline, content store error, relationship builder
  error), and a request rejected before the graph read (400 for a missing
  entity id, 404 for an empty scoped grant). A request that errors after
  sending statements still carries `statements_tried`.
- `h.Instruments` and its counter may be nil; the recorder then writes the span
  attributes only.

## Baseline and after

Baseline is the `origin/main` handler at 1277f0429. After is this branch with
its three commits over that base (head 5f5d8ffd0 before this note). Both were
built from the same benchmark file, `BenchmarkGetEntityContextResolution` in
`context_resolution_bench_test.go`, which uses only `Handler` fields that exist
on both trees; the file compiled unchanged against the baseline.

Backend and version: none. Every graph answer is a hermetic fake
(`graph.FakeGraphReader`) and the content store is
`entityContextFakeContentStore`. No Neo4j, NornicDB, or Postgres ran. The
benchmark measures the cost of the handler's own Go code per request, including
`httptest` request and recorder construction (identical on both trees), and says
nothing about graph round-trip latency. The Neo4j statement shape is the
two-statement list (indexed `CALL ()` anchor, then the unlabeled fallback). The
NornicDB shape is the per-label loop: one statement per entry in
`EntityContextAnchorLabels` plus the unlabeled one, 16 in total.

Host and method: one Apple M5 Max, 18 cores, Go 1.27.1, one process at a time,
`-cpu=1 -benchtime=2000x -count=1` per process, ten processes per tree run
alternately (after, base, after, base, and so on) from test binaries built once
per tree. The host was shared and oversubscribed: load average (1 minute) was
33.3 before the runs and 31.9 after, on 18 cores. The ten alternating pairs
finished in under a minute.

### Input shape and terminal counts

Each case runs twice. `bare` has no instruments and a request context with no
recording span (the cheapest the handler can run). `instrumented` has real SDK
instruments (manual-reader meter provider) and a recording server span (SDK
tracer provider without a processor), the production shape. `stmts/op` is the
number of graph statements the fake received per request and was the same on
both trees in every run.

| case | graph answer | content store | status | statements_tried | resolved_by |
| --- | --- | --- | --- | --- | --- |
| anchor_neo4j_fast_path | first statement hits | unused | 200 | 1 | anchor |
| fallback_neo4j_second_statement | second (unlabeled) statement hits | unused | 200 | 2 | fallback |
| none_true_miss_neo4j | both miss | answers nothing | 404 | 2 | none |
| content_answer_neo4j | both miss | answers | 200 | 2 | content |
| anchor_nornicdb_loop_third_label | third labeled statement hits | unused | 200 | 3 | anchor |
| none_true_miss_nornicdb_loop | all 16 miss | answers nothing | 404 | 16 | none |

The `statements_tried` and `resolved_by` columns are pinned per case by the
package tests (`assertResolved` in `context_resolution_test.go` and
`context_resolution_unanswered_test.go`), which read the span attributes and the
counter. The benchmark itself asserts the HTTP status and reports `stmts/op`.

## Performance Evidence

Performance Evidence: per request, the change adds 2 allocs/op and 128 B/op with no instruments or recording span, and 8 allocs/op and about 489 B/op in the production shape, against 48 to 171 allocs/op before; statements sent per request are unchanged in every case, and ns/op could not be sized on an oversubscribed host (the instrumented medians were higher after the change in every case, so a small CPU cost is expected and unmeasured).

`allocs/op` and `B/op` are deterministic and are the primary figure. Medians of
ten runs per tree; `B/op` varied by at most 2 bytes between runs of one tree.

| case | variant | allocs/op base | allocs/op after | B/op base | B/op after |
| --- | --- | --- | --- | --- | --- |
| anchor_neo4j_fast_path | bare | 96 | 98 | 17491 | 17619 |
| anchor_neo4j_fast_path | instrumented | 98 | 106 | 18019 | 18508 |
| fallback_neo4j_second_statement | bare | 96 | 98 | 17491 | 17619 |
| fallback_neo4j_second_statement | instrumented | 98 | 106 | 18019 | 18508 |
| none_true_miss_neo4j | bare | 48 | 50 | 15130 | 15258 |
| none_true_miss_neo4j | instrumented | 50 | 58 | 15658 | 16147 |
| content_answer_neo4j | bare | 94 | 96 | 17195 | 17323 |
| content_answer_neo4j | instrumented | 96 | 104 | 17723 | 18211 |
| anchor_nornicdb_loop_third_label | bare | 169 | 171 | 63354 | 63483 |
| anchor_nornicdb_loop_third_label | instrumented | 171 | 179 | 63882 | 64370 |
| none_true_miss_nornicdb_loop | bare | 121 | 123 | 60992 | 61120 |
| none_true_miss_nornicdb_loop | instrumented | 123 | 131 | 61521 | 62007 |

The instrumented overhead is the same 8 allocations and about 489 bytes in all
six cases, so it does not grow with the number of statements. I did not profile
which call sites own the allocations (the request-local record, the deferred
closure, the two `SetAttributes` calls, and the counter `Add` with its
attribute set are the candidates); that attribution is NOT_CHECKED.

`ns/op` is secondary. Medians in nanoseconds (min to max of the ten runs in
parentheses), base then after:

| case | variant | base | after | delta |
| --- | --- | --- | --- | --- |
| anchor_neo4j_fast_path | bare | 11786 (8804 to 15674) | 11034 (9072 to 15629) | -6.4% |
| anchor_neo4j_fast_path | instrumented | 10834 (9128 to 22352) | 11456 (10011 to 17129) | +5.7% |
| fallback_neo4j_second_statement | bare | 10199 (8436 to 17804) | 10265 (8993 to 23971) | +0.6% |
| fallback_neo4j_second_statement | instrumented | 10866 (8734 to 18077) | 11910 (9618 to 20331) | +9.6% |
| none_true_miss_neo4j | bare | 5376 (4101 to 8943) | 5310 (4365 to 8518) | -1.2% |
| none_true_miss_neo4j | instrumented | 5696 (4718 to 8763) | 6156 (5247 to 13727) | +8.1% |
| content_answer_neo4j | bare | 9371 (7762 to 13713) | 9408 (8273 to 17325) | +0.4% |
| content_answer_neo4j | instrumented | 10365 (8152 to 16829) | 12152 (9184 to 18808) | +17.2% |
| anchor_nornicdb_loop_third_label | bare | 22924 (16534 to 45566) | 24716 (18360 to 34558) | +7.8% |
| anchor_nornicdb_loop_third_label | instrumented | 23265 (18291 to 34296) | 24005 (19533 to 32817) | +3.2% |
| none_true_miss_nornicdb_loop | bare | 18950 (12410 to 29756) | 17316 (13333 to 24341) | -8.6% |
| none_true_miss_nornicdb_loop | instrumented | 16363 (12493 to 28573) | 16657 (14364 to 27335) | +1.8% |

Reading the table: the spread inside one tree is wider than most medians'
differences, and the bare deltas fall on both sides of zero (-8.6% to +7.8%), so
the host noise is of the size of the signal. The instrumented deltas are
positive in all six cases (+1.8% to +17.2%), which is the direction the 8 extra
allocations predict, but ten runs on a host at load 32 cannot size that cost.
This note therefore does not claim ns/op parity, and it does not claim a
speedup. The instrumented medians moved by +0.5 to +1.8 microseconds in the
four Neo4j cases and by +0.3 to +0.7 in the two NornicDB cases, within the
spread of the runs. The expectation is that this is small next to a graph round
trip of milliseconds, but no live backend measured that: NOT_CHECKED.

Reproduce with the command below, once on this branch and once on a detached
worktree of the base commit, alternating the two trees run by run (the numbers
above used ten runs of 2000 iterations each):

```
cd go && env -u GOROOT go test -run '^$' -bench BenchmarkGetEntityContextResolution -benchmem -count=10 -benchtime=2000x -cpu=1 ./internal/query/entity/
```

NOT_CHECKED: ns/op on a quiet host, the cost against a live Neo4j or NornicDB,
the cost on a deployed environment under the real request mix, and the
allocation attribution by call site.

Why no larger proof is needed: the change adds no statement, no loop, no lock,
no goroutine, no queue or lease use, and no graph or SQL write. The request-local
record is never shared across goroutines. The statement count per request is
the same on both trees in every case, which is expected to dominate the real
latency (the #7212 sweep points that way but is context here, not proof). The change is on the read-only query path and touches no worker,
batch size, or concurrency setting.

## Observability Evidence

Observability Evidence: the counter eshu_dp_entity_context_resolution_total{resolved_by}, the span attributes eshu.entity_context.resolved_by and eshu.entity_context.statements_tried on the request span, and a statements_tried key on the two existing handler warnings let an operator read the true-miss share and the statements wasted per request at 3 AM; the list below gives each signal.

- `eshu_dp_entity_context_resolution_total{resolved_by}`: counter with the closed
  value set `anchor`, `fallback`, `content`, `none`. `none / sum(rate(...))` over
  a window is the share of requests that ran the whole statement list, the
  content store, and ended in 404. Errors and pre-read rejections are not
  counted (see Counting rules), so the series is not a request counter.
- `eshu.entity_context.statements_tried` on the request span: the number of
  graph statements sent before the request resolved or ended, 0 when no graph
  reader is configured. Present on every request that passed input and access
  validation, including one that ends in an error.
- `eshu.entity_context.resolved_by` on the same span: present only when the
  request reached the answer decision; same value set as the counter.
- The warning "entity context anchor loop ended with an error before resolving"
  keeps `labels_tried` and `labels_total` and gains `statements_tried`, so a
  failed request says how far down the list it got.
- The `backend_anchor_mismatch` scoped-grant warning gains `statements_tried`.
- No new log line was added: a healthy request logs nothing new, so log volume
  does not move.

At 3 AM: a rising `none` share on the counter says callers are asking for ids
the graph and the content store do not hold. A high `statements_tried` on the
slow spans, with `resolved_by` of `none` or `fallback`, says the latency is the
unlabeled scan after the indexed anchors missed, not a slow indexed anchor. A
request that is slow and has `statements_tried` but no `resolved_by` ended in an
error; its warning line carries the failure class.

The signals are proven by the package tests, which read the span, the counter,
and the log lines for each outcome (anchor on both dialects, fallback on both,
content with and without a graph reader, none, graph error, content errors, and
the pre-read rejections). The telemetry contract test and the metrics and traces
references list the new instrument and attributes.

## Why it is safe

- No statement text, count, order, parameter, or deadline moved. The
  `statements_tried` and `stmts/op` figures above are the same on both trees.
- No response body or status moved: the existing handler tests, including the
  graph-error, deadline, and scoped-grant tests, pass unchanged, and the new
  tests pin that an error request has no `resolved_by`.
- The recorder reads state the handler already has, runs once per request from a
  deferred call, and is nil-safe for a nil record, an unstarted record, nil
  instruments, and a nil counter.
- The counter has four label values, fixed in the code, so series cardinality is
  bounded at four. The span attributes are an integer and one of the same four
  strings.
- The measured cost is the allocations above. A small CPU cost from them is
  expected and was not sized (see NOT_CHECKED).
