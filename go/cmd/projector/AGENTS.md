# cmd/projector Agent Rules

These rules apply only inside `go/cmd/projector/`. Root `AGENTS.md` still
controls global proof, performance, concurrency, and skill requirements.

## Read First

- `go/cmd/projector/README.md`
- `go/cmd/projector/doc.go`
- `go/cmd/projector/main.go`
- `go/cmd/projector/runtime_wiring.go`
- `go/internal/projector/README.md`
- `go/internal/runtime/README.md`
- `go/internal/storage/cypher/README.md`

## Local Invariants

- MUST treat this binary as local verification and Compose debugging support.
  Deployed source-local projection normally runs inside `eshu-ingester`.
- MUST keep `buildProjectorRuntime` as the single place that constructs
  `projector.Runtime`.
- MUST keep projector and reducer queue lease durations aligned unless the
  claim/retry design changes with proof.
- MUST keep the raw graph executor wrapped in `sourcecypher.InstrumentedExecutor`
  before canonical writing.
- MUST keep `Service.Run` responsible for claim, heartbeat, ack, retry, and
  empty-queue polling. Do not call projection methods directly from `main.go`.
- MUST keep `ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION` as a test
  fault-injection knob, not production tuning.
- MUST stop through SIGINT/SIGTERM context cancellation, not ad hoc goroutine
  kills or `os.Exit` calls.

## Change Gates

- New runtime ports MUST be added to `internal/projector`, wired in
  `buildProjectorRuntime`, and asserted in `runtime_wiring_test.go`.
- New tuning env vars MUST be parsed in config helpers, documented in the
  README, and covered by runtime wiring tests.
- Canonical writer backend changes MUST stay behind the
  `projector.CanonicalWriter` boundary and include graph-write evidence.
- Queue lease changes require coordinated ingester/reducer review and
  same-shape queue/concurrency proof.

## Focused Verification

```bash
cd go
go test ./cmd/projector -run 'Test.*Runtime|Test.*Service|Test.*Retry|Test.*Batch|Test.*Driver' -count=1
go test ./cmd/projector ./internal/projector -count=1
```
