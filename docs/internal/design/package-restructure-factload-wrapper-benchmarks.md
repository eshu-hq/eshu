# Factload wrapper inlinability: the accepted cost and its measured wall-clock

Moved verbatim out of [package-restructure.md](package-restructure.md) (the
reducer factload hoist, #6061 PR3) to keep that document under its Markdown
line cap. The five wrappers that lose inlinability at 77 -> 94, and the
mechanism behind it, are described there.

Accepted rather than fixed, and the reason is the shape of those five: each is
a thin wrapper that immediately issues a store read, so one non-inlined call is
nanoseconds against a millisecond query. That is an argument from shape, not a
benchmark — the magnitude is not measured. `//go:noinline` on the forwarders
would restore the costs and is a hack; repointing the callers at `factload`
directly would widen this PR past its scope. The four `retryableFactLoadError`
methods also leave the `-m` output, but only because they changed package.

Measured wall-clock (#6359, closes the argument-from-shape above).
`BenchmarkLoad{CodeownersOwnership,Documentation,Rationale,ShellExec,SubmodulePin}MaterializationFacts`
(`go/internal/reducer/factload_materialization_bench_test.go`, shared
601-envelope in-memory corpus over `stubFactLoader`, `go test
./internal/reducer/ -run '^$' -bench 'MaterializationFacts|WrapperFrameOverhead'
-benchmem -count=3`), darwin/arm64 Apple M4 Pro, go1.27.0 — the same toolchain the
`-m=2` costs above were taken on, so compiler, corpus, and loader path match
the inline decision. Medians of three runs, 0 allocs/op throughout:

- codeowners 7.39 ns/op (runs 14.22 / 6.96 / 7.39; first-iteration warmup)
- documentation 6.29 ns/op (7.68 / 6.29 / 6.21)
- rationale 6.36 ns/op (7.29 / 6.36 / 6.29)
- shellexec 6.58 ns/op (6.69 / 6.58 / 6.51)
- submodule-pin 6.35 ns/op (6.35 / 6.27 / 7.44)

Isolated hoist-introduced cost (`BenchmarkFactloadWrapperFrameOverhead`,
same runs): direct `factload.LoadFactsForKinds` 5.67 ns/op versus the same
call through the thin codeowners wrapper 6.64 ns/op — the wrapper frame the
hoist added costs ~1 ns/op. The wrappers are new in this change, so no
pre-hoist wrapper baseline exists; direct-vs-wrapper is the comparable
base-vs-head delta.

One non-inlined wrapper call is ~7 ns, of which ~1 ns is the hoist-introduced
frame, against the store read it immediately issues — over three orders of
magnitude against any store round-trip — so the accepted inlinability loss is
noise on this path. The store-read magnitude itself is not measured by this
bench; it remains a shape argument (each wrapper's next step is a store read,
covered in production by `eshu_dp_postgres_query_duration_seconds`), and the
bench's claim stops at the wrapper-frame cost it actually measures.

Benchmark Evidence (follow-up #6549 review): the repro `-bench` pattern was
widened to `MaterializationFacts|WrapperFrameOverhead` so the ~1ns figure
reproduces; re-executed with both sub-benchmarks running green.
No-Observability-Change (follow-up): prose-only correction, no code touched.

Benchmark Evidence: baseline is the direct `factload.LoadFactsForKinds` call
at 5.67 ns/op (median of three runs); after (through the thin wrapper) is
6.64 ns/op, so the hoist-introduced frame costs ~1 ns/op. Backend/version:
in-memory `stubFactLoader` fallback branch (the branch a non-push-down store
takes), go1.27.0, darwin/arm64 Apple M4 Pro. Input shape: shared 601-envelope
corpus (`benchFactloadCorpusEnvelopes(100)`), `go test ./internal/reducer/
-run '^$' -bench 'MaterializationFacts|WrapperFrameOverhead' -benchmem
-count=3`, 0 allocs/op throughout. Terminal counts: every wrapper returns the
full seeded generation (pinned by `TestFactloadMaterializationWrappersReturnSeededEnvelopes`
FactID-multiset identity). Safe because the measured frame is nanoseconds
against the store read each wrapper immediately issues.

No-Observability-Change: no metric, span, log field, status field, or runtime
setting is added, removed, or renamed by the wrapper hoist or its benchmarks.
Loader reads stay covered by `eshu_dp_postgres_query_duration_seconds` and the
owning pass by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`.
