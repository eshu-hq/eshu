# Timing Proof Rules

Detail for the timing-proof rules that the
[Agent Engineering Guide](agent-guide.md#live-gate-serialization-and-contention)
and the `eshu-performance-rigor` skill point to. Use it when a claim depends on
wall time, a ratio of wall times, or a test harness that creates and drops
databases.

The rules come from the #7127 PR-3d timing proof (arbiter rulings D and E,
2026-09-27/28). Each one names the run that broke it.

## 1. A timing proof owns the host

While a wall-time gate runs, nothing else the coordinator controls may run:
no `make pre-push`, golden run, live suite, build, lint, or extra container.
Leave only the databases under test up. Paste `docker ps` and `uptime` into the
gate file at the start and at the end of the run, and start no other gate until
the results file is closed.

A quiet host is declared and then checked. The check is rule PD:

- **Headroom.** load1 below half the CPU count at the start and at the end of
  every run. The harness reads the CPU count itself.
- **In-run maximum.** Sample load1 every second during the run and record the
  maximum. A run is valid only if the maximum is also below half the CPU count.
- **Control canary.** Each round runs the unchanged base binary next to the
  binary under test. If the base run takes longer than its bound, the round is
  invalid on both sides and is re-run. Fix the bound before the run, from the
  control's quiet-host timings (rule 2).
- **Bounded retries.** Cap the attempts per fixture. If the valid rounds do not
  arrive, report that the host could not be quieted. That is not a code result.

Why the earlier rule failed. It asked for load1 below the CPU count (below 18
on 18 CPUs), sampled at the start and end of each run. On the #7127 final-SHA
run that admitted a control run 8.5 times slower than the fastest control:
3,946 ms against 462 ms for the same 49,396-row delete, when quiet-window
controls mostly ran 436 to 732 ms. The identical
`EXPLAIN (ANALYZE, BUFFERS)` took 3.6 s at load 11 and 6.4 s at load 19 to 20
(as the run recorded it),
with byte-identical buffer counts. A one-minute load average also lags: two
runs went from load 12 to 30 and from 16.5 to 26 inside 4 to 6 seconds, and a
start and end sample cannot see a burst that the average has not yet absorbed
when the run ends. The control run measures the thing that matters, whether the
backend was starved, and it was in the results file all along.

A failed gate under load has proven nothing about the code. Do not read it as a
finding, and do not re-run it under the same conditions.

## 2. Derive the bounds from measured spread

A wall-time or ratio bound must sit outside the estimator's measured spread on
unchanged code. Take that spread from at least two prior sets on the same rigs
before the gate is written. Do not choose a round number.

Report the same-round ratios with their mean and standard deviation beside any
ratio of medians. Which estimator you pick can flip the verdict, so the reader
needs to see the spread.

The #7127 gate (ii) required the 1.54M-row median over the 771k-row median to
be at most 2.5. The number was chosen, not derived. Across the six sets taken
on the two rigs, including the final run, the same ratio ran from 2.12 to 2.61
(mean 2.39, SD 0.22), so the bound sat inside the noise it was meant to
exclude. The final run read 2.551.
Depending on the estimator the same data gave 2.551, 2.491, 2.417 or 2.511.
Ruling E withdrew the gate and replaced it with deterministic plan and buffer
checks: buffer touches per deleted row equal at both sizes within 1 percent,
identical plan node set, no temp files, and the largest in-memory CTE below
`work_mem`. The wall-time ratio is reported with its spread and never gated.

Prefer a gate the host cannot move. When the claim is "cost per row does not
grow with size", buffer counts and plan shape prove it; wall time only
corroborates.

## 3. `t.Cleanup` must not use `t.Context()` for teardown I/O

`t.Context()` is cancelled just before the functions registered with
`t.Cleanup` run (Go 1.24 and later; `go doc testing.T.Context`). A teardown
that does I/O with it gets `context.Canceled` at once. If the error is
discarded, the teardown silently does nothing.

- Build a background context with its own timeout inside the cleanup function,
  for example `context.WithTimeout(context.Background(), 30*time.Second)`.
- Report a failed teardown with `t.Errorf`. A leaked resource is a harness
  failure, not a warning.
- A `defer db.Close()` in the test body runs before `t.Cleanup` does. Teardown
  that needs a connection must own its own, not borrow the one the body closed.
- Prove the teardown with a test that runs the helper inside a subtest and
  checks the resource is gone after the subtest returns. Write it to fail
  first.

In #7127 the retention timing harness dropped its clone databases from a
`t.Cleanup` using `t.Context()`. That leaked 17 clones; the committed evidence
records 4.5 GB for the 6 on one rig and no size for the other 11. A second
cause followed it: the test body
closed its admin pool with a `defer`, which runs before `t.Cleanup`. It leaked
5 more clones (3 on one rig, 2 on the other), 22 in all. Their sizes were not
recorded.

## Sources

- [7127 ledger retention lock-hold evidence](evidence/7127-ledger-retention-lock-hold.md):
  the committed record of the control runs, the ratio sets and the clone leak.
- #7127 PR-3d, arbiter ruling D: rule PD and the control canary.
- #7127 PR-3d, arbiter ruling E: the withdrawn gate (ii), the deterministic
  replacement, and the `t.Context()` teardown leak.
- #7372: the request to write these rules down.
