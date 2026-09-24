# AGENTS.md — internal/telemetry/snapshot guidance for LLM assistants

## Read first

1. `go/internal/telemetry/snapshot/README.md` — flow, surface, concurrency
2. `go/internal/telemetry/snapshot/doc.go` — the godoc contract
3. `go/internal/telemetry/AGENTS.md` — the parent package's rules

## Invariants

- **No I/O on the scrape path.** `Source.Counts` reads the cached snapshot only.
  Never make it, or anything an observable-gauge callback calls, wait on a
  backend.
- **One refresh in flight per Source.** Do not add a second goroutine per
  Source or overlap reads to "catch up"; serializing per Source is the design,
  and Sources stay independent of each other.
- **Bounded reads.** Every `Fetch` runs under `Config.Timeout`. Do not remove
  the deadline.
- **Keep the last good snapshot on failure.** Never publish an empty or partial
  map from a failed read; a stale value with a rising age is honest, a fake zero
  is not.
- **Closed `gauge` label.** `Register` names come from the caller as constants;
  never derive one from graph data.
- **Shutdown is not a failure.** A read cancelled by the parent context is not
  counted or logged as an error.

## Common changes

- Serving another graph-backed gauge: add a `Register` call in
  `go/cmd/reducer/provenance_gauge_wiring.go` with a new closed `gauge` constant,
  and document the value in the telemetry reference.
- Changing signals: update `metrics.go`, the telemetry reference docs,
  `docs/public/observability/telemetry-coverage.md`, and the tests together.
