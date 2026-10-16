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
- **`verify-dirgate.sh`** — no ledger row to re-derive. A move or shrink needs no
  dirgate action; growth of a directory past max(40, its count at the base) needs a
  `Dirgate-Growth:` trailer. Never edit the write-once baselines.

## Do not

- Do not add a metric here for resweep duration; the Debug log line
  with `duration_ms` paired with the `materialized_at` watermark is the
  agreed operator signal.
