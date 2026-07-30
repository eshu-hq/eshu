-- Bounded redrive ledger for the config_state_drift runtime delta-trigger
-- (issue #5593). ConfigStateDriftRuntimeTrigger fires when a
-- state_snapshot:* scope generation activates via ProjectorQueue.Ack, which
-- can race a first-time config+backend repo pair: the config-side repo that
-- owns this backend may not have synced and activated its own
-- terraform_backends parser fact yet. When that happens
-- TerraformConfigStateDriftHandler.Handle returns Succeeded with a
-- non-fatal "no config repo owns this backend" rejection -- a durably
-- terminal reducer work item nothing else revisits, because the reducer
-- queue's (scope, generation, domain) ON CONFLICT DO NOTHING fence makes a
-- later re-enqueue of the SAME (scope_id, generation_id) a silent no-op.
--
-- A row is scheduled ONLY when the handler actually observes that rejection
-- (reducer.TerraformConfigStateDriftHandler.Redrive, issue #5593 P1-A) --
-- not unconditionally on every runtime-triggered activation -- so this table
-- holds one row per rejection needing a retry, not one row per activation.
--
-- This table lets a periodic catch-up (go/cmd/ingester's
-- config_state_drift_redrive_catchup.go) durably remember, per
-- (scope_id, generation_id), how many times it has reopened that
-- generation's config_state_drift work item via ReducerQueue.ReplayDomain
-- and when the next attempt is due. Unlike
-- crossplane_satisfied_by_redrive_state (migration 076), this ledger needs
-- no claim/lease/fencing-token machinery: the unit of work per row is one
-- fast, already-idempotent ReplayDomain UPDATE (a single WHERE status =
-- 'succeeded' statement, not an unbounded paged fan-out), so a crash between
-- claiming a row and calling ReplayDomain for it merely wastes that one
-- bounded attempt -- self-healing on the row's next scheduled attempt --
-- rather than requiring crash-safe resumption of an in-flight multi-page
-- sweep.
--
-- attempt_count is the convergence bound, and DELETION is the growth bound
-- (issue #5593 P1-B): drift_runtime_redrive.go's
-- claimAndAdvanceConfigStateDriftRedrivesQuery claims and advances a row
-- while its NEXT attempt would stay under the caller-supplied max;
-- claimAndDeleteExhaustedConfigStateDriftRedrivesQuery claims AND DELETES a
-- row on its LAST allowed attempt. Without the delete, an exhausted row's
-- next_attempt_at stays frozen in the past and would permanently
-- re-satisfy the due-row scan's index condition on every tick for the life
-- of the deployment, relying only on the non-indexed attempt_count filter
-- to skip it. With the delete, every row EnsureScheduled creates is
-- guaranteed to be gone within exactly maxAttempts claims of it, so
-- steady-state table size is bounded by recent rejection volume, not
-- unbounded accumulation. A genuine "no config repo will ever own this
-- backend" case terminates (stops being claimed AND stops existing); a row
-- whose backend correlation resolves within the bounded attempt window gets
-- a fresh Handle() call via ReplayDomain and converges on the correct
-- evaluation before it is ever deleted.
CREATE TABLE IF NOT EXISTS config_state_drift_redrive (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    attempt_count INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    first_scheduled_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);

-- Backs the due-row scan (both claim queries above) without a full-table
-- scan of every scheduled redrive ever created. Proven against a scratch
-- Postgres 16 instance (issue #5593 P1-1/P1-B): each claim CTE's Bitmap
-- Index Scan uses this index for the next_attempt_at <= $1 bound, the
-- attempt_count comparison is applied as a cheap in-memory filter on the
-- already-narrow bitmap result, and a maxAttempts=3 row is fully deleted
-- from the table after exactly 3 claims (attempt_count 0->1, 1->2, then
-- claimed+deleted at 2) -- proving both the bound and the cleanup in one
-- trace. Also proven concurrently against real Postgres (issue #5593 P1-C,
-- drift_runtime_redrive_live_test.go): two genuinely concurrent ClaimDue
-- callers racing the same due row converge on exactly one winner, for both
-- the advance and the delete-on-exhaustion path.
CREATE INDEX IF NOT EXISTS config_state_drift_redrive_due_idx
    ON config_state_drift_redrive (next_attempt_at ASC);
