# AGENTS.md - query/admin/acknowledgement guidance for LLM assistants

## Read first

1. `go/internal/query/admin/acknowledgement/README.md` - what this leaf owns
2. `go/internal/query/admin/acknowledgement/handler.go` - the route and its
   answers
3. `go/internal/storage/postgres/collector_terminal_failure_ack.go` - the
   store and its three refusals
4. `go/internal/storage/postgres/workflow_terminal_failure_status.go` - the
   status read and the one shared `workflowTerminalFailureAckCovers` predicate
5. `go/internal/query/admin/AGENTS.md` - the family failure-answer rules
   (#7674)

## Invariants this leaf enforces

- **The operator names the failure they read.** `failure_class` is required
  and the store refuses a failure that changed class, so an acknowledgement
  never suppresses a failure the operator has not seen.
- **No raw operator text in audit events.** Events carry the bounded reason
  code and the hashed actor. The reason text lives only in the
  acknowledgement row and the status payloads.
- **Failure answers follow #7674.** The fixed `acknowledgeFailedMessage`,
  `tracing.WriteServerFailure` for 5xx, and a 499 comment-free single upsert:
  a client cancel after the upsert committed leaves the acknowledgement
  durable, and a repeat answers `duplicate`.
- **The covering rule has one definition.** The write's "already covered"
  check and both status laterals use `workflowTerminalFailureAckCovers`. Do
  not copy it into another query.
- **OpenAPI stays in lockstep.** The fragment is
  `go/internal/query/openapi/paths/status/collector_terminal_failure_acknowledgement.go`.

## Common changes and how to scope them

- **Change what an acknowledgement covers** -> change the SQL predicate and
  its live proofs (`workflow_terminal_failure_ack_live_test.go`) first, then
  the status docs and the OpenAPI text.
- **Add a refusal** -> add the store sentinel, the handler case, the audit
  reason code, the counter outcome, the OpenAPI response, and a test row.
- **Add a field to the acknowledgement** -> migration, store, status read,
  RuntimeStatus, PromotionProof, the query projections, and OpenAPI, together.
