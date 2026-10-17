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
- **`verify-dirgate.sh`** — no ledger row to re-derive. A move or shrink needs no
  dirgate action; growth of a directory past max(40, its count at the base) needs a
  `Dirgate-Growth:` trailer. Never edit the write-once baselines.

## Do not

- Do not move a `Service`-level "starts side runner" wiring test into
  this leaf. `Service` is root-owned; that proof stays in
  `internal/reducer` beside the other `TestServiceStarts*` tests.
