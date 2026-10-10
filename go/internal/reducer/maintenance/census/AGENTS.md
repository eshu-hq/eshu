# AGENTS.md — internal/reducer/maintenance/census

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Import rule

This leaf may import `internal/graph/anchor` and `internal/telemetry`. It
must **never** import the parent `internal/reducer` package, directly or
transitively.

## What must stay conservative

- `Runner` (#7212) MUST record the unreachable-nodes gauge, the
  id-bearing-nodes gauge (the pair a metrics-based rollout gate reads, since an
  empty graph also reads zero unreachable), and the last-success time only
  from a successful pass, so a failed or timed-out
  pass keeps the last good snapshot and the snapshot age grows. It runs one
  bounded AllNodesScan per pass under its own deadline and never on a scrape
  path. It takes no lease: the scan is read-only and idempotent.

## Gates that will fire on your change

- **`verify-telemetry-coverage.sh`** — `runner.go` needs its row in
  `docs/public/observability/telemetry-coverage.md`, keyed by file path.
- **`verify-dirgate.sh`** — re-derive the `internal/reducer` row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the mirror;
  never hand-edit either.
