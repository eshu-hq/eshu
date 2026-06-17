# Collector Parity Harness

## Purpose

`parity` is a fixture-to-runtime parity harness for claim-driven collectors. It
runs representative fixture fact emissions through the **real**
`collector.ClaimedService` claim/commit path and verifies them against the same
reducer readback contract hosted collectors must satisfy. It exists so collector
growth has a repeatable, machine-checkable proof that fixture facts actually
reach the expected reducer/readback contract — not only that they parse — and so
that a fixture-only lane is never mistaken for live readiness.

The harness uses only in-memory fixtures and stubs. It never needs
live-provider credentials, a database, or a graph backend.

## Ownership boundary

Owns the in-memory scenario model, the readback contract model, and the harness
that drives `collector.ClaimedService`. It does **not** own claim execution
(that is `internal/collector`), durable persistence (`internal/storage/postgres`),
or domain-specific reducer admission rules (`internal/reducer`). The readback
model here is intentionally domain-agnostic: it enforces only the universal
guarantees (stable-key idempotency, fencing supersede, withheld evidence) that
hold for every claim-driven collector.

## Exported surface

- `Harness` / `New` — drives one scenario per claim attempt against a shared,
  stateful readback model; reuse one harness across attempts to model duplicate
  delivery, stale generations, and dead-letter replay.
- `Scenario` / `NewScenario` / `WithFacts` / `Expecting` — build a consistent
  scope/generation/work-item triple plus behavior and expectation.
- `FixtureFact` / `AdmissibleFact` / `PermissionHiddenFact` / `UnsupportedFact` —
  fixture facts tagged with their expected admission class.
- `Expectation` / `Result` — the declared contract and the observed outcome,
  including claim outcome, dead-letter state, replay completion, and readable
  fact kinds. `Result.Err` reports contract failures.
- `TerminalError` — wraps an error so the claim routes to terminal failure.
- `FixtureReadiness` / `Summarize` — aggregate results into a per-collector
  readiness verdict that feeds the per-collector promotion proof report.

## Dependencies

- `internal/collector` — the real `ClaimedService`, committer, and dead-letter
  contracts the harness drives.
- `internal/workflow` — work item, claim, and mutation types.
- `internal/facts`, `internal/scope` — fact envelope and scope identity.

## Telemetry

The harness emits no metrics, spans, or logs. It is test/proof infrastructure.

## Gotchas / invariants

- One `Scenario` is one claim attempt. Multi-attempt sequences run multiple
  scenarios against one `Harness`; the readback model and dead-letter log are
  shared across the harness lifetime.
- A failed commit records **no** readable facts (atomic rollback). The harness
  asserts this so a fixture lane cannot appear ready after a commit failure.
- Stale-generation rejection is keyed on the fact fencing token. Give a fresher
  generation a higher token than the one it should supersede.
- Permission-hidden and unsupported facts are committed but never readable. The
  harness models the contract, not any one collector's reason codes.
- Keep this package free of live credentials, network access, and database or
  graph imports. Adding any of those breaks the fixture-only guarantee.

## Related docs

- `docs/public/reference/collector-reducer-readiness.md` — readiness states and
  promotion proof.
- `go/internal/collector/README.md` — claim-driven collector execution.
- `go/internal/reducer/README.md` — domain admission and readback.

## Evidence

`No-Regression Evidence:` in-memory harness; drives the real `ClaimedService`
with stub stores and asserts claim/commit/readback contracts.
`go test ./internal/collector/parity -count=1`.
`No-Observability-Change:` emits no telemetry; verifies existing collector
claim/commit/dead-letter behavior only.
