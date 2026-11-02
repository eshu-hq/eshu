# AGENTS.md — internal/storage/postgres/claim/fence

## Read first

1. `doc.go` and `README.md` in this directory.
2. `docs/internal/design/9376-claim-plane-commit-fence.md`: the ruled
   design, lock order, and failure matrix.
3. `../../ingestion_claim_fence.go`: the only production caller.

## Invariants

- Never fall back from a failed pin to the in-tree `HeartbeatClaim(tx)`.
- The fence key is `(generation_id, work_item_id)`. Never compare tokens
  across work items; tokens are per item.
- `CheckDeadline` takes the Go clock the deadline was built from. Never use
  SQL `now()` (it is the transaction start time).
- `UpsertQuery` runs after the generation upsert and under the scope row
  lock. Do not move it before the scope lock; that adds a lock-order edge.
- Keep the reason set closed. A new reason is a telemetry contract change.

## Verification

```bash
cd go && go test ./internal/storage/postgres/claim/fence -race -count=1
```
