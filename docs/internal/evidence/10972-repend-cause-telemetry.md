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

Baseline: `b3085189ad`, the branch's original base. The branch now sits on
`71781e83de`; `git diff b3085189ad 71781e83de` touches nothing under
`internal/reducer/supplychain/core` or `internal/telemetry`, so the baseline
still matches the code the branch changes. Backend: none. The handler runs over an
in-memory loader and a recording writer that reports one changed CVE, with the
registered instruments and a JSON logger writing to `io.Discard`, so this
measures the handler's own work and not Postgres. Input shape: a stamped
one-hop pass (`BenchmarkHandleStampedPassWithSignals`, the same file copied
into the baseline tree). Host: shared Linux box with other agents running, so
every run is noisy; the two trees ran in alternating rounds of 15 runs each,
two rounds per tree.

```text
go test ./internal/reducer/supplychain/core/ -run '^$' -bench BenchmarkHandleStampedPassWithSignals -benchtime=3000x -count=15
```

| | baseline | after |
| --- | --- | --- |
| ns/op, minimum of 15 (round 1 / round 2) | 13,455 / 13,787 | 16,914 / 16,557 |
| ns/op, median of 15 (round 1 / round 2) | 13,722 / 13,951 | 17,357 / 16,712 |
| B/op | 13,769 | 15,270 |
| allocs/op | 97 | 112 |

The cost is about 3 microseconds (0.003 ms) per pass, and 15 allocations, most
of it the INFO line's attributes. A real pass spends its time in Postgres reads
and writes that take 62 to 206 seconds on the QA environment, so the added
work is under one part in 10 million of a pass, and it adds no read.

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

`pass_reason` reads the loaded evidence, the stamp and two handler fields; it
adds no read. The fan-out site timing wraps four existing calls (the in-flight
read, each holder-read chunk, each holder reopen, and the state writes) with
two `time.Now` calls each, and adds one histogram sample and one span
attribute per site entered. The fan-out pass holds at most 256 holder reopens,
so the worst case is a few hundred `time.Now` calls, which is tens of
microseconds against a fan-out whose every site is a database round trip. This
cost is reasoned, not benchmarked: the benchmark above covers the handler
without a fan-out port, and the host load made a second paired run poor
evidence. `TestFanOutReportsTimeBySite` and `TestFanOutSkipsSitesItDoesNotEnter`
pin what is reported, and the existing fan-out suites pass unchanged
(`go test ./internal/reducer/supplychain/core/ ./internal/reducer/`).

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
