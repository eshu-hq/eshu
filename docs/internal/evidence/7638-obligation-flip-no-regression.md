# #7638 default flip: no-regression and observability evidence

The flip commit defaults the activation obligation consumer on. This note
records why the flip carries no new regression surface and what telemetry
changes for the operator.

## Scope

Only the boolean default of `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED`
changes (false to true). Lease (30m), poll interval (10s), workers (1), the
claim SQL, and the Ack write path are identical before and after.

## No-Regression Evidence (#7638 flip):

- Baseline: origin/main with the consumer explicitly enabled.
- After: the flip head with the env unset (default on).
- Input shape: `go test ./cmd/reducer/ ./internal/envregistry/` plus
  `go test -race` on both packages; `go vet` clean.
- Result: both suites ok; the 20 builder tests that assumed blank-means-off
  now pin the consumer off through one shared helper.
- Corpus behavior banked remotely: 30m-lease full-corpus pass 3/3
  completions (item-2); reopen and DISTINCT ON statements 1.8ms/167buf and
  8.0ms/505buf at 62 scopes (item-5); single-repo completion with zero
  refusals (item-9).
- Why safe: no statement shape or knob value changed, so the flip adds no
  new regression surface beyond the default itself.

## Observability Evidence (#7638 flip):

With the consumer on, the reducer emits the pre-existing
`eshu_dp_activation_obligations{status}`,
`eshu_dp_activation_obligation_oldest_open_age_seconds`,
`eshu_dp_activation_obligation_finalize_total{outcome}`,
`eshu_dp_activation_obligation_woken_total`,
`eshu_dp_activation_obligation_maintenance_duration_seconds{outcome}`, and
`eshu_dp_activation_obligation_failures_total{reason}` gauges (see
`docs/public/services/resolution-engine.md`). While the consumer is off,
that telemetry is absent; query `activation_obligations` directly instead.
