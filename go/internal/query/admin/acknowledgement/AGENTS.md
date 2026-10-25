# AGENTS.md - query/admin/acknowledgement guidance for LLM assistants

## Read first

1. `go/internal/query/admin/acknowledgement/README.md` - what this leaf owns
2. `go/internal/query/admin/acknowledgement/handler.go` - the routes and their
   answers (`refusals.go` holds the store-refusal answers)
3. `go/internal/storage/postgres/collector_terminal_failure_ack.go` - the store
   and its refusals
4. `go/internal/storage/postgres/workflow_terminal_failure_status.go` - the status
   read and the one shared `workflowTerminalFailureAckCovers` predicate
5. `go/internal/query/admin/AGENTS.md` - the family failure-answer rules (#7674)

## Invariants this leaf enforces

- **Refuse before the claim.** Every expected refusal (validation, auth,
  unknown instance, nothing to acknowledge) happens before
  `ClaimReplayIdempotency`, so the key stays unconsumed. After the claim only a
  store fault or a resolve race can fail, and then the key is consumed: the 422
  from the in-transaction re-resolve says so (`answerStoreError` `claimed`). The
  404 from the same re-resolve (instance disabled or deactivated meanwhile)
  consumes the key too, and its body says so (`key_consumed`, `key_detail`) while
  keeping the standard `error` and `detail`.
- **The ledger answers retries.** A completed key replays its stored outcome
  (`encodeOutcome`/`decodeOutcome`); the same key with another target is 409.
  Do not add a second dedupe path.
- **No raw operator text in audit, status, or metrics.** Events carry the bounded
  reason code and the hashed actor. The reason text lives in the acknowledgement
  row and the admin query route only.
- **Failure answers follow #7674.** Fixed message constants and
  `tracing.WriteServerFailure` for 5xx. The comment beside each mutation call
  says what may already have committed when it fails.
- **The list is the status read, unrolled per scope.** `ListTerminalFailures` uses the
  same window, `workflowTerminalFailureResolvedBy`, and `workflowTerminalFailureAckCovers`
  as the status read; the live set-equality test pins it. Never give status a scope id
  (it reaches callers outside the admin gate) and never put the reason in the list.
- **The covering rule has one definition,** `workflowTerminalFailureAckCovers`,
  in `storage/postgres`. Do not copy it here.
- **OpenAPI stays in lockstep.** The fragment is
  `go/internal/query/openapi/paths/status/collector_terminal_failure_acknowledgement.go`.

## Common changes and how to scope them

- **Change what an acknowledgement covers** -> the SQL predicate and its live
  proofs (`workflow_terminal_failure_ack_live_test.go`) first, then the docs.
- **Add a refusal** -> the store sentinel, the handler case, the audit reason
  code, the OpenAPI response, and a test row.
- **Add a field to the acknowledgement** -> migration, store, status read,
  `RuntimeStatus.AcknowledgedFailure`, the projections, and OpenAPI, together.
