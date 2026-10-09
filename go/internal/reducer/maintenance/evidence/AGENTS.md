# AGENTS.md — internal/reducer/maintenance/evidence

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Import rule

This leaf uses the standard library only today. It must **never** import
the parent `internal/reducer` package, directly or transitively.

## What must stay conservative

- `Maintainer` MUST always release its claimed lease (the `defer` in
  `RunOnce`), even on a rebuild error.
- Keep the full-resweep design: do not replace it with per-scope dirty
  tracking without proving no change class is missed (generation flips,
  tombstones, hard deletes, FK-cascade prunes).
- The resweep stays off the hot fact-write path; never add per-row
  write cost or counter-row contention against live ingestion load.

## Gates that will fire on your change

- **`verify-telemetry-coverage.sh`** — `maintainer.go` needs a row in
  `docs/public/observability/telemetry-coverage.md`, keyed by file path;
  it carries a `No-Observability-Change` marker naming the covering
  signals, since this leaf registers no instrument of its own.
- **`verify-dirgate.sh`** — re-derive the `internal/reducer` row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the
  mirror; never hand-edit either.

## Do not

- Do not add a metric here for resweep duration; the Debug log line
  with `duration_ms` paired with the `materialized_at` watermark is the
  agreed operator signal.
