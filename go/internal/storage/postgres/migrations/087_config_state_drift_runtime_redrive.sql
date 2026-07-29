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
-- attempt_count is the convergence bound: claimDueConfigStateDriftRedrivesQuery
-- (drift_runtime_redrive.go) only claims rows with attempt_count < the
-- caller-supplied max, so after that many attempts a row simply stops being
-- claimed -- a genuine "no config repo will ever own this backend" case
-- terminates instead of retrying forever. A row whose backend correlation
-- resolves within the bounded attempt window gets a fresh Handle() call via
-- ReplayDomain and converges on the correct evaluation.
CREATE TABLE IF NOT EXISTS config_state_drift_redrive (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    attempt_count INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    first_scheduled_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);

-- Backs the due-row scan (claimDueConfigStateDriftRedrivesQuery) without a
-- full-table scan of every scheduled redrive ever created. Proven against a
-- scratch Postgres 16 instance (issue #5593 P1-1 fix): the claim CTE's
-- Bitmap Index Scan uses this index for the next_attempt_at <= $1 bound, and
-- the attempt_count < $2 bound is applied as a cheap in-memory filter on the
-- already-narrow bitmap result.
CREATE INDEX IF NOT EXISTS config_state_drift_redrive_due_idx
    ON config_state_drift_redrive (next_attempt_at ASC);
