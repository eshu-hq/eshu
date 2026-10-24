# Collector terminal failure acknowledgement

The operator acknowledgement of a collector terminal failure (issue #8830):
`POST /api/v0/admin/collector-terminal-failure-acknowledgements` marks one
failure on one collector instance and scope as known and expected.

## Ownership

This leaf owns the request contract only:

- `handler.go` - the `Handler`, route `Mount`, request validation, the admin
  (all-scopes) authorization gate, the 200/400/403/404/409/500/503 answers,
  the outcome counter, and the governance audit events.

The acknowledgement table (migration 231), the `TerminalFailureAckStore`
write, and the status read that consumes the rows live in
`go/internal/storage/postgres` (`collector_terminal_failure_ack.go`,
`workflow_terminal_failure_status.go`). This leaf never writes SQL. It is
constructed from the API writer in `go/cmd/api/wiring_router.go` and mounted
through the query `APIRouter`.

## Behavior

The caller names `instance_id`, `scope_id`, the `failure_class` it read from
collector readiness, and a `reason` of at most 512 characters. The store
refuses an unknown or inactive instance (404), a scope with no unresolved
failure (404), and a failure whose class changed since the read (409). A
repeat of a covering acknowledgement answers 200 with `duplicate` true and
keeps the first reason and actor. A scoped token gets 403; unauthenticated dev
mode stays open, mirroring replay and recover-generations.

An acknowledged failure stops degrading the instance and is not a promotion
blocker, but collector status and readiness keep the failure fields and set
`last_failure_acknowledged`, so it is never read as a healthy instance. A
completed work item on the scope, or a failure of another class, reports the
failure again with no further write. See
`docs/public/reference/http-api/collector-terminal-failure.md`.

## Telemetry and audit

- Counter `eshu_dp_collector_terminal_failure_acknowledgements_total{outcome}`:
  `recorded`, `unchanged`, `refused`, `not_found`, `class_changed`, `failed`.
- INFO log `collector terminal failure acknowledged` with the bounded kind,
  instance, and class.
- One `admin_recovery_action` governance audit event per decision with a
  bounded reason code (`collector_failure_ack_recorded`, `_unchanged`,
  `_refused_*`, `_failed`) and the hashed actor. The operator's reason text
  is never put in the audit event.
- Gauge `eshu_runtime_coordinator_collector_terminal_failure_acknowledged_instances`
  counts acknowledged instances by kind and class on the status scrape.
