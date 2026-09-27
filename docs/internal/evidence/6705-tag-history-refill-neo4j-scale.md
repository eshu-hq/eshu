# #6705 Tag-History Refill: Shared Deadline And Neo4j Corpus-Scale Timing

`taghistory.RefillScopedPage` (`go/internal/query/taghistory/refill.go`) can
issue up to `taghistory.MaxRefillReads*2` = 8 sequential graph reads -- one
keyset window plus one `BuiltFromCypher` lookup per window -- to answer ONE
scoped `GET /api/v0/images/tag-history` request. `Neo4jReader.runRead`
(`go/internal/query/neo4j_read_policy.go`) gives EACH read its own fresh
`querycontract.DefaultGraphReadTimeout` (10s) window unless the caller shares
one, which this loop had not been doing -- the same gap #7006 closed for
`infra_relationship_filter.go`'s and `entity/context_handler.go`'s per-label
loops. Left open, production's raw request context (which carries no deadline
of its own) let one request cost up to `MaxRefillReads*2*10s` (about 80s)
instead of the single bounded-read budget a lone graph statement gets.

This document records two things: the deadline fix, and the corpus-scale
measurement `MaxRefillReads`'s doc comment had been missing -- the #6564
evidence doc's own "Known hazard" section says plainly that "no corpus-scale
timing was run locally for this change" and that the refill loop's cost was
bounded by multiplying a single NornicDB lookup's latency by the cap, never
benchmarked. That multiplication also measured the wrong backend: NornicDB,
not the pinned Neo4j the query-plan gate profiles against.

## The Fix

`RefillScopedPage` now derives ONE `querycontract.WithBoundedGraphReadDeadline`
budget (`querycontract.WithGraphQueryName(ctx, "tag_history.refill")`) before
its loop's first iteration and reuses it for every one of the loop's reads,
with `defer cancel()` releasing it on every return path. `writeTagHistoryReadError`
(`go/internal/query/tag_history.go`) also maps a bypassing `GraphQuery`'s raw
`context.DeadlineExceeded` to the existing `querycontract.ErrGraphReadDeadline`
504 response -- the same defensive fallback `infra_relationship_filter.go` and
`entity/context_handler.go` carry for their own #7006 loops, for a `GraphQuery`
implementation (a fake, or a future backend) that does not already wrap a spent
deadline the way the real `Neo4jReader` does.

TDD proof (`go/internal/query/taghistory/refill_deadline_test.go`,
`go/internal/query/tag_history_deadline_test.go`):

- RED (before the fix), `go test ./internal/query/taghistory/... -run
  TestRefillScopedPage -v -count=1`:
  `TestRefillScopedPageSharesOneDeadlineAcrossReads` failed with "ctx has no
  deadline" on all 8 recorded calls, because the loop passed the caller's
  context straight through with no deadline of its own.
- GREEN after the fix: the same command passes, and every one of the loop's 8
  calls (4 windows x keyset-read-plus-`BuiltFromCypher`) observes the SAME
  `ctx.Deadline()` instant.
- `TestRefillScopedPageReturnsDeadlineErrorWhenBudgetExhaustedMidLoop` proves a
  budget spent partway through the loop (after the first window already
  succeeded) returns an error `errors.Is` `querycontract.ErrGraphReadDeadline`,
  with zero rows and `CapReached=false` -- never a partial page served as
  complete.
- `TestTagHistoryScopedReadTranslatesASpentBudgetToDeadlineResponse` is the
  handler-level RED/GREEN for the `writeTagHistoryReadError` translation: RED
  was `status=500` with body `"query failed: context deadline exceeded"`;
  GREEN is `status=504`.

## Cap Decision: Keep `MaxRefillReads = 4`

The measurement below settles the question `MaxRefillReads`'s doc comment
left open: on the pinned Neo4j, at both 5,000- and 10,000-image corpus scale,
the fully-withheld worst case the cap was sized for costs low tens of
milliseconds per call, not seconds -- roughly two orders of magnitude under
the single 10s bounded-read budget the shared-deadline fix now enforces, and
roughly three orders of magnitude under the 80s an unbounded 8-read loop could
have cost before it. **`MaxRefillReads` stays 4.** Nothing in this measurement
argues for raising it (Neo4j has no latency pressure pushing the cap up) or
for lowering it (the cap is nowhere close to binding against the shared
budget); the disclosure-granularity argument already on `RefillScopedPage`'s
doc comment (a fixed 800-raw-row scan span) is the one still doing the actual
work of choosing 4, and it does not depend on backend timing at all.

## Identity

