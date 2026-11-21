# stagedaudit — agent instructions

This package runs the staged winners audit sweep (#11131). The range and NULL
probe statements and the writer-side range verify live in
`internal/storage/postgres` (`supply_chain_impact_winners_audit_range*.go`,
`supply_chain_impact_winners_audit_sweep*.go`); the fleet, deferral reasons and
telemetry recorder live in `internal/runtime/postgres` (staged-audit files).
This package sequences one range per `AuditWinners` call.

## Invariants

- Never import the parent reducer package or `go/cmd`. The only allowed
  edges are core (interface and result types), storage/postgres (statements,
  store types), runtime/postgres (fleet, reasons, telemetry), and
  storage/postgres/db (reader surface).
- Never add SQL here. Writer contact is exactly `ReadWinnersAuditSweep` and
  `VerifyWinnersAuditRange` through the `SweepStore` interface.
- Every failure path defers with a closed reason and records it; never skip
  a range, never truncate one, never count a range as a completed audit.
- A behavior change starts from a failing test: the unit fakes in
  `tick_test.go` (scripted corpus, in-memory cursor with the compare-and-set)
  and the live proofs in `internal/storage/postgres`
  (`supply_chain_impact_winners_audit_*_live_test.go`).
- Keep one log line per range and one per completed sweep.
