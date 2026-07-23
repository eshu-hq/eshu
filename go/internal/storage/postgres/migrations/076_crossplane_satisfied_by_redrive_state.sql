-- Durable claim + completion tracking for the Crossplane cross-scope
-- SATISFIED_BY re-drive sweep (issue #5476). #5347 shipped the Claim -> XRD
-- correlation ungated within a scope, but a Claim resolving against an XRD
-- from a DIFFERENT (commonly platform) repo depends on that repo's XRD
-- already being active by the time the Claim's own generation runs its
-- correlation. If the XRD repo is ingested for the first time AFTER the
-- Claim repo's latest generation, the edge never materializes until the
-- Claim repo produces a new generation -- an unbounded window for a Claim
-- repo that stops changing. This table lets the sweep durably remember,
-- per XRD source-generation, whether the cross-scope Claim re-drive fan-out
-- for that generation has completed, so:
--   - a crash mid-sweep leaves the row 'claimed' with an expired
--     claim_expires_at, which a later sweep attempt reclaims and reruns
--     (the fan-out is idempotent, so a full rerun is safe);
--   - completed_at is written ONLY after every page of the fan-out has
--     committed, so a partial failure never records false completion; and
--   - re-running the sweep for an already-completed XRD generation is a
--     cheap no-op (single primary-key lookup), never duplicate work.
--
-- Mirrors graph_node_owner_backfill_state's "durable completion marker"
-- shape (migration 074) plus aws_freshness_triggers' claim/lease shape
-- (aws_freshness_sql.go) for the concurrent-sweeper convergence this table
-- adds on top: FOR UPDATE SKIP LOCKED lets multiple sweep triggers (the live
-- post-activation hook and a startup/periodic catch-up scan) claim rows
-- without blocking each other, and the expiring lease recovers a sweep whose
-- owning process died mid-fan-out.
--
-- claim_fencing_token mirrors aws_freshness_triggers' claim_fencing_token
-- (#4576): every claim (fresh or reclaim) bumps it, and MarkCompleted only
-- succeeds when the caller presents the token it was handed back at claim
-- time. claimed_by alone is NOT a safe completion fence here: every replica
-- and the periodic catch-up scanner share one static per-process-class owner
-- string (mirroring ProjectorQueue/ReducerQueue's own LeaseOwner
-- convention), so a stale invocation whose lease already expired and was
-- reclaimed by ANOTHER invocation under the SAME owner string would still
-- match a claimed_by-only fence and silently complete a claim it no longer
-- holds (a split-brain false-positive completion). The bumped token gives
-- each invocation, not each owner string, a fence that cannot collide.
CREATE TABLE IF NOT EXISTS crossplane_satisfied_by_redrive_state (
    xrd_scope_id TEXT NOT NULL,
    xrd_generation_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    claimed_by TEXT NULL,
    claimed_at TIMESTAMPTZ NULL,
    claim_expires_at TIMESTAMPTZ NULL,
    claim_fencing_token BIGINT NOT NULL DEFAULT 0,
    completed_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (xrd_scope_id, xrd_generation_id)
);

-- Backs the reclaim scan (expired 'claimed' rows) and the claim scan
-- ('queued' rows) without a full-table scan of every XRD generation ever
-- swept.
CREATE INDEX IF NOT EXISTS crossplane_satisfied_by_redrive_state_claimable_idx
    ON crossplane_satisfied_by_redrive_state (status, claim_expires_at, updated_at ASC);

-- Backs the cross-scope Claim target-discovery query
-- (listCrossplaneRedriveTargetScopesQuery): an active (non-tombstone)
-- K8sResource content_entity row's derived (group, kind) join key -- the
-- same identity ExtractCrossplaneSatisfiedByEdgeRows resolves against an
-- XRD's (spec.group, spec.claimNames.kind) -- scoped to exactly the fact
-- partition the discovery query filters on. The derived-group expression
-- mirrors reducer.crossplaneAPIVersionGroup exactly: split_part returns the
-- whole string when api_version carries no "/", so the CASE only takes the
-- split_part branch when a "/" is actually present, matching Go's
-- idx <= 0 -> "" fallback (including a leading "/", where idx == 0).
-- Partial to keep the index limited to actual Claim candidates
-- (content_entity + K8sResource) instead of every fact row.
CREATE INDEX IF NOT EXISTS fact_records_active_k8s_claim_redrive_idx
    ON fact_records (
        (CASE
            WHEN position('/' IN COALESCE(payload->'entity_metadata'->>'api_version', '')) > 0
            THEN split_part(payload->'entity_metadata'->>'api_version', '/', 1)
            ELSE ''
        END),
        (payload->'entity_metadata'->>'kind'),
        scope_id,
        generation_id,
        fact_id ASC
    )
    WHERE fact_kind = 'content_entity'
      AND source_system = 'git'
      AND is_tombstone = FALSE
      AND (payload->>'entity_type' = 'K8sResource' OR payload->>'entity_kind' = 'K8sResource');

-- Durable "already re-driven" ledger for the target-discovery query's
-- already-satisfied fence. Keyed by (target scope, XRD group, XRD claim
-- kind) -- the exact resolution identity ExtractCrossplaneSatisfiedByEdgeRows
-- joins on -- NOT by XRD scope or generation: once a target Claim scope has
-- had a re-drive chance against this (group, claim_kind) identity while some
-- XRD advertising it was active, re-running the SAME target's materialization
-- against the SAME identity is a deterministic no-op (a pure function of that
-- target's own unchanged facts plus the same active-XRD set), so it is
-- correct to skip it on every later sweep for this identity, including a
-- resync of the XRD platform repo that changes nothing about the XRD's
-- (group, claim_kind) itself. This is why the fence must NOT be keyed on the
-- XRD's own fact observed_at (the design's original, rejected shape): that
-- ingestion-time value strictly advances on every platform-repo resync even
-- when the XRD's identity is unchanged, so it never actually skips anything.
-- If the target scope is later re-ingested with a genuinely NEW Claim
-- candidate, that new generation's own projector-triggered SATISFIED_BY
-- intent resolves it directly (the XRD is already active by then) --  no
-- redrive, and therefore no ledger check, is needed for that case.
CREATE TABLE IF NOT EXISTS crossplane_satisfied_by_redrive_target_ledger (
    target_scope_id TEXT NOT NULL,
    xrd_group TEXT NOT NULL,
    xrd_claim_kind TEXT NOT NULL,
    redriven_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (target_scope_id, xrd_group, xrd_claim_kind)
);