- Eshu commit under test: `3b0f8ca960f14a4d0952253d93b8955784028f51` (the
  #6705 shared-deadline fix; branch `perf/6705-tag-history-refill-deadline`).
- Container: `docker run -d --name eshu-6705-neo4j -p 17705:7687 -e
  NEO4J_AUTH=neo4j/eshu-6705-pass neo4j:2026-community`.
- Image digest: `sha256:91fb0bf237c41b7b3dcbe84703aa0b82e0d7d067b16e1c8ab21f03fc679edf4e`
  (`docker inspect eshu-6705-neo4j --format '{{.Image}}'`, matching `docker
  inspect neo4j:2026-community --format '{{index .RepoDigests 0}}'`).
- Neo4j self-reported version: `2026.09.0` (`docker exec eshu-6705-neo4j neo4j
  --version`).
- Schema: applied through `graph.EnsureSchemaWithBackend(ctx, executor,
  logger, graph.SchemaBackendNeo4j)` -- the SAME production DDL
  `cmd/api`'s and `cmd/bootstrap-index`'s bootstrap paths run, including
  `container_image_digest` (`ContainerImage.digest`) and
  `container_image_tag_observation_ref` (`ContainerImageTagObservation.image_ref`)
  from `go/internal/graph/schema_tables_indexes.go` -- not a hand-copied
  subset, applied by `refillScaleSchemaExecutor` in
  `go/internal/query/tag_history_refill_scale_neo4j_live_test.go` before any
  seed write.
- Host: local macOS laptop, SHARED with other concurrent agent work (a second,
  differently-named/-ported Neo4j container for an unrelated issue was running
  throughout). `absolute_target_applicable: false` -- these are relative,
  same-machine numbers, not a claim about a dedicated or production host.
- Removed with `docker rm -f eshu-6705-neo4j` after the final run; `docker ps
  -a --filter name=eshu-6705-neo4j` then lists nothing, and nothing else was
  touched on the shared host.

## Seed Shape (Per Corpus Size N: 5,000 And 10,000)

Written and verified by `refillScaleGraph.seed` in
`go/internal/query/tag_history_refill_scale_neo4j_live_test.go`:

- N background `ContainerImage {digest}` nodes (digest prefix
  `sha256:6705-corpus-<N>-`), unconnected -- sizing the
  `container_image_digest` index to the scale the #6705 claim comment's
  ops-qa probe measured against (a 400-key `NodeIndexSeek`, 0-1ms, on a real
  store rather than an empty one).
- One `image_ref` ("withheld") carrying 850 `ContainerImageTagObservation`
  rows (> 4*`taghistory.MaxLimit` = 800) whose `resolved_digest` values have
  NO matching `ContainerImage` node at all -- every window's `BuiltFromCypher`
  lookup resolves zero edges, so the scoped page is fully withheld and the
  refill loop walks every one of `taghistory.MaxRefillReads` windows.
- One `image_ref` ("granted") carrying the same 850-row count, each row's
  `resolved_digest` backed by its own `ContainerImage` node and a
  `BUILT_FROM` edge to one `Repository` the test's `RepositoryAccessFilter`
  grants -- the control proving no granted row is lost at this scale.
- Counts verified by count queries before any read is timed: `count(i)` on
  the corpus prefix equals N; `count(t)` on each `image_ref` equals 850;
  `count(b)` on `BUILT_FROM` edges into the granted repository equals 850.
  Any mismatch fails the seed with `t.Fatalf` before a single
  `RefillScopedPage` call runs, so a dropped write cannot produce a vacuous
  pass.
- Cleaned up per corpus size (`refillScaleGraph.cleanup`) immediately after
  its subtest, and the container removed at the end (see Identity).

## Measurement

`TestTagHistoryRefillScaleNeo4jLive` (env-gated
`ESHU_TAG_HISTORY_REFILL_SCALE_LIVE=1`) times the FULL `RefillScopedPage` call
through the real `*query.Neo4jReader` -- the same reader production wires,
carrying the same bounded-read policy -- cold (the first call after seed) and
warm (20 further calls), on the fully-withheld `image_ref`, at
`limit=100`. It then walks the granted `image_ref` to completion and asserts
`kept == 850`.

Run:

```text
cd go && ESHU_TAG_HISTORY_REFILL_SCALE_LIVE=1 \
  ESHU_NEO4J_URI=bolt://127.0.0.1:17705 \
  ESHU_NEO4J_USERNAME=neo4j ESHU_NEO4J_PASSWORD=eshu-6705-pass \
  go test ./internal/query -run TestTagHistoryRefillScaleNeo4jLive -count=1 -v -timeout 20m
```

Three independent process runs, same container, same seed shape each time
(seeded and cleaned up fresh per run):

| N | variant | reads | cap_reached | truncated | run 1 cold (ms) | run 2 cold (ms) | run 3 cold (ms) | warm p50 (ms) | warm p95 (ms) | warm max (ms) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 5,000 | withheld | 4 | true | true | 274.89 | 14.57 | 23.64 | 10.30-17.73 | 16.48-36.33 | 18.42-48.96 |
| 10,000 | withheld | 4 | true | true | 19.30 | 12.30 | 11.17 | 10.17-12.34 | 14.76-20.73 | 16.22-20.96 |

| N | variant | pages | run 1 cold (ms) | run 2 cold (ms) | run 3 cold (ms) | kept | want |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 5,000 | granted | 9 | 9.56 | 4.31 | 3.61 | 850 | 850 |
| 10,000 | granted | 9 | 4.29 | 3.33 | 3.52 | 850 | 850 |

Run 1's n=5,000 cold figure (274.89ms) is the process's first-ever query of
that statement shape against this container -- Neo4j's own query-plan-cache
cold start, on top of session/connection setup. Every later "cold" figure in
this table (including n=10,000 within the same run, and every figure in runs
2-3) is process-cold but SERVER-plan-cache-warm, because the same long-lived
container answered every run: those numbers are the ones to compare against
the shared 10s budget for a caller hitting an already-served route, and
274.89ms is the one honest "first request in this route's lifetime" figure.
Both are reported rather than averaged together, because they answer
different questions and averaging would hide the plan-cache-cold outlier
inside a warm-dominated mean.

On the n=5,000 withheld cell, all three independent runs hit CapReached=true,
Truncated=true, with Reads=taghistory.MaxRefillReads (4):

Measurement: 3/3 trials (ledger:6705-refill-cap-n5000)

The same held on the n=10,000 withheld cell:

Measurement: 3/3 trials (ledger:6705-refill-cap-n10000)

On the n=5,000 granted-control cell, all three independent runs walked the
full 850-row history to completion with kept == 850 (no granted row lost):

Measurement: 3/3 trials (ledger:6705-refill-kept-n5000)

The same held on the n=10,000 granted-control cell:

Measurement: 3/3 trials (ledger:6705-refill-kept-n10000)

Every one of the six independent process runs -- 3 trials x 2 corpus sizes,
28 (5000) and 28 (10000) `RefillScopedPage` calls issued per run (1 cold +
20 warm on the withheld path, 9 pages on the granted control, minus the
shared cold/first-page double-count) -- passed with the SAME qualitative
result: cap hit exactly at 4 reads on the withheld path, zero granted rows
lost on the control path, and every observed latency at least an order of
magnitude under the single 10s bounded-read budget the #6705 fix now shares
across the whole loop.

## Performance Evidence

Performance Evidence: the #6705 shared-deadline fix changes no Cypher
statement text and no query shape -- `taghistory.ReadWindow` and
`taghistory.LookupBuiltFromRepositories` are unchanged; only the `ctx` they
receive now carries one shared deadline instead of none. The corpus-scale
measurement above is therefore a NEW baseline for the refill loop's real
cost on the pinned Neo4j (the #6564 evidence doc measured only NornicDB's
single-lookup cost and explicitly never benchmarked the refill loop itself),
not a before/after on this change's own behavior: the query plan and its
`db hits` are identical with or without the shared deadline, and the fix's
only effect on a request that completes within budget is zero added
overhead (one `context.WithTimeout` allocation and one deferred cancel per
scoped request). Correctness across all `RefillScopedPage`-owned dispatch
variants (fully-withheld cap-reached, granted-control full walk, and -- in
`refill_deadline_test.go` -- the mid-loop budget-exhaustion error path) holds
on Neo4j at 5,000- and 10,000-image scale with zero rows lost and the cap
hit exactly where `taghistory.MaxRefillReads` says it should be.

## Observability Evidence

Observability Evidence: the existing handler span attributes
`eshu.query.tag_history.refill_reads` and
`eshu.query.tag_history.refill_read_cap_reached`
(`go/internal/query/tag_history_telemetry.go`,
`annotateTagHistoryRefill`) already tell an operator whether a scoped page
paid more than one window and whether it hit the cap -- unchanged by this
fix, and now backed by a measured Neo4j cost instead of an unmeasured one.
What #6705 adds: the deadline path itself is diagnosable. `Neo4jReader.runRead`
attributes a spent bounded-read budget with
`querycontract.GraphQueryNameFromContext(ctx)` -- now `"tag_history.refill"`
for every read this loop makes, where before it fell back to the
low-cardinality `"unnamed"` default -- on the `query.graph_read.warning` log
line and its matching span, and classifies the deadline via
`querycontract.IsBoundedGraphReadDeadline` so the log/span/status distinguish
the graph-read policy's OWN spent budget from an unrelated caller deadline
(`graphReadOutcomeDeadline` vs. `graphReadOutcomeCallerDeadline`). The
handler-level response is the existing, unchanged 504 shape
(`WriteGraphReadError` -> `querycontract.ErrGraphReadDeadline` ->
`ErrorCodeBackendTimeout`), recorded under the existing
`backend_unavailable` outcome on
`eshu_dp_query_container_image_tag_history_duration_seconds` and
`..._errors_total` -- an operator sees a named, classified timeout at 3 AM
rather than an anonymous one, and never a silently long request.

## Package Placement

No package or file moved. `writeTagHistoryReadError`'s defensive translation
was added inline in `tag_history.go` rather than split into a new file: that
directory is pinned at the dirgate 40-file cap
(`scripts/lib/dirgate-grandfather.tsv`; see the #6564 evidence doc's Package
Placement section for the same constraint), and `tag_history.go` itself sits
at exactly 500 non-test lines after this change -- at the cap, not over it.
