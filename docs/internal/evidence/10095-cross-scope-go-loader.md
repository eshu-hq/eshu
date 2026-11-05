# #10095 cross-scope Go evidence loader — core stage (lane-4 escalation slice)

Part of #10002. Branch `drive/10095-cross-scope-go-loader`.

## What landed

One evidence stage in `go/internal/reducer/supplychain/core`
(`go_reachability_load.go`, wired in `evidence_load.go`): every
supply-chain-impact pass loads `vulnerability.go_module_evidence` and
`vulnerability.go_call_reachability` for each repository id in its filter
through the new `ListActiveGoReachabilityFacts` seam. With the evidence in
hand, `BuildSupplyChainImpactFindings` reads the govulncheck verdict onto
the holder's copy (`not_called` on `runtime_reachability`); without a
loader the copy stays `unknown`. The CVE-alias join is covered: an
advisory keyed by its GO id with the finding keyed by its CVE alias still
reads `not_called`.

The stage no-ops in production until a loader implements the interface —
no production store does yet, so no pass gains a backend call, queue
stage, lock, or graph write from this change. The `FactStore`
implementation (`ListActiveGoReachabilityFacts` on
`fact_records_scope_generation_idx`, `(scope_id, generation_id,
fact_kind)` probe per repository) stays with lane 1, as does flipping the
live chain test green.

## Proof (all postdating the final edit, exit codes captured)

- `go test ./internal/reducer/supplychain/core/ -count=1` → ok.
- `go test ./internal/reducer/ -run 'SupplyChainImpact' -count=1` → ok.
- `go vet ./internal/reducer/supplychain/core/` → clean.
- Mutation: dropping the stage call fails the two `not_called`
  assertions (restored after).
- `EXPLAIN (ANALYZE, BUFFERS)` of the intended storage probe on the local
  `eshu` shell database: one index probe per candidate scope on the
  `(scope_id, generation_id, fact_kind)` index, no sequential scan of
  `fact_records`. Representative-data EXPLAIN belongs to the
  storage-reader half.

## Markers

No-Regression Evidence (#10095): per-pass cost on the no-loader path is
one type assertion plus a filter over the in-hand envelopes:
`BenchmarkGoReachabilityStageNoLoader` (holder seed: 3 envelopes,
1 repository id; linux/arm64, Go 1.26.10, three 10000-sample runs)
measured 5.465/4.724/4.739 ns/op with 0 B/op and 0 allocs/op. Baseline
`origin/main` (`ab6df73458`) has no cross-scope Go stage; after, this
branch HEAD, the full core package suite passes. Classification
(`BuildSupplyChainImpactFindings`) is byte-identical code before and
after; only the evidence set it reads can grow, and only once a loader
exists.

Observability Evidence (#10095): two additive signals,
`go_reachability_facts` and the `load_go_reachability` sub-duration, both
zero until a loader implements the interface. No existing signal or
duration key is renamed or removed.

## NOT_CHECKED (lane-1 halves)

- `FactStore.ListActiveGoReachabilityFacts` and its representative-data
  EXPLAIN.
- The live chain-test flip (`seedGoFenceChain` verdict assertions).
- Winners row shape and golden-corpus digest parity — need the storage
  half first (no production pass can change a verdict yet, so no digest
  can change either).
