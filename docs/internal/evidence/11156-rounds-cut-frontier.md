# Round-cap frontier diagnostic: no-regression evidence (#11156)

The 8-round expansion cap stops a pass and leaves its scope partial. Before
this change a cut pass reported `active_evidence_loads = 8` and the rounds
cause, and nothing about the follow-up filter that was still open. The
diagnostic reads that filter once, at the cut, and reports one size per arm.

## What runs

At the cut, `newRoundsCutFrontier` reads 13 slice lengths from the follow-up
filter and stores them with the round count and the envelope count on the
truncation record. Nothing decides from them. A cut pass then writes 15
sub-signal keys and one INFO line. A pass the cap did not stop runs none of
it. There is no new query, no new instrument and no new lock.

## Measurement

Baseline: `da04c6bb34`, the PR base. The first measurement (8 runs per tree)
ran at the earlier base `5d486c52e6`; the rebase onto `da04c6bb34` touched none
of the measured files, and the table below is a second measurement at
`da04c6bb34` and at the commit that adds this note. Backend: none; the handler
runs over an in-memory loader and a recording writer, so this measures the
reducer's own work and not Postgres. Input shape: the seeded expanding fixture
(`expandingImpactLoader`), which finds one new package per round and so is cut
at round 8 on every run, with a JSON logger writing to `io.Discard` so the INFO
line is paid for. Host: shared 16-thread Linux box with other agents running,
so every run is noisy; the three rounds alternate trees to spread that.

Command, run in both trees with the same benchmark file (3 rounds of 5 runs
each, 15 runs per tree):

```text
go test ./internal/reducer/supplychain/core/ -run '^$' -bench BenchmarkHandleRoundCutPass -benchtime=3000x -count=5
```

| | baseline | after |
| --- | --- | --- |
| ns/op, minimum of 15 | 126,286 | 133,507 |
| ns/op, median of 15 | 168,130 | 172,163 |
| allocs/op | 1,201 | 1,251 |

On the minimum the cost is +7,221 ns (+5.7 percent, about 0.01 ms) on a cut
pass; on the median it is +2.4 percent, and the spread inside each tree
(126,286 to 316,223 before) is larger than either gap. The extra 50
allocations are the sub-signal map keys and the INFO line attributes. A cut
pass is about 0.13 ms in this harness; a real pass spends its time in Postgres
reads that take seconds, and the diagnostic adds no read. A pass the cap did
not stop pays one `rounds > 0` check.

Terminal counts: every run ends in one cut pass with `PartialEvidence = true`,
`evidence_truncated_rounds = 1` and the same writer input as the baseline. The
test `TestRoundsCutDiagnosticChangesNothing` compares that writer input
(findings, partial flag, stamp, causes, fencing token) byte for byte against a
golden captured on the baseline commit.

Observability Evidence: a pass the cap stopped carries `rounds_cut_rounds`,
`rounds_cut_envelopes` and `rounds_cut_frontier_<arm>` on its success line and
one INFO line, `supply chain impact evidence expansion cut at the round cap`,
with scope, generation, pass mode, cause, rounds, envelopes and one field per
arm (`TestRoundsCutReportsUnrequestedFrontier`). The telemetry reference
section "Round-cap frontier" in
`docs/public/reference/telemetry/supply-chain-active-evidence.md` says how to
read them.

No-Observability-Change: `eshu_dp_supply_chain_impact_evidence_truncated_total`,
`eshu_dp_supply_chain_impact_pass_total` and the partial-scope gauges are
unchanged; no instrument was added, so no unscoped route golden moved.

No-Regression Evidence: the table above, plus the golden that pins the writer
input.
