# Repository retirement trigger

The operator-driven repository retirement path (issues #8602, #7766):
`POST /api/v0/admin/repository-retirements` resolves repository selectors,
then runs the tombstone mark, the phase-1 fence, and the narrowed purge in
that order.

## Ownership

This leaf owns the operator sequencing only:

- `handler.go` — the `Handler`, route `Mount`, selector resolution, auth
  gate, and the 202/400/403/409/500/503 answers.
- `trigger.go` — the `Trigger` orchestrator with its `Marker`, `Fencer`,
  and `Purger` ports, the outcome counter, and the audit events.

The tombstone store, the phase-1 fence, and the narrowed purge live in
`go/internal/storage/postgres` (#7766 foundation). This leaf never writes
SQL. The trigger is the single production caller of `RetirementFencer` and
`RetirementPurger`; both are constructed from the API writer in
`go/cmd/api/wiring_router.go` and mounted through the query `APIRouter`.

## Behavior

One request retires 1 to 25 repositories, all resolved before anything is
marked. `reason` and `idempotency_key` are mandatory. A scoped token gets
403; unauthenticated dev mode stays open, mirroring replay and
recover-generations.

Per repository the trigger emits `accepted` when the tombstone mark commits
and exactly one terminal outcome after the fence and purge steps:
`completed`, `blocked` (a designed purge exit that wrote nothing —
`terminal_not_reached` or `scope_lock_busy` — with the fenced tombstone
left open for a later pass), `refused` (a mark conflict or fence refusal,
before any purge), or `failed`. The counter is
`eshu_dp_repository_retirements_total{outcome}`.

A re-POST with the same idempotency key resumes the open retirement and
reports `duplicate: true`. The mark, fence, and purge each commit
atomically, so a client cancel between steps leaves resumable state, never
a stranded key.

## Telemetry and audit

- Counter: `eshu_dp_repository_retirements_total{outcome}` (this trigger).
- Logs: `repository retirement refused at fence` (WARN); the purge's own
  `repository retirement purged` (INFO) and `repository retirement blocked:
  terminal generations not reached` (WARN) lines.
- Audit: `admin_retirement_action` events for accepted, refused, blocked,
  completed, failed, and denied inputs. Raw reasons and keys never reach a
  tombstone row, a log, or an audit event — only their safe hashes do.

## What this leaf does not do

No dry-run preview, no candidates listing, and no readback GET: those ride
the follow-up after this trigger. No graph retraction and no background
runner: the narrowed purge covers Postgres rows only (Deliverable 1
scope), and the reducer-owned graph steps are Deliverable 2.
