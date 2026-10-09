# Generation Liveness Runner

## Purpose

Self-healing path for the generation lifecycle: detects active
generations wedged past their activation deadline and re-drives them
through projector re-enqueue, superseding orphaned older actives. Moved
here from the flat `maintenance` package under #7648.

## Ownership boundary

Owns the sweep loop, the redrive/skip accounting, and the per-recovery
logging. Does not own candidate selection or locking (the storage
`Recoverer` implementation), the projector re-enqueue it triggers, or
the `Service` side-runner startup loop in the reducer root.

## Exported surface

- `Runner`, `Config` — the sweep loop and its bounds
- `Policy` — activation deadline, recover budget, batch limit,
  progress window
- `Result`, `Recovery` — sweep summary and per-generation redrives
- `Recoverer` — storage port (`RecoverWedgedGenerations`)

See `doc.go` for the full contract.

## Dependencies

- `internal/telemetry` — counters and log attributes
- `pkg/log` — structured error logging

Never `internal/reducer`.

## Telemetry

- Metrics: `eshu_dp_generation_liveness_recovered_total`,
  `eshu_dp_generation_liveness_superseded_total`,
  `eshu_dp_generation_liveness_failures_total{reason}`
- Spans: none; the sweep runs inline in the side-runner goroutine
- Logs: per-redrive INFO (`generation liveness re-drove wedged
  generation` with scope, generation, attempts, the bounded
  `no_intent_progress_within_window` reason, and the effective
  progress window); cycle completed/failed lines

## Gotchas / invariants

- The runner MUST log each re-driven generation and MUST NOT log
  skipped draining generations individually (#7265).
- A cycle that recovers or supersedes work loops immediately so a
  backlog drains; an empty cycle waits a poll interval.

## Related docs

- `go/internal/reducer/recovery-runners.md`
- `docs/public/observability/telemetry-coverage.md`
