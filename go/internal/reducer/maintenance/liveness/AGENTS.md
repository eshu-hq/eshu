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
- **`verify-dirgate.sh`** — re-derive the `internal/reducer` row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the
  mirror; never hand-edit either.

## Do not

- Do not move a `Service`-level "starts side runner" wiring test into
  this leaf. `Service` is root-owned; that proof stays in
  `internal/reducer` beside the other `TestServiceStarts*` tests.
