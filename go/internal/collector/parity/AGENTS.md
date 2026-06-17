# AGENTS.md - internal/collector/parity guidance

## Read first

1. `README.md`
2. `doc.go`
3. `harness.go`
4. `readback.go`
5. `go/internal/collector/claimed_service.go`
6. `docs/public/reference/collector-reducer-readiness.md`

## Invariants this package enforces

- The harness must drive the real `collector.ClaimedService`. Do not
  reimplement claim/commit/dead-letter routing; that would let the proof drift
  from production behavior.
- The package must stay credential-free and offline: no live providers, no
  database, no graph backend, no network. Adding any of these breaks the
  fixture-only guarantee that makes the harness safe to run anywhere.
- The readback model stays domain-agnostic. It enforces only the universal
  contract (stable-key idempotency, fencing supersede, withheld permission-
  hidden/unsupported evidence). Domain-specific admission rules belong in
  `internal/reducer`, not here.
- A failed commit must record no readable facts. Keep the committer stub's
  atomic-rollback behavior.

## Common changes and how to scope them

- Add a scenario behavior only when it maps to a real claim/commit path in
  `claimed_service.go`; cite the path in the test.
- Extend `Expectation`/`Result` when a new contract dimension needs asserting;
  keep fields deterministic and credential-free.
- Add a readiness aggregation only when the promotion proof report needs it.

## Failure modes and how to debug

- A non-deterministic test usually means a real timer (heartbeat) fired; keep
  `HeartbeatInterval` well above the scenario's wall time.
- A leaked goroutine means the run context was not cancelled; the control-store
  stub must cancel once the work item is exhausted.
- A credential, URL, or payload appearing in a `Result` or blocker string is a
  privacy bug — fixtures and contract strings must stay safe to share.

## Anti-patterns specific to this package

- Asserting on internal reducer rows instead of the readback contract.
- Driving more than one claim attempt from a single `Scenario`.
- Importing `internal/storage`, `internal/reducer` writers, or graph packages.
