# No-Regression Evidence: scoped determinism seed (#8516)

The determinism cell seed moved from a bare-handle literal to a
repository-scoped `-workload-id` plus `SET` name/repo_id properties. The
emitted edge-key derivation gains one property read per edge.

No-Regression Evidence: `BenchmarkExpectedEdgeKey`, go1.26.6 linux/arm64,
`-benchtime=100x -count=3`, input shape two-property identity vs nil
identity: before 218.8 ns/op 120 B/op 4 allocs/op; after 276.0–294.8 ns/op
176 B/op 4 allocs/op. Delta is +~70 ns/op and +56 B/op with zero additional
allocations — sub-microsecond on a test-harness seed path, no production hot
loop affected. `go test ./internal/ifa/materializededges/ ./cmd/ifa/` green.

No-Observability-Change: no span, metric, structured log key, status field,
or pprof surface changed; no new functions on a serving path.
