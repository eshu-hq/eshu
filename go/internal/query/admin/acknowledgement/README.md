# Collector terminal failure acknowledgement

The operator acknowledgement of a collector terminal failure (issue #8830):
`POST /api/v0/admin/collector-failures/acknowledge` marks one failure on one
collector instance and scope as known and expected, and
`POST /api/v0/admin/collector-failures/acknowledgements/query` is the admin-only
read that returns the operator's reason.

## Ownership

This leaf owns the request contract only:

- `handler.go` - the `Handler`, route `Mount`, request validation, the admin
  (all-scopes) authorization gate, the replay idempotency ledger claim, the
  200/400/403/404/409/422/500/503 answers, the outcome counter, and the
  governance audit events.
- `outcome.go` - the encoding of a recorded acknowledgement in the ledger's
  outcome list, and the 200 body.

The acknowledgement table (migration 231), `TerminalFailureAckStore`, and the
status read that consumes the rows live in `go/internal/storage/postgres`
(`collector_terminal_failure_ack.go`, `workflow_terminal_failure_status.go`).
This leaf never writes SQL. It is constructed from the API writer in
`go/cmd/api/wiring_router.go` and mounted through the query `APIRouter`. It is a
leaf, not a route in package `admin`, because the admin tree's rule is one leaf per
route family; it follows the replay/reopen contract by using the admin Store's
ledger methods.

## Behavior

The caller names `collector_kind`, `instance_id`, `scope_id`, a `reason` of at
most 512 characters, and an `idempotency_key`. Every expected refusal happens before the ledger claim, so it leaves the
key unconsumed: an unknown, mismatched, or inactive instance (404), a scope with no
unresolved failure inside the instance's window (422
`ack_refused_no_terminal_failure`). The same key and
target replays the stored outcome (`status` duplicate); the same key with another
target answers 409. A scoped token gets 403; unauthenticated dev mode stays open.

An acknowledged instance reads `partial`, never healthy or failed, with the
failure kept as a separate `acknowledged_failure` object. A completed work item
on the scope, or a failure of another raw class, reports the failure again with
no further write. See
`docs/public/reference/http-api/collector-terminal-failure.md`.

## Telemetry and audit

- Counter `eshu_dp_collector_terminal_failure_acknowledgements_total{outcome}`:
  `acknowledged`, `duplicate`, `refused`. A server fault is not counted; it is
  on the request span and the audit event `ack_failed`.
- INFO log `collector terminal failure acknowledged` with the bounded kind,
  instance, and class.
- One `admin_recovery_action` governance audit event per decision with a bounded
  reason code (`ack_recorded`, `ack_idempotent_replay`, `ack_refused_*`,
  `ack_idempotency_key_reused`, `ack_failed`) and the hashed actor. The
  operator's reason text is never put in the audit event.
- Gauge `eshu_runtime_coordinator_collector_terminal_failure_acknowledged_instances`
  counts instances with an acknowledged failure by kind and class on the status
  scrape.
