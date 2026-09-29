# #7246 change-planning coverage truth

The change-planning responses previously lost a PostgreSQL topic candidate-pool
cap when they shaped returned rows, and an empty nonzero-offset page carried no
row from which to recover the cap state. This change propagates known pool and
changed-path symbol caps through change-surface, pre-change, and developer
change-plan responses. The empty-offset case reports partial coverage with
`candidate_pool_status=unknown_empty_page`; it does not assert that more rows
exist. Developer patch guidance is unsafe when evidence is missing, truncated,
or partial.

## Benchmark Evidence:

The changed work is Go response shaping. The SQL text, graph statements, index
selection, row fetch, authorization check, and storage state are unchanged.
The comparison uses the same `BenchmarkChangeSurfaceResponseCoverage` test file
(`GOMAXPROCS=1 go test ./internal/query/impact -run '^$' -bench
'^BenchmarkChangeSurfaceResponseCoverage$' -benchtime=500ms -count=1`, with
an isolated `GOCACHE` in each worktree) on base `8ffb434fd0cb` and the
candidate, Go 1.27.1 on darwin/arm64 (Apple M5 Max). The fixture has one 100-symbol topic
response with zero graph-impact rows and one empty page at offset 5,000 with
zero symbols and zero graph-impact rows. The benchmark stops at
`changeSurfaceResponse` return; it excludes Postgres, Neo4j/NornicDB,
transport, and handler span time. Both variants were warmed before ten
alternating base/candidate pairs (500 ms per sub-benchmark). No peer build or
gate process was observed during the series. The candidate production tree was
`22d05219e7`; the base was `8ffb434fd0`.

| Shape | Base ns/op | Candidate ns/op | Base B/op / allocs | Candidate B/op / allocs |
| --- | ---: | ---: | ---: | ---: |
| 100 symbols, offset 0 | 20,278.5 | 20,239.5 | 38,420 / 242 | 38,420 / 242 |
| Empty page, offset 5,000 | 2,284.5 | 2,740.0 | 3,952 / 44 | 4,944 / 52 |

These are medians of ten runs per arm; the 100-symbol paired median delta was
-1.7%, within its 18,502-34,935 ns/op base and 18,664-39,505 ns/op candidate
ranges. The empty-page paired median delta was +27.7%, or +455.5 ns by
arm medians. The eight added allocations and 992 B are a real cost of making
the partial state observable. A separate five-second allocation profile points
to coverage map copying and building the `coverage_partial` AnswerMetadata
reason; removing those fields would restore the misleading complete answer.
This exceeds the relative 10% investigation threshold, so it was profiled
rather than hidden as noise.

The new loop inspects only the topic rows already returned to the adapter,
bounded by the request limit plus one (at most 101). The changed parent
response adds a constant number of map fields only when coverage is partial.
The measured extra 0.456 microseconds on the empty response cannot explain
the original seconds-scale endpoint latency, but it is not a built-handler or
deployed p95 comparison. No end-to-end latency improvement is claimed. The
original issue's observed 3.0-5.7 s planning reads and under-1-second cold/warm
p95 target remain open; this
microbenchmark cannot prove the endpoint budget. Local PostgreSQL 18 fixture
proof exercised an actual capped 4,000-row topic pool and empty offset 5,000
page; it returned zero rows and reported the pool status as unknown. That
isolated fixture is behavioral proof, not deployed ops-qa timing.

## Observability Evidence:

The response itself exposes `truncated`,
`code_surface.candidate_pool_truncated`,
`code_surface.coverage.path_symbols_truncated`, and
`code_surface.coverage.candidate_pool_status` where applicable. Existing
`query.*` handler spans and `eshu_dp_api_request_duration_seconds` record
route time. No-Observability-Change: no new metric, span, log field, queue stage,
or storage operation is introduced. Operators can distinguish a known cap
from an unknown empty-page pool status in the returned coverage.

## Classification

Correctness win for partial-result truth only. The latency hypothesis and
same-argument deployed cold/warm p95 proof for #7246 remain unresolved.
