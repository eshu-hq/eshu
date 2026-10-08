// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

// insertProducerObligationQuery is the one-row idempotent write
// ProjectorQueue.Ack runs inside its transaction, after the generation is
// activated and before commit, for every activated generation. The settle
// retires generations without producer evidence as inapplicable. A first
// activation inserts a pending row. A re-activation of a generation whose
// row is obsolete owes it again. completed and inapplicable stay terminal:
// the generation's dependents were already reopened, or it carried no
// producer
// evidence, and the generation's facts are unchanged.
//
// Lock order: Ack already holds the scope row when this statement locks the
// conflicting obligation row, which ON CONFLICT DO UPDATE does even when its
// WHERE skips the update. The producer settle takes the same
// scope-then-obligation order, Claim skips locked rows, so this adds no wait
// cycle.
const insertProducerObligationQuery = `
INSERT INTO producer_activation_obligations (scope_id, generation_id, work_item_id)
VALUES ($1, $2, $3)
ON CONFLICT (scope_id, generation_id) DO UPDATE
SET state = 'pending', finished_at = NULL, lease_owner = NULL, lease_until = NULL,
    created_at = clock_timestamp(), work_item_id = EXCLUDED.work_item_id
WHERE producer_activation_obligations.state = 'obsolete'
`

// claimProducerObligationQuery leases the oldest open producer obligation. It
// locks only the obligation row, skips rows another claimer holds, and
// repeats the row-self claimability predicate in the UPDATE so the
// EvalPlanQual recheck drops a row another claimer leased and committed
// between snapshot and lock. The lease and the clock are the database clock
// (clock_timestamp()), never the caller's. claim_token increases on every
// claim, so it fences every earlier owner, including the same owner name
// after a restart.
const claimProducerObligationQuery = `
WITH eligible AS (
    SELECT scope_id, generation_id
    FROM producer_activation_obligations AS obligation
    WHERE state = 'pending' OR (state = 'leased' AND lease_until <= clock_timestamp())
    ORDER BY created_at, scope_id, generation_id
    LIMIT 1
    FOR NO KEY UPDATE OF obligation SKIP LOCKED
)
UPDATE producer_activation_obligations AS obligation
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

// lockProducerObligationQuery is the settle's second lock, taken after the
// scope row (lockScopeQuery, shared with Finalize).
const lockProducerObligationQuery = `
SELECT claim_token, COALESCE(lease_owner, ''), state,
    COALESCE(lease_until > clock_timestamp(), FALSE)
FROM producer_activation_obligations
WHERE scope_id = $1 AND generation_id = $2
FOR NO KEY UPDATE
`

// obsoleteProducerObligationQuery retires an obligation whose scope moved to
// another generation. It is token- and lease-fenced like completion.
const obsoleteProducerObligationQuery = `
UPDATE producer_activation_obligations
SET state = 'obsolete', lease_owner = NULL, lease_until = NULL,
    finished_at = clock_timestamp()
WHERE scope_id = $1 AND generation_id = $2 AND state = 'leased'
  AND claim_token = $3 AND lease_owner = $4 AND lease_until > clock_timestamp()
`

// inapplicableProducerObligationQuery retires an obligation whose generation
// carries no producer evidence at settle time (the normal case for
// non-producer generations; tombstoned-between-Ack-and-settle facts retire
// the same way). It is token- and lease-fenced like completion.
// Prune never deletes an inapplicable row (only the generation cascade
// does), so catch-up cannot owe it again.
const inapplicableProducerObligationQuery = `
UPDATE producer_activation_obligations
SET state = 'inapplicable', lease_owner = NULL, lease_until = NULL,
    finished_at = clock_timestamp()
WHERE scope_id = $1 AND generation_id = $2 AND state = 'leased'
  AND claim_token = $3 AND lease_owner = $4 AND lease_until > clock_timestamp()
`

// stillOwnedProducerQuery rechecks the lease on the database clock before a
// reopen commits, so a reopen never commits under an expired lease.
const stillOwnedProducerQuery = `
SELECT EXISTS (
    SELECT 1 FROM producer_activation_obligations
    WHERE scope_id = $1 AND generation_id = $2 AND state = 'leased'
      AND claim_token = $3 AND lease_owner = $4 AND lease_until > clock_timestamp()
)
`

// completeProducerObligationQuery is the token-fenced completion.
const completeProducerObligationQuery = `
UPDATE producer_activation_obligations
SET state = 'completed', finished_at = clock_timestamp(),
    lease_owner = NULL, lease_until = NULL
WHERE scope_id = $1 AND generation_id = $2 AND state = 'leased'
  AND claim_token = $3 AND lease_owner = $4 AND lease_until > clock_timestamp()
`
