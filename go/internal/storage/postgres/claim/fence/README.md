# Claim-plane commit fence

This package fences fact commits made under a claim that the extracted
claim plane (`eshu-workflow`) holds, instead of a claim row in core
Postgres. It is the core consumption side of
`ValidateAndPinClaimForCommit` (issue #9376, step 9 of the #8875 flip
ruling).

## Why it exists

The in-tree commit path runs `HeartbeatClaim` on the same transaction as
fact persistence, so the core claim row stays locked until `COMMIT` and a
reaped writer cannot commit. A claim held in another database cannot share
that transaction. Without a replacement, a writer that lost its claim
(reaped, then reclaimed at a higher fencing token) could still commit facts.

## How the plane path is fenced

1. `Pin` before `BEGIN`: the plane pins `(claim, token)` on the item's
   planned identity until `now + lease`. Stale or unknown claims are
   terminal; transport failures, including an attempt that hits its
   `Retry.AttemptTimeout`, retry three times in total with identical
   arguments, then fail retryable as `pin_unavailable`. There is no
   fallback to the in-tree heartbeat.
2. `Keeper` re-pins every `lease/3` while the transaction runs, each call
   bounded the same way. A stale re-pin cancels the commit context; a
   failed re-pin is left to step 4. `Keeper.Stop` cancels a re-pin in
   flight before it waits, so a stalled plane cannot hold a decided commit
   open; the store stops the keeper as soon as `COMMIT` returns.
3. `Write` upserts one `generation_commit_fences` row per
   `(generation_id, work_item_id)`: accepted when the stored token is lower,
   or equal under the same claim (the ambiguous-timeout replay); refused
   otherwise.
4. `CheckDeadline` runs right before `COMMIT` on the store's Go clock:
   the latest pin deadline must exceed now by the margin (5 s by default).

## Rejection reasons

`stale_fence`, `not_found`, `fence_token`, and `fence_claim_mismatch`
are terminal. `deadline` and `pin_unavailable` are retryable.
`fence_claim_mismatch` should never happen, because the plane enforces
`UNIQUE (work_item_id, fencing_token)`. Treat one as an integrity alarm.

## Ownership

The ingestion store in the parent package owns the transaction, the lock
order, and the telemetry. This package owns the protocol steps and the
fence SQL. The design and the arbiter ruling are in
`docs/internal/design/9376-claim-plane-commit-fence.md`.
