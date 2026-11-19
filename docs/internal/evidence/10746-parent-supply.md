# #10746: the parent service supplies the NVD baseline plan inputs

Refs #10746, #10728, #10814, #10828. Part of #9209. Rulings:
[10728-baseline-pace-ruling-2026-11-16.md](../arbiter/10728-baseline-pace-ruling-2026-11-16.md)
(the seed-manifest input is deleted; the ETA is remaining records over the
trailing six-hour rate, started years only) and the 2026-11-17 arbiter
comments on the issue (two wiring sites share one mapping function and one
loader; per-year `PlannedAt` and in-flight years come from the durable plan
row; at most `max_in_flight_years` in flight; order after #10828).

## What this lane ships

The planner exists but production never filled its request fields. This lane
adds the one loader both planning sites share
(`loadNVDBaselinePlanInputs` in
`go/internal/coordinator/service_nvd_baseline.go`):
the validated `derive_nvd_baseline` section, the stored checkpoints, the
per-year in-flight verdicts and last-planned times from the plan rows, the
trip latch sampled hourly, and the per-year drain states. The in-tree tick
(`scheduleVulnerabilityIntelligenceWork`) spreads them onto
`IntelligencePlanRequest`; the activation path
(`scheduleVulnerabilityActivationDerivedScopes`, moved to
`activation_derived_scopes.go` to keep its old file under the line cap)
sets them as `ActivationDerivedPlanRequest.NVDBaseline`, and
`PlanActivationDerivedWork` mints baseline chunks through the shared
`observeNVDBaselinePlan` verdict. The `cmd` adapters
(`go/cmd/workflow-coordinator/nvd_baseline_supply.go`) map the checkpoint,
latch, drain, and sampler stores onto the coordinator ports.

## Decisions the code records

- **Fail closed, never fail the tick.** A checkpoint or plan failure zeroes
  the trip (the lane holds as `baseline_trip_unloaded` instead of replaying
  the committed prefix from zero). A trip failure keeps the load's own open
  latch (holds as `baseline_trip_open`). A drain failure leaves the year out
  of the map (holds that year as `drain_unloaded`). Every path logs a WARN
  naming the cause, so a broken baseline probe blinds the baseline lane but
  never the OSV, GHSA, or window lanes.
- **Bounded reads.** A disabled derivation reads nothing. An enabled tick
  runs one checkpoint read, one plan read, one or two latch reads, at most
  one drain probe per incomplete checkpointed year (25 at most), and at most
  one hourly dirty-log sample. No new goroutine: the tick clock is the
  sample clock.
- **Hourly sampler, tick-driven.** The loader samples when the stored sample
  is older than an hour, missing, or stamped ahead of the clock; a fresh
  latch skips the probe. A skew past the two-minute bound surfaces as a loud
  `stored_ahead`/`future_sample` hold, never silent planning. The sampler
  records the existing dirty-proxy gauge (tests pin the CVE-keyed inflow),
  the latch outcome, and one transition per trip-line flip (`opened`,
  `paged`, `eligible`).
- **ETA and not-done age.** The ETA divides the remaining records of started
  years (total reported) by the trailing six-hour done rate from the
  sampler's hourly snapshots; it records nothing until an hour of history
  exists, so a restart stays silent instead of guessing. The not-done age is
  the smallest `NotDoneSince` among the loaded drain states, reported next
  to its phase over the closed non-drained set.
- **Drain signals.** The per-tick drain load runs under the tick's deadline
  inside the `nvd baseline drain load` span; the joined error logs whole
  (years are ints, stages carry no scope id) and timeouts count apart on
  `eshu_dp_nvd_baseline_drain_probe_timeouts_total` through the
  coordinator's timeout type the `cmd` adapter maps. A moved held-year set
  logs a WARN (per-run memory on the service; a missing memory warns loudly
  instead of going quiet).
- **Config errors.** The loader logs `NVDBaselineConfigError` at WARN every
  tick it is present and returns it on the inputs; the planner holds the
  lane under `baseline_config_error`. Baseline validation stays out of the
  shared collector-configuration validator.

## Proof

Red then green in-session: the loader, sampler, drain, ETA, age,
held-change, and both-site loop tests in
`go/internal/coordinator/service_nvd_baseline_test.go`
(15 tests: the 13 loader/sampler/drain/ETA/loop proofs plus the plan-row
failure and missing-reader fail-closed pins), the inputs and timeout helpers in
`go/internal/coordinator/vulnerability/nvd_baseline_inputs_test.go`, the
three service instruments in
`go/internal/telemetry/instruments_nvd_baseline_service_test.go`, and the
seven `cmd` adapter proofs in
`go/cmd/workflow-coordinator/nvd_baseline_supply_test.go` (checkpoint,
latch, drain, sampler, and the plan-row stamp plus its failure
propagation). The loop test
runs one scheduler tick through the real planner: one hourly sample, one
minted chunk, one planned outcome. Full suites green for
`go/internal/coordinator`, `go/internal/coordinator/vulnerability`,
`go/internal/telemetry`, and `go/cmd/workflow-coordinator`.

## Open points

- **Plan rows (#10828, merged).** Migration 312 and
  `NVDBaselinePlanStore.ReadNVDBaselinePlans` are on `origin/main`; the
  `cmd` `nvdBaselinePlanReader` maps them onto the coordinator port beside
  the checkpoint reader and `main.go` wires it. The read is keyed by run
  id in one statement, so a superseded run's rows (#10831) never enter
  another run's inputs; an unreadable read zeroes the trip and holds the
  lane loudly (`baseline_trip_unloaded` plus the WARN), pinned by
  `TestNVDBaselinePlanReaderReturnsWrittenStamp`,
  `TestNVDBaselinePlanReaderPropagatesProbeFailure`,
  `TestLoadNVDBaselinePlanInputsPlanFailureFailsClosed`, and
  `TestLoadNVDBaselinePlanInputsMissingReaderHoldsLoud`.
- **Closed transitions.** `closed` and `close_refused` have no producer: no
  operator Close route calls the latch from production code. The sampler
  records `opened`, `paged`, and `eligible` only.
- **Pre-existing text fixed elsewhere.** The metrics-doc 51/52 wording and
  the behind-position seed rule with the settlement-eviction counter landed
  under #10887, so this lane does not touch them.

## No-Regression Evidence:

- Changed hot files: `go/internal/coordinator/service_nvd_baseline.go`
  (the shared plan-input loader plus the moved activation-derivation
  mapping) and `go/internal/telemetry/instruments.go` (the instruments
  struct the lane extends). The gate fires on them by content. The lane
  adds exactly one concurrency primitive and no others: the
  `NVDBaselineTickMemory.mu` leaf mutex, guarding only the in-memory
  held/progress maps, never held across I/O, awaits, or nested locks.
  There is no new `go` statement, channel, worker pool, batch-size, or
  lease knob. The per-run maps are bounded to
  `maxNVDBaselineTickMemoryRuns` retained run IDs with oldest-evicted,
  so a long-lived Service cannot grow on run IDs it does not control.
  The per-tick work is one checkpoint read, one plan read, and one or
  two latch reads, all keyed point lookups, and every failure path
  zeroes the trip or drops the year instead of retrying in a loop.
- Before/after suite timing, same machine (darwin/arm64, Go from
  `go.mod`), `-count=1`, base `b9267c2336` vs head `801df639c1`:

  | suite | base | head |
  | `internal/coordinator/vulnerability` | 1.168s | 0.407s |
  | `internal/coordinator/security/alert` | 1.130s | 0.561s |
  | `internal/coordinator/semantic` | 1.082s | 0.555s |
  | `internal/telemetry` | 3.352s | 2.633s |
  | `cmd/workflow-coordinator` | 2.658s | 2.570s |

  No suite regressed; the new loader tests run inside the existing
  packages, so their cost is included in the head column. These are
  suite wall times, not production tick latency: the production claim
  rests on the bounded-read paragraph above, and the tick that carries
  the loader already runs hourly behind the trip latch.

## Observability Evidence:

- Log: every fail-closed path logs a WARN naming the cause
  (`baseline_trip_unloaded`, `baseline_trip_open`, `drain_unloaded`),
  so a broken baseline probe blinds only the baseline lane and pages
  as the lane's own hold reason, never as a silent zero plan.
- Metric: the lane reuses the `observeNVDBaselinePlan` verdict the
  #10828 lane counts (`eshu_dp_nvd_baseline_plan_rows_total`), so the
  parent-service supply path is covered by the same counter the plan-row
  lane proves; no new label dimension is added.
- The telemetry-coverage collectors doc updated in this PR names the new
  instruments file, keeping the coverage inventory exact.
