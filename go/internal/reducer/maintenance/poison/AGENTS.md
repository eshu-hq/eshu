# AGENTS.md — internal/reducer/maintenance/poison

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Import rule

This leaf may import `internal/telemetry` and `pkg/log`. It must
**never** import the parent `internal/reducer` package, directly or
transitively.

## What must stay conservative

- `Runner` MUST only re-drive a dead-letter row when
  `Config.AutoRetryEnabled` is true. The stuck-gauge reporting the
  poison class size is wired independently in `cmd/reducer` and MUST
  remain active regardless of this flag.
- Re-drive stays bounded by `Policy.MaxRecoverAttempts` and
  `Policy.BatchLimit` so a genuinely poison item cannot loop forever.

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
