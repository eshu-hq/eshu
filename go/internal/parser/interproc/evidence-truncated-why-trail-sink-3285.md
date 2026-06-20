# Truncated Why Trail Sink Evidence (#3285)

No-Regression Evidence: `go test ./internal/parser/interproc -run TestFindingTrailKeepsSinkWhenTruncated -count=1 -v` proves a source-to-sink path longer than the 64-port why-trail cap keeps the real terminal sink as the final retained port while preserving the bounded trail length and `TrailTruncated` flag.

Benchmark Evidence: `go test ./internal/parser/interproc -count=1` on darwin/arm64 Apple M4 Pro exercised the solver package after the cap fix. The hot-path change is one in-place assignment on an already allocated bounded trail slice only after the cap is reached; it does not add allocation, graph writes, queue rows, Cypher queries, or worker fanout.

No-Observability-Change: this changes only in-memory evidence trail retention for findings that already set `TrailTruncated`. It adds no worker, queue domain, metric, span, log field, status surface, backend branch, retry loop, graph query, emitted row count, or graph-write route.
