# Security-Alert Scope Retirement Reads

## Purpose

`internal/storage/postgres/security/alerts/retirement` reads the
security-alert scopes the coordinator's planner must retire (#9019). A sharded
org target collects through shard scopes whose `parent_scope_id` is the
target's base `scope_id`. A scope the current partition no longer plans keeps
its last active generation, and every alert list, count, and inventory read
joins `ingestion_scopes.active_generation_id`, so the planner retires it with
an empty generation.

## Ownership boundary

This package owns the candidate read only. The planner
(`internal/coordinator/security/alert`) decides what is planned and creates
the retirement work items; the collector
(`internal/collector/securityalerts/alertruntime`) commits the empty
generation; the read state it commits is written by
`security/alerts/unread`.

## Exported surface

- `CandidatesQuery` — the exact statement, exported for the live EXPLAIN proof.
- `Reader`, `NewReader`, `Reader.RetirementCandidates` — the planner's
  `ShardScopeReader` port.

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/storage/postgres/db` — the `Queryer` and `Rows` contracts.
- `internal/storage/postgres/array` — text array arguments.

## Telemetry

None of its own. Production wires the reader over an `InstrumentedDB` with
store name `security_alert_shard_scopes`, so the read's duration and errors
appear on the Postgres store metrics and spans.

## Gotchas / invariants

- The two scope predicates combine as a BitmapOr of
  `ingestion_scopes_parent_idx` and the primary key, and the read-state join
  is a primary-key probe on `generation_id`; the live proof EXPLAINs the
  statement on more than 10k scopes.
- The read skips an active generation whose read state has
  `repositories_total = 0`: every retirement, and a collection with no
  allowlisted repository of the organization, which holds no alert fact (org
  alerts pass `RepositoryAllowed` first). The zero count is a skip condition,
  not the retirement marker; the marker is `shard.RetirementFreshnessHint`. A
  generation without a read state (before #8943) is still a candidate.
- An error is returned, never an empty list.
- Never import the parent `postgres` package.

## Related docs

- `docs/public/reference/security-intelligence-provider-alert-parity.md`
