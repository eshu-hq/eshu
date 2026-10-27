# 9275: phase timings for the freshness link and gate G4

Refs #9275. Gate G4 (the 600-key link, at most 100 ms on one sample) failed on
two hosted runs at 130.9 ms and 181.6 ms. Over 100 hosted samples of
`scheduled-freshness-b` the median is 13.30 ms, p95 28.22 ms and the maximum
181.64 ms; both outliers passed on retry on the same head. The cause of the tail
is not proven. This change adds the evidence that will prove it. It does not
move the gate.

## What changed

- `LinkResult.Phases` (`LinkPhases`) splits `Duration` into `EnsureCursor`,
  `Begin`, `Locks`, `Statement`, `Advance` and `Commit`. They are laid end to end
  on one monotonic clock, so their sum never exceeds `Duration`.
- The reducer's `changed-since link` log line carries them as
  `phase_*_seconds` next to `duration_seconds`.
- `TestLinkStatementPlanShape` keeps the 100 ms bound on one sample. It logs one
  diagnostic line and appends it to its failure: `samples=1`, the phases, the
  Postgres settings (`fsync`, `synchronous_commit`, `shared_buffers`), a
  durable-commit control (five single-row autocommit inserts into a scratch
  table, taken just before the link and never gated), the WAL and checkpointer
  movement, and the runner's CPU model, load and pressure where `/proc` has them.
  The control table is a regular table on purpose: a temporary or unlogged table
  writes no WAL, so its commits never wait for a flush.
- `TestG4GateStillFailsAt100msPlus` pins the bound: exactly 100 ms passes, and
  100 ms plus 1 ns fails. With the bound raised to 150 ms it fails on the 101 ms
  and 100 ms plus 1 ns rows.

## Performance impact

No-Regression Evidence: the change adds about a dozen `time.Now` calls to a link
that takes milliseconds. Baseline is `origin/main` 8b97b2e01d; after is this
branch. Both were run through `LinkNext` on the in-memory scripted database
(`go test -bench`, 20,000 iterations, six interleaved pairs, a busy host with
load average 33 to 78, so the wall time is noisy). Allocations are unchanged at
70 per link and bytes move from 2392 to 2440 per link. The fastest runs were
2721 ns/op before and 2194 ns/op after, so the cost is below this host's noise
and four orders of magnitude under the 5.2 ms live link. Backend: PostgreSQL
18.6 (private local cluster, default settings) for the live sample below. Input
shape: the G4 fixture, 3.8M state rows over 800 scopes, a 600-key scope with 6
delta rows. Terminal rows: the link kind is incremental and the gate's plan
assertions pass unchanged.

Local, non-hosted sample (an arm64 laptop, not a hosted runner, so it says
nothing about the hosted tail): 5.72 ms total, with `ensure_cursor` 216 us,
`begin` 120 us, `locks` 1.50 ms, `statement` 3.48 ms, `advance` 212 us and
`commit` 195 us. The control inserts took 190 to 690 us each.

## Observability

Observability Evidence: the phases are log fields on the existing
`changed-since link` line, so an operator reading a slow link sees which phase
grew: a long `commit` points at WAL flush latency, a long `statement` at the SQL
or its buffers, a long `begin` or `locks` at the connection or a lock wait.
`TestLinkLogCarriesPhaseTimings` pins the fields. No metric, span or label was
added, so the telemetry registry and the metrics reference are unchanged;
`docs/public/observability/telemetry-coverage.md` names the new log fields.

## Next step

Collect the diagnostic line from the hosted job over one scheduled cycle and
the PR attempts, then decide with at least 30 samples. Do not claim a cause
before the control line is read next to the phases.
