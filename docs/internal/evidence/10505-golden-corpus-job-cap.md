# 10505: golden corpus gate job-cap measurement

Root-Cause Evidence: `call-golden-corpus-gate / corpus-gate (neo4j)` on #10503
was cancelled at the 30-minute job cap three times (30m26s, 30m28s, 30m24s)
with every gate summary at `0 required-fail` and `The operation was canceled`
as the terminal line. The cap is `timeout-minutes: 30` in
`.github/workflows/golden-corpus-gate.yml` for the `corpus-gate` and
`differential-capture` jobs.

## Measurement

Method: check-runs API (`filter=all`, every attempt) for the head commits of
the 40 most recent PRs as of 2026-11-10 (PR range #10439–#10504). Duration
is `completed_at - started_at` per job attempt. Metric boundaries are
identical across samples (same jobs, same workflow). `n` counts listed
attempts including failures and cancels; percentiles are over successful
attempts only (p50 is the median; p90 is the element at rank
`floor(0.9 * n_success)` of the sorted successes). Rows may include
duplicate listings of one attempt from pagination; an independent
deduped-by-check-run-id rerun reproduced every min, p50, and max exactly,
with p90 within 0.5m and cancel counts one higher (window had shifted).

| Job | n | Success min | p50 | p90 | max | Cap cancels |
| --- | --- | --- | --- | --- | --- | --- |
| corpus-gate (neo4j) | 38 | 21.3m | 25.4m | 28.0m | 28.4m | 12 at ~30.4m |
| differential capture pair 1 (neo4j) | 39 | 20.1m | 24.0m | 26.1m | 29.7m | 13 at ~30.4m |
| differential capture pair 2 (neo4j) | 38 | 22.6m | 26.8m | 28.8m | 29.6m | 8 at ~30.4m |
| differential nornicdb vs neo4j | 39 | 1.3m | 1.6m | 1.8m | 2.0m | 0 |

The differential comparison failures (0.4–0.8m) are downstream of a
cap-cancelled or failed capture (for example `matrix result is cancelled`),
not comparison findings. Its 15-minute budget has 7x headroom and does
not change.

## Decision

Align the cap to the measured distribution instead of sharding or duration
work: `timeout-minutes: 30` becomes `45` for `corpus-gate` and
`differential-capture`.

- Successful runs already reach 29.7m; the cap sits inside the success
  distribution, so cancels are runner-variance losses, not hangs.
- 45m is 1.5x the observed success maximum and 1.7–1.9x the per-job p50s. The
  cancelled tail is censored, but cancelled logs show late-stage legs
  (suppression perf, final drains), so the needed tail is minutes, not tens
  of minutes.
- A genuinely hung job burns at most 15 extra minutes. Each cap cancel
  already burns a full 30-minute rerun, so headroom is the cheaper failure
  mode at the observed ~30% cancel rate.
- No gate assertion, required correlation, or snapshot tolerance changes.
  This is a capacity alignment, not a duration improvement claim.

## Verification

- `bash scripts/test-verify-golden-corpus-gate.sh` (static mirror contract).
- CI on the PR that carries this change: `corpus-gate (neo4j)` and both
  differential captures complete inside the new budget.
