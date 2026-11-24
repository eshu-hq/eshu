# Re-pend cause and pass outcome: no-regression evidence (#10972)

The QA environment re-runs `supply_chain_impact` passes in a loop, and until
now nothing said why a pass ran or whether it changed output. This change adds
that attribution. It observes only.

## What runs

After the writer commit, `writeFindingsWithSuppressionDiff` calls
`emitPassOutcome` (`repend_cause.go`). It reads six fields of the intent the
claim already loaded (attempt count, `fanout_remaining`, `fanout_origin`,
`fanout_owed`, `EnqueuedAt`, `CycleStartedAt`) and the write result's
`ChangedCVEIDs` and `TouchedGroupIDs`, then adds one counter point, sets three
span attributes and writes one INFO line. There is no new query, claim, ack,
lock or write, and no change to the order of the existing stages. The one
signature change is a `loaded` parameter on the write wrapper.

## Measurement

Baseline: `origin/main` at `9fa8f6049a`, a throwaway detached worktree with
only `repend_cause_bench_test.go` copied in. After: this branch (the code is identical at `3b0c2056bd` and its tip) on
the same base. Backend: none. The handler runs over an in-memory loader and a
recording writer that reports one changed CVE, with the registered instruments
and a JSON logger writing to `io.Discard`, so this measures the handler's own
work and not Postgres. Input shape: a stamped one-hop pass
(`BenchmarkHandleStampedPassWithSignals`). The benchmark passes no fan-out
port, so it measures the pass-outcome signals and not the fan-out site timers.
Host: shared Linux box with other agents running, so every run is noisy; the
two trees ran in alternating rounds of 15 runs each, two rounds per tree, with
`nice -n 19` and `GOMAXPROCS=2`.

```text
go test ./internal/reducer/supplychain/core/ -run '^$' -bench BenchmarkHandleStampedPassWithSignals -benchtime=3000x -count=15
```

| | baseline | after |
| --- | --- | --- |
| ns/op, minimum of 15 (round 1 / round 2) | 14,357 / 16,649 | 21,479 / 17,320 |
| ns/op, median of 15 (round 1 / round 2) | 14,708 / 18,519 | 24,131 / 19,131 |
| B/op | 14,170 | 15,977 |
| allocs/op | 102 | 120 (119 in one round) |

The time numbers moved by more between rounds of the same tree on the after
tree (21,479 to 17,320 ns, 4,159 ns) than the signals add, so read the stable columns: the
signals add about 1.8 KB and 17 to 18 allocations per pass, and on the
best-case minimum across both rounds about 3 microseconds (14,357 to 17,320
ns). A real pass spends its time in Postgres reads and writes that take 62 to
206 seconds on the QA environment, so the added work is under one part in 10
million of a pass, and it adds no read.

The fan-out site timers are not in that benchmark, so their cost is argued from
construction: each site entry takes two `time.Now` calls and one map add, the
timer is a map of at most six keys allocated once per fan-out, and `finish`
makes at most six histogram records and span attributes. The fan-out pass
holds at most 256 holder reopens, so the worst case is a few hundred `time.Now`
calls, tens of microseconds, against a fan-out whose every site is a database
round trip. This is reasoned, not measured.

Terminal counts: the pass's writer input, result and fan-out are unchanged.
`TestPassOutcomeCounterIsBoundedAcrossManyScopesAndCVEs` runs 60 passes with 60
scope, generation, intent and CVE ids and records exactly 60 points in a
bounded series set whose label keys are the five documented ones.

Observability Evidence: `eshu_dp_supply_chain_impact_pass_outcome_total`
(`domain`, `mode`, `repend_cause`, `changed`, `change_class`), the
`supply chain impact pass complete` INFO line and the `repend_cause`, `changed`
and `change_class` span attributes agree for the same pass
(`TestPassOutcomeSignalsCarryCauseChangedAndClassOnEverySurface`). Each cause
derives from its payload shape (`TestRependCauseOfDerivesEachCauseFromItsPayloadShape`),
and a failed write records nothing (`TestPassOutcomeIsSilentWhenTheWriteFails`).
The reference is "Re-pend cause and pass outcome" in
`docs/public/reference/telemetry/supply-chain-active-evidence.md`.

No-Regression Evidence: the table above, plus the unchanged reducer-root and
core package suites.

## Pass reason and fan-out site timing

`pass_reason` reads the loaded evidence and the stamp; it adds no read. The
fan-out site timing credits the D4b load-end read's own measured duration and
wraps five existing calls (the in-flight read, each holder-read chunk, the
queued-holder prefilter, each holder reopen, and the state writes), with its
cost argued from construction in the measurement section above.
`TestFanOutReportsTimeBySite` and `TestFanOutSkipsSitesItDoesNotEnter` pin what
is reported, and the existing fan-out suites pass unchanged
(`go test ./internal/reducer/supplychain/core/`).

Observability Evidence (additions): `pass_reason` on the counter, the
pass-complete log line and the pass span
(`TestPassReasonOfSplitsTheFullMode`,
`TestPassOutcomeCarriesTheScopedAndNoSeedReasons`);
`eshu_dp_supply_chain_impact_fanout_site_duration_seconds` and the
`<site>_seconds` span attributes on `supply_chain_impact.fanout`.

## Limits

- `engaged_protection` is not seen in the QA environment; it is kept for other
  stores. There is no `unknown` pass reason: every pass lands in exactly one of
  the six checks.

- `repend_cause="fanout_rerun"` covers both a reopen of a succeeded item and
  the replay of an item that was in flight when the stamp landed. Their
  payloads are identical. Telling them apart needs the reopen statement to
  stamp the prior status, which is a SQL change and out of scope here.
- `change_class` is `unknown` for every changed pass. Separating an
  enrichment-only change from another needs the stored row's fields; the
  writer's digest read returns only the digest.
- The counter fires after the write, so a pass whose write fails is counted by
  `eshu_dp_supply_chain_impact_pass_total` and not by this counter.
