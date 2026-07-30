# One URL Normalizer For Git Remote Keys Evidence (#5421)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## #5421 — one URL normalizer for git remote keys

Benchmark Evidence: `go test ./internal/repositoryidentity -bench
BenchmarkNormalizedRemoteKey -benchmem -count=3 -benchtime=500ms` (Apple M5 Max,
darwin/arm64, 12 representative input shapes per op): ~5,332 ns/op, 4,355 B/op,
82 allocs/op (~444 ns/input, ~6.8 allocs/input). The re-parse validation guard
(N1) added ~60% over the pre-guard shape; the SCP canonicalization through
NormalizeRemoteURL (Codex P1) added further cost by constructing an https:// URL
and calling NormalizeRemoteURL + url.Parse for SCP inputs instead of simple
string operations. This is the price of matching the collector's identity path
for SCP hints — not a hot-loop function (runs once per hint/repository pair).

No-Regression Evidence: `go test ./internal/repositoryidentity ./internal/reducer
-count=1` (39 value-pinning cases, all green); `go test ./internal/query
./internal/mcp ./internal/payloadusage -count=1` (all green); `golangci-lint run`
(0 issues); `scripts/verify-golden-corpus-gate.sh` (431 pass, 0 fail — 34s
elapsed); `scripts/verify-package-docs.sh` (present).

No-Observability-Change: no route, graph query shape, queue table, worker, lease,
runtime knob, metric instrument, metric label, span, or log key is added, removed,
or changed. `canonicalPackageSourceURLKey` is a pure in-process value function;
operators diagnose correlation outcomes through existing reducer run spans,
execution counters, and `eshu_dp_reducer_*` metrics. The normalization path
produces the same graph truth (431/0 golden gate), so existing query/MCP/test
surface observability is unaffected.
