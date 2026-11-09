# stagedaudit — agent instructions

This package assembles the staged stale-winners audit tick (#10451). The
query builders live in `internal/storage/postgres` (batch SQL, census,
compaction, diff, ident resolution); the tick runner, conflict classifier,
member affinity, and telemetry live in `internal/runtime/postgres`
(staged-audit files). This package only wires them into the reducer core's
auditor interface.

## Invariants

- Never import the parent reducer package or `go/cmd`. The only allowed
  edges are core (interface and result types), storage/postgres (read-phase
  functions), runtime/postgres (fleet, runner, telemetry), and
  storage/postgres/db (reader surface).
- Never add writer SQL, writer pool handles, or writer-spanning transactions
  here: the only writer contact is `VerifyStagedCandidates`, after every
  read succeeds.
- A behavior change starts from a failing test through the fakes in
  `staged_audit_test.go` (scripted reader, pinned member). The scripted
  differential pins the candidate contract; extend it when the contract
  grows.
- Keep the tick's log line to one per completed or deferred tick with the
  gauges the post-deploy watch needs.
