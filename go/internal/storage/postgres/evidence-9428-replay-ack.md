# Q1 Replay-Ack Alignment (#9428)

Arbiter ruling 6476206689 (SATELLITE HOLDS): an identical replay of an
already-applied terminal claim mutation (complete, release, fail-retryable,
fail-terminal with matching claim ID, fencing token, and owner, and the work
item still carrying that fencing token) returns nil instead of
`ErrWorkflowClaimRejected`. A retry after server-commit/response-loss that got
`ErrWorkflowClaimRejected` would otherwise report a committed generation as
fenced away (accuracy defect in `ClaimedService`, `claimed_service.go`).
Heartbeat never acks post-terminal; every other zero-row shape stays
rejected.

## Change

`workflow_control_helpers.go` threads a `claimMutationOp` through
`execClaimMutation`, `execCompleteClaimMutation`, and
`execTerminalClaimMutation`. On zero rows affected, terminal ops classify via
two single-row primary-key reads — `workflow_claims` (status, fencing, owner,
work_item_id) then `workflow_work_items` (status, NULL-safe
`current_fencing_token`) — and return nil only when the epoch exists with
matching fencing and owner, the stored claim is bound to the mutation's work
item (crossed claim/item pairs stay rejected), the
item fencing equals the mutation token, and both statuses equal the op's
terminal pair (complete: completed/completed; release: released/pending;
fail-retryable: failed_retryable/pending; fail-terminal:
failed_terminal/failed_terminal). The reads deliberately bypass the
tenant-grant join: the original mutation already passed the grant check when
it committed, and a replay answers from the stored epoch alone.

## False-ack impossibility

Nil requires the claim row to already rest in the op's terminal status, which
only the op's own mutation statement writes, and that statement writes the
claim and item rows atomically in one statement, so the two reads cannot
straddle a torn commit. The stored claim `work_item_id` must equal the
mutation's work item — every mutation statement joins `claim.work_item_id` to
`item.work_item_id`, so only an identical-pair replay may ack. A successor epoch only moves the item fencing forward
(claim issuance bumps the token monotonically), breaking the item-fencing
equality; it leaves the claim row in its own terminal status, breaking any
other op's status equality. A concurrent identical replay racing the read
already committed the same terminal state, so acking double-applies nothing:
the mutation matched zero rows precisely because the state is already there.
Expired epochs match no op terminal status, and heartbeats never classify.
Proven by `TestWorkflowControlStoreIntegrationReplayAckConcurrentReplayVsReclaim`
(8 identical fail-retryable replays race one reclaim-plus-complete successor:
every replay returns nil or `ErrWorkflowClaimRejected`, epoch 1 stays
failed_retryable exactly once, epoch 2 completes).

## Proof

- `go test ./internal/storage/postgres/ -run
  'TestWorkflowControlStoreReplayAck|TestWorkflowControlStoreMutationSuccess'
  -count=1`: red before (identical replays rejected), green after; inverted
  fencing comparison re-fails all four replay subtests (mutation check).
- `ESHU_POSTGRES_DSN=<local pg18> go test
  ./internal/storage/postgres/ -run 'TestWorkflowControlStoreIntegrationReplayAck'
  -count=1`: 3/3 pass (terminal replays ack + heartbeat-after-terminal
  rejects on all four terminal states, replay-after-reclaim rejects with the
  successor unaffected, concurrent replay-vs-reclaim exactly-once).
- Existing stale tests (`HeartbeatClaimRejectsStaleClaim`,
  `CompleteClaimRejectsStaleClaim`, `ReleaseClaimRejectsStaleClaim`, tenant
  boundary heartbeat cases) unchanged and green: their fixtures stage no
  committed epoch, so classification finds no row and still rejects.

No-Regression Evidence: `go test ./internal/storage/postgres/ -run
'TestWorkflowControlStoreMutationSuccessIssuesNoClassificationReads'
-count=1` passes — a mutation that matches a row issues zero extra queries on
every op, so the hot path is byte-identical to before. The two fallback reads
run only on zero rows and are primary-key index scans on the existing
`workflow_claims_pkey` and `workflow_work_items_pkey` (no new index):
`EXPLAIN (ANALYZE, BUFFERS)` on Postgres 18.6 reports `Index Scan`,
`Buffers: shared hit=2`, `Execution Time: 0.023 ms` (claims) and `0.003 ms`
(work items). Conflict domain: the claimed epoch's `workflow_claims.claim_id`
and `workflow_work_items.work_item_id` rows; no worker, lease TTL, batch, or
queue-ordering change, so no worker/lease before/after counts apply.

No-Observability-Change: the rejection path carries no new logging today and
gains none; ack-vs-reject is visible to operators through the existing
collector outcomes (`CollectorRunOutcomeSuccess` vs fenced-away handling) and
the durable `workflow_claims.status` / `workflow_work_items.status` row shape
the integration tests read back. No new metric, span, log field, status field,
route, runtime knob, or backend dependency is added.
