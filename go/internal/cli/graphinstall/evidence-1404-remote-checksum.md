# Remote install checksum evidence (#1404)

No-Regression Evidence: a temporary Go benchmark compared the valid remote
binary path at base `114518361458a1c2977746c26b5e148878b899c9` and candidate
`0ab38c32dfc8f32e32ee3b8291c33e8e75e897c8`. Both used Go 1.26.10 on macOS
arm64, an `httptest` server returning the same 1 MiB payload, a stub version
reader, and five samples of three source preparations. The base samples were 1.987, 1.886,
1.796, 2.140, and 2.150 ms/op (median 1.987 ms/op). The candidate samples were
1.909, 1.940, 1.830, 1.926, and 1.851 ms/op (median 1.909 ms/op). The valid
checksum path showed no regression in this small local microbenchmark; the
sample is too small to claim a speedup or characterize real network downloads.
Both revisions hash the downloaded source and then hash the inspected binary;
that work is unchanged. The candidate adds normalization and comparison of the
supplied 64-character checksum before source inspection. No graph
backend, queue, or database rows are involved: this is an inline CLI download
and validation before a backend starts.

No-Observability-Change: install remains inline and exposes failures through
its existing returned error and CLI JSON result. The checksum change adds no
metric, span, log field, status field, queue, graph write, or worker. The
package-level telemetry contract in `README.md` remains unchanged.
