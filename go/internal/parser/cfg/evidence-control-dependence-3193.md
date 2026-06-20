# Control Dependence Guard Provenance Evidence (#3193)

No-Regression Evidence: `go test ./internal/parser/cfg ./internal/parser/taint -run 'TestControlDependence|TestGuardedSink|TestNestedGuardedSink' -count=1` proves deterministic block-level control-dependence, synthetic-exit early-return handling, loop/header self-dependence avoidance, capped overflow accounting, and guarded taint finding provenance.

Benchmark Evidence: `go test ./internal/parser/cfg -bench BenchmarkBuildReaching -run '^$' -count=3` on darwin/arm64 Apple M4 Pro reported `1505780 ns/op`, `1537601 ns/op`, and `1809626 ns/op` for the existing 64-diamond CFG benchmark with control-dependence enabled inside `Build`.

No-Observability-Change: this adds no worker, queue domain, metric name, metric label, runtime knob, backend branch, retry loop, graph query, or new graph-write route. Parser performance remains visible through existing file parse timing, and reducer projection still uses the existing `code_taint_evidence` domain, writer statement metadata, executor dispatch, retry wrapping, and failure logging.
