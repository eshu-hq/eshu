# #9861 Typed File-Payload Decode Evidence

The JS/TS, Python, and JVM reachability readers consume only seven
`parsed_file_data` keys plus three top-level payload keys of each file fact.
`go/internal/reducer/supplychain/core/file_payload_typed.go` decodes only
those fields (fast struct pass; tolerant map+payloadcore fallback) and
projects the trimmed envelope the per-file bodies read. The wire-in covers
the file-fold/load path only: `foldSupplyChainImpactFilePage`,
`foldPythonReachabilityFilePage`, and `listActiveJVMReachabilityFactsPage`.

## Benchmark Evidence: typed decode vs map decode

Runner: `go test ./internal/reducer/supplychain/core/ -run NONE
-bench BenchmarkTypedFileDecodeTheory -benchmem -count=3`, backend go1.26.6
linux/arm64. Input shape: representative parser payloads WITH ballast
(source snippets, variables, symbols, tokens) at 1x=100, 5x=500, 10x=1000
files; the map side is the `payloadstore.UnmarshalPayload` production path.
Terminal counts: identical JS/TS+Python+JVM indexes and fold digests on both
sides (see `TestTypedFilePayloadDecodeParity`,
`TestTypedFilePayloadDecodeAdversarialParity`, and the scan tests in
`go/internal/storage/postgres/active_file_typed_decode_test.go`).

| Scale | Map B/op | Typed B/op | Map allocs/op | Typed allocs/op | Map ns/op | Typed ns/op |
| --- | --- | --- | --- | --- | --- | --- |
| 1x | 1,278,600 | 194,761 | 31,308 | 5,215 | 1,748,843 | 1,276,134 |
| 5x | 6,372,851 | 973,861 | 155,635 | 25,912 | 8,696,545 | 6,307,580 |
| 10x | 12,757,059 | 1,948,595 | 311,788 | 51,800 | 17,459,935 | 12,643,935 |

The typed decode allocates ~6.6x fewer bytes and ~6.0x fewer objects at every
scale, and is faster wall-clock on the same host. The change is safe because
the decoded set is exactly the key set the three readers consume (verified by
parity tests over clean and adversarial corpora), the byte accounting stays
on raw payload bytes, and decode errors happen only on malformed JSON, never
on a shape mismatch.

## No-Observability-Change: no new telemetry

No instruments, log lines, worker counts, batch sizes, queue claims, leases,
or SQL text change. The fold stats (`FoldedPayloadBytes`, `FoldedFiles`,
`payloadBytes`) keep counting raw payload bytes read, and error wrap prefixes
at each call site are unchanged, so existing dashboards and alerts observe
the same signals.
