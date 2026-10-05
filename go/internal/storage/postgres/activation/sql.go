// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

// insertObligationQuery is the one-row idempotent insert ProjectorQueue.Ack
// runs inside its transaction, after the generation is activated and before
// commit. A duplicate Ack of the same generation inserts nothing.
const insertObligationQuery = `
INSERT INTO activation_obligations (scope_id, generation_id, work_item_id)
VALUES ($1, $2, $3)
ON CONFLICT (scope_id, generation_id) DO NOTHING
`

// claimObligationQuery leases the oldest open obligation. It locks only the
// obligation row, skips rows another claimer holds, and repeats the row-self
// claimability predicate in the UPDATE so the EvalPlanQual recheck drops a
// row another claimer leased and committed between snapshot and lock. The
// lease and the clock are the database clock (clock_timestamp()), never the
// caller's. claim_token increases on every claim, so it fences every earlier
// owner, including the same owner name after a restart.
const claimObligationQuery = `
WITH eligible AS (
    SELECT scope_id, generation_id
    FROM activation_obligations AS obligation
    WHERE state = 'pending' OR (state = 'leased' AND lease_until <= clock_timestamp())
    ORDER BY created_at, scope_id, generation_id
    LIMIT 1
    FOR NO KEY UPDATE OF obligation SKIP LOCKED
)
UPDATE activation_obligations AS obligation
SET state = 'leased', lease_owner = $1,
    lease_until = clock_timestamp() + ($2::double precision * interval '1 millisecond'),
    claim_token = claim_token + 1
FROM eligible
WHERE obligation.scope_id = eligible.scope_id
  AND obligation.generation_id = eligible.generation_id
  AND (obligation.state = 'pending'
       OR (obligation.state = 'leased' AND obligation.lease_until <= clock_timestamp()))
RETURNING obligation.scope_id, obligation.generation_id, obligation.lease_owner,
    obligation.claim_token, obligation.lease_until, obligation.created_at
`

// finalizeLockTimeoutQuery bounds every lock wait inside Finalize. A scope
// row held by a long ingestion commit or Ack makes Finalize fail fast and
// retry on a later pass instead of piling up behind it.
const finalizeLockTimeoutQuery = `SELECT set_config('lock_timeout', '1s', true)`

// lockScopeQuery is Finalize's first lock. Ack, Fail and the ingestion
// commit all take the scope row before any generation, work or obligation
// row, so Finalize does the same and cannot join a wait cycle with them.
const lockScopeQuery = `
SELECT active_generation_id
FROM ingestion_scopes
WHERE scope_id = $1
FOR NO KEY UPDATE
`

// lockObligationQuery is Finalize's second lock, taken after the scope row.
const lockObligationQuery = `
SELECT claim_token, COALESCE(lease_owner, ''), state,
    COALESCE(lease_until > clock_timestamp(), FALSE)
FROM activation_obligations
WHERE scope_id = $1 AND generation_id = $2
FOR NO KEY UPDATE
`

// obsoleteObligationQuery retires an obligation whose scope moved to another
// generation. It is token- and lease-fenced like completion.
const obsoleteObligationQuery = `
UPDATE activation_obligations
SET state = 'obsolete', lease_owner = NULL, lease_until = NULL,
    finished_at = clock_timestamp()
WHERE scope_id = $1 AND generation_id = $2 AND state = 'leased'
  AND claim_token = $3 AND lease_owner = $4 AND lease_until > clock_timestamp()
`

// phaseReadyQuery checks the exact generation's own backward-evidence phase.
// Every key column is pinned to the obligation's generation, so a phase that
// another generation of the same scope published never satisfies it.
const phaseReadyQuery = `
SELECT EXISTS (
    SELECT 1 FROM graph_projection_phase_state
    WHERE scope_id = $1 AND acceptance_unit_id = $1
      AND source_run_id = $2 AND generation_id = $2
      AND keyspace = 'cross_repo_evidence'
      AND phase = 'backward_evidence_committed'
)
`

// wakeQuery makes up to WakeBatchLimit future-deferred deployment_mapping
// rows of the exact scope generation visible now. It touches only rows that
// are retrying with the backward-evidence-not-ready class and hold no lease,
// skips rows another transaction holds, and changes only the three
// visibility columns, so status, attempts, failure fields and every other
// domain, generation and scope stay as they were. The UPDATE repeats the
// row-self predicates for the EvalPlanQual recheck.
const wakeQuery = `
WITH eligible AS (
    SELECT work_item_id FROM fact_work_items AS work
    WHERE scope_id = $1 AND generation_id = $2 AND stage = 'reducer'
      AND domain = 'deployment_mapping' AND status = 'retrying'
      AND failure_class = 'cross_repo_backward_evidence_not_ready'
      AND lease_owner IS NULL AND claim_until IS NULL
      AND visible_at > clock_timestamp()
    ORDER BY work_item_id
    LIMIT 32
    FOR NO KEY UPDATE OF work SKIP LOCKED
)
UPDATE fact_work_items AS work
SET visible_at = clock_timestamp(), next_attempt_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM eligible
WHERE work.work_item_id = eligible.work_item_id
  AND work.status = 'retrying' AND work.lease_owner IS NULL AND work.claim_until IS NULL
  AND work.failure_class = 'cross_repo_backward_evidence_not_ready'
`

// remainingWorkQuery reports whether the generation still has a
// deployment_mapping handler in flight (it may have read readiness before the
// phase committed and fail later) or a waiting row the wake did not reach.
// Either keeps the obligation open.
const remainingWorkQuery = `
SELECT EXISTS (
    SELECT 1 FROM fact_work_items
    WHERE scope_id = $1 AND generation_id = $2 AND stage = 'reducer'
      AND domain = 'deployment_mapping'
      AND (status IN ('claimed', 'running')
           OR (status = 'retrying'
               AND failure_class = 'cross_repo_backward_evidence_not_ready'
               AND lease_owner IS NULL AND claim_until IS NULL
               AND visible_at > clock_timestamp()))
)
`

// stillOwnedQuery rechecks the lease on the database clock before a partial
// wake commits, so a wake never commits under an expired lease.
const stillOwnedQuery = `
SELECT EXISTS (
    SELECT 1 FROM activation_obligations
    WHERE scope_id = $1 AND generation_id = $2 AND state = 'leased'
      AND claim_token = $3 AND lease_owner = $4 AND lease_until > clock_timestamp()
)
`

// completeObligationQuery is the token-fenced completion.
const completeObligationQuery = `
UPDATE activation_obligations
SET state = 'completed', finished_at = clock_timestamp(),
    lease_owner = NULL, lease_until = NULL
WHERE scope_id = $1 AND generation_id = $2 AND state = 'leased'
  AND claim_token = $3 AND lease_owner = $4 AND lease_until > clock_timestamp()
`
