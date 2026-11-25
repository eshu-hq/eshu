# Sub-millisecond winners resweep timeout clamp (eshu-hq/eshu#11446)

## No-Regression Evidence: sub-ms guard values render 1 ms, never 0 ms

`formatWinnersResweepTimeout` renders a `time.Duration` for Postgres
`set_config('statement_timeout', ...)` with `%dms` of `timeout.Milliseconds()`.
A positive timeout below one millisecond used to render `"0ms"`, and Postgres
reads `statement_timeout = 0` as no timeout — so a configured guard would
silently not exist. The fix clamps positive sub-millisecond inputs up to
`1 ms` in the single rendering function all guard installs share.

Baseline (pre-fix) vs after (post-fix), hermetic unit proof on this binary
(`go test ./internal/storage/postgres/ -run 'TestWinnersResweepTimeoutClass|TestWinnersResweepStatementTimeoutDefault|TestResweepWinnersKeyedPinsStatementTimeout|TestWinnersResweepTimeoutFormatMatchesSetConfig' -count=1`): before, `500µs` rendered `"0ms"` (test FAILs as
seeded); after, all 4 tests PASS in 0.110 s package time, including the new
`500µs -> "1ms"` case and the unchanged `120s -> "120000ms"` case.

Input shape: `time.Duration` values at the renderer — `0` and negatives
still fall back to the 120 s default upstream in `resweepStatementTimeout`,
so they never reach the clamp; `1ns`…`999µs` render `"1ms"`; `1ms` and above
are byte-identical to before. Backend: hermetic Go unit proof (no database);
the guarded statement path is unchanged. Row counts: none (no statements
added, removed, or reworded; one comparison on a per-tick pin). Telemetry,
log, and status evidence: unchanged — no new metric, span, log key, queue,
or knob; the `statement_timeout` failure class already exists and still
classifies 57014 aborts exactly as before.

Why this is safe: the clamp touches one pure function with one production
caller (`pinResweepStatementTimeout`); every other timeout in the file
already uses fixed whole-millisecond constants, so no other rendering
changes. Winner output, tick phases, and the contract pin are untouched
(`git diff` is the formatter plus its unit test).

## No-Observability-Change: guard trips look exactly as before

No new instrument, label value, span, or log line. A tripped guard still
records the existing `statement_timeout` failure class on the existing
`eshu_dp_supply_chain_impact_winners_write_conflicts_total` series, and a
sub-millisecond operator setting now installs a 1 ms guard instead of
silently installing none — the observable difference is the intended fix.
