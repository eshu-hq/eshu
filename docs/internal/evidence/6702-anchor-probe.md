# #6702 — bounded instrumented anchor probe (H1 load race vs H2 writer variance)

## What this is

PR #6734 landed the tier tie-break guard and the baseline anchor assertion.
What remains open on #6702 is the upstream cause of the missing evidence:
H1 (consumer-load race through the documented #5709 residual windows) versus
H2 (writer variance leaving every identity row tier C). Per
Prove-The-Theory-First, no production behavior changes until a bounded,
instrumented probe separates the two.

This note tracks the probe itself:
`supply_chain_anchor_probe_6702`, emitted from
`go/internal/reducer/supplychain/core` on the commit path (`impact.go`,
after findings are built) and the deferral path (`evidence_load.go`,
before returning the readiness deferral). Each emission captures, for the
pinned CVE-2026-00010 digest only: identity row count by anchor tier,
consensus winner tier and repository, floor armed flag, batch-wide producer
delta, and the finding's own anchor, classified as `healthy`,
`h1a_batch_disarm`, `h1b_no_floor`, `h2_all_tier_c`, `wrong_winner`,
`evidence_absent_armed`, or `unexpected_tier`.

Fail-closed suppression matching is untouched (`scope.go` not in this
change). No floor, tier, consensus, write, retry, or status-transition
behavior changes. No reducer naming moves.

## Cost bound

Ordinary generations pay one linear scan over already-loaded envelopes
(FactKind plus digest string compare, no allocations beyond the result
struct) and emit nothing. Generations carrying the pinned digest pay one
additional consensus fold — the same `bestSupplyChainImageIdentitiesByDigest`
pass `Handle` already computes while building findings, so at most twice
the consensus cost on rare pinned generations, zero extra consensus
otherwise. The consensus fold is skipped entirely when no pinned-digest row
is present.

## No-Regression Evidence:

Same-machine sequential runs, Intel i7-8700K, Go 1.27.1, quiet host (no
live gate running), identical metric boundaries (ns/op, B/op, allocs/op on
the same benchmark inputs):

```
# baseline: origin/main cb5ed69f1
BenchmarkBestSupplyChainImageIdentitiesByDigestConsensus-12  100  1537240 ns/op  1005861 B/op  10073 allocs/op
BenchmarkBestSupplyChainImageIdentitiesByDigestConsensus-12  100  1492825 ns/op  1005525 B/op  10072 allocs/op
BenchmarkBestSupplyChainImageIdentitiesByDigestConsensus-12  100  1496613 ns/op  1005598 B/op  10072 allocs/op
BenchmarkBuildSupplyChainImpactIndexWithQuarantine-12        100  11975175 ns/op  7900664 B/op  100158 allocs/op
BenchmarkBuildSupplyChainImpactIndexWithQuarantine-12        100  12062400 ns/op  7900665 B/op  100158 allocs/op

# after: fix/6702-anchor-upstream-probe afd64b7e3
BenchmarkBestSupplyChainImageIdentitiesByDigestConsensus-12  100  1532639 ns/op  1005748 B/op  10073 allocs/op
BenchmarkBestSupplyChainImageIdentitiesByDigestConsensus-12  100  1494744 ns/op  1005567 B/op  10073 allocs/op
BenchmarkBestSupplyChainImageIdentitiesByDigestConsensus-12  100  1534028 ns/op  1005597 B/op  10072 allocs/op
BenchmarkBuildSupplyChainImpactIndexWithQuarantine-12        100  11911890 ns/op  7900724 B/op  100158 allocs/op
BenchmarkBuildSupplyChainImpactIndexWithQuarantine-12        100  11836201 ns/op  7900665 B/op  100158 allocs/op
```

Identical allocation counts, wall time within run-to-run noise (after is
nominally faster on the index bench, which is noise, not a claim). No
backend involved (pure in-memory reducer pass, no NornicDB/Postgres in this
path). Terminal queue and row counts unchanged: the probe writes no facts,
enqueues nothing, and alters no status transition — `go test
./internal/reducer/supplychain/core/ -count=1` green before and after
(0.07s). The change is safe because it only reads envelopes already in
hand and appends at most one log line per pinned generation.

Limits: unit-scale comparison only, single connection, no contention arm.
Live corpus-gate cost (log volume per pinned generation) is bounded by
design — one line per pass touching the pinned digest — and will be
observed on the bounded gate batch that consumes this probe.

## Observability Evidence:

New operator signal: one structured log line
`supply_chain_anchor_probe_6702` per pass touching the pinned digest or
finding, carrying fixed low-cardinality keys (`probe_class`, `digest`,
`cve_id`, `floor_armed`, `batch_producer_hits`, `deferred`,
`identity_rows`, `tier_a/b/c`, `winner_found/tier/repository`,
`finding_present/repository/digest`) plus the standard domain/scope/
generation attributes shared with the existing readiness-deferral line. No
UIDs or high-cardinality values. Nil-logger safe; silent when the pass
touches neither the pinned digest nor the finding. No new metric: existing
`eshu_dp_*` signals unchanged.

`ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash
scripts/verify-telemetry-coverage.sh` passes (no new untracked stages; the
emission lives in existing files, which the static verifier does not flag
as new stages). Deferral-path emissions reuse the handler's existing
`Logger` seam, already wired through `cmd/reducer` for the #5709 floor.

## Next step

Run the bounded instrumented gate batch with this probe until the next red
(or the agreed batch bound); pass-vs-fail comparison of the probe classes
decides H1a vs H1b vs H2. The upstream fix lands separately; this probe is
re-evaluated (kept as a diagnostic or removed) in that change.

Refs #6702
