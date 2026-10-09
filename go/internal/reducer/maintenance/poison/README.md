# Poison Liveness Runner

## Purpose

Closes the gap generation liveness cannot reach: scopes whose newest
generation is terminally dead-letter get a bounded auto-retry sweep
(#4740). Moved here from the flat `maintenance` package under #7648.

## Ownership boundary

Owns the sweep loop and the redrive accounting. Does not own
dead-letter selection or re-enqueue (the storage `Recoverer`
implementation), the stuck-gauge (wired independently in
`cmd/reducer`), or the `Service` side-runner startup loop in the
reducer root.

## Exported surface

- `Runner`, `Config` — the sweep loop and its bounds
  (`AutoRetryEnabled` gates the sweep itself)
- `Policy` — recover budget and batch limit
- `Result` — recovered count
- `Recoverer` — storage port (`RecoverPoisonDeadLetters`)

See `doc.go` for the full contract.

## Dependencies

- `internal/telemetry` — counters and log attributes
- `pkg/log` — structured error logging

Never `internal/reducer`.

## Telemetry

- Metrics: `eshu_dp_poison_liveness_recovered_total`,
  `eshu_dp_poison_liveness_failures_total{reason}`
- Spans: none; the sweep runs inline in the side-runner goroutine
- Logs: `poison dead-letter recovery cycle completed` (INFO, only
  when items recovered); `poison dead-letter recovery cycle failed`
  (`failure_class=poison_liveness_error`)

## Gotchas / invariants

- The runner MUST only re-drive a dead-letter row when
  `Config.AutoRetryEnabled` is true (default posture is
  surface-only); the stuck-gauge stays active regardless.
- A cycle that recovers work loops immediately so a backlog drains;
  an empty cycle waits a poll interval.

## Related docs

- `go/internal/reducer/recovery-runners.md`
- `docs/public/observability/telemetry-coverage.md`
