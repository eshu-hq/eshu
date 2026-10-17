# AGENTS.md — internal/reducer/maintenance/liveness

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Import rule

This leaf may import `internal/telemetry` and `pkg/log`. It must
**never** import the parent `internal/reducer` package, directly or
transitively.

## What must stay conservative

- `Runner` MUST log each re-driven generation from `Result.Recoveries`
  (scope, generation, attempts, the bounded
  `no_intent_progress_within_window` reason, the effective progress
  window) and MUST NOT log skipped draining generations individually;
  the `draining` gauge bucket is their signal (#7265).
- Candidate selection and locking stay in the storage implementation;
  this leaf only receives the re-driven rows back for logging.

## Gates that will fire on your change

- **`verify-telemetry-coverage.sh`** — `runner.go` needs its row in
  `docs/public/observability/telemetry-coverage.md`, keyed by file path.
- **`verify-dirgate.sh`** — no ledger row to re-derive. A move or shrink needs no
  dirgate action; growth of a directory past max(40, its count at the base) needs a
  `Dirgate-Growth:` trailer. Never edit the write-once baselines.

## Do not

- Do not move a `Service`-level "starts side runner" wiring test into
  this leaf. `Service` is root-owned; that proof stays in
  `internal/reducer` beside the other `TestServiceStarts*` tests.
