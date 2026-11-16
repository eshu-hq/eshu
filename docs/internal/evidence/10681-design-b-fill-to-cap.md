# #10681 design-B probe bound (S2 fill-to-cap, K=100)

The UNMET re-arbitration verdict (2026-11-16) requires design B: bound probe
work independently of arm-3 mass, bias preserved. The theory lane compared
S0 (shipped control), S1 (EXISTS-to-cap, REJECTED: deterministic >10 s
timeouts, planner flips to unbounded filter scan), and S2 (arm-3 COUNT over
LIMIT 101, >100 reports the cap else exact, RECOMMENDED). This lane ships
S2 in `supply_chain_impact_fanout_mass.go` from the two single sources
`supplychaincore.HolderMassProbeCap` and
`supplychaincore.HolderMassProbeFillToCapRows`, keeping cheap arms exact
and unbounded, UNION ALL, LEAST total, and zero-fill.

## Performance Evidence (#10681 design B):

Baseline then after, same production-mass data, EXPLAIN ANALYZE BUFFERS
(theory lane, read-only, recorded on #10681): S2-cold5 98 ms vs S0 412 ms
(4.2x); independent re-run S2 196 ms vs S0 1536 ms (7.8x, S0 degrades cold
while S2 holds ~100-200 ms). Worst observed 98-196 ms, far under the 5 s
verdict bar. Input shape: the proven-hot N=2 pair from the split ladder
(CVE-2026-44496, CVE-2022-3517), n=100 sequential executions of the shipped
S0 probe on a warm cache, 10 s statement timeout each, zero timeouts, zero
errors (p50 127 ms, empirical p99 177 ms, one 2.5 s cold outlier; S0
numbers, not S2). Honest scope: warm
cache plus pg_stat post-deploy max 9.26-9.90 s over ~1000-4000 production
calls (cold/IO-driven tail); true-cold cache, 5-hot chunks, member mass
above 6996, other K values, and post-deploy pg_stat are NOT_CHECKED and
need the deploy + two-stable-reads watch the verdict demands before
closure. Why safe: the heap fetch per id is bounded by LIMIT 101 (S0 arm-3
walked ~26k rows/id); the residual tail is capped cold IO that fails
closed to retry, never to a blind pass.

## No-Regression Evidence (#10681 design B):

- `TestHolderMassProbeQueryIsFillToCapBound` fails on the old template
  (quotes `LIMIT 10001`, no CASE verb) and passes after; it also pins the
  planner-stability guard (no EXISTS, exactly one LIMIT so cheap arms stay
  uncapped) and the 2-LEFT-JOIN zero-fill shape.
- `TestHolderMassProbeFillToCapDecidesLikeUncapped` pins decision
  equivalence vs S0 on zero/cold/boundary/hot/huge/mixed chunks, the one
  accepted mid-band deviation (101st..threshold member row reads hot,
  split-safe), and a full-band sweep proving the fill never underestimates
  and never loses a hot id.
- Pre-existing gates still green unchanged: capped-shape,
  single-source-cap, core `TestCappedMassesDecideLikeRawMasses`,
  `TestCapBoundaryKeepsHotAloneAndColdPacked`, and the constants guard
  (now also pinning K=100 and K far under the hot threshold).
- The shipped query text carries LEAST/CASE/LIMIT/ON-gate values
  10001/100/10001/101/10001, matching the measured `/tmp/probe10681` S2
  statement line-for-line apart from leading/trailing blank lines
  (verified by rendering `HolderMassProbeQuery` and diffing).

## No-Observability-Change (#10681 design B):

No new metric, span, log key, label, worker, lease, or runtime knob. The
probe still runs through `InstrumentedDB` (`eshu_dp_postgres_query_duration_seconds`)
and the existing fan-out span attributes, summary log line, and
`fanout_holder_statements` / `fanout_hot_ids` / `fanout_max_id_mass_rows`
result sub-signals; failures still count `read_error`/`read_timeout` on the
existing counter with the `holders` site. An operator watches the
post-deploy tail (max vs the 5 s bar, estimate-site timeouts/DLs) on the
surfaces they already watch.

## Residual risk

Cheap-arm COUNT is unbounded by design (capping it needs an arbiter call);
an arm-3-only bound cannot move cheap-arm tails, and the verdict's two
stable reads must confirm max under 5 s with zero estimate timeouts/DLs.
A member count in (100, 10000] now reads hot and splits alone: extra
statements, each far under the guard, by the safe bias.
