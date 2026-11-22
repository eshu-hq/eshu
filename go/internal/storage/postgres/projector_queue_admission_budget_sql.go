// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// This file holds the admission-twin retry UPDATE (#10671), split out of
// projector_queue_sql.go to keep both files under the 500-line cap. The
// twin is byte-identical to retryProjectorWorkQuery except for the
// admission_wait_attempts increment; argument positions match one for one.

// retryProjectorWorkWithAdmissionWaitQuery is the retryProjectorWorkQuery
// twin for backpressure admission-timeout failures: besides the shared
// retry transition it increments admission_wait_attempts inside the same
// fenced UPDATE, so the admission budget is consumed exactly once per
// admission Fail on exactly the row this attempt owned.
const retryProjectorWorkWithAdmissionWaitQuery = `
UPDATE fact_work_items
SET status = 'retrying',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $5,
    next_attempt_at = $5,
    updated_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4,
    admission_wait_attempts = fact_work_items.admission_wait_attempts + 1
WHERE stage = 'projector'
  AND scope_id = $6
  AND generation_id = $7
  AND lease_owner = $8
  AND attempt_count = $9
  AND status IN ('claimed', 'running')
`

// retryProjectorWorkWithSourcePacedWaitQuery is the retryProjectorWorkQuery
// twin for per-source backpressure releases (#10882): besides the shared
// retry transition it increments source_paced_waits inside the same fenced
// UPDATE. It never touches admission_wait_attempts, so pacing cannot spend
// the admission budget a later genuine admission timeout owns.
const retryProjectorWorkWithSourcePacedWaitQuery = `
UPDATE fact_work_items
SET status = 'retrying',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $5,
    next_attempt_at = $5,
    updated_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4,
    source_paced_waits = fact_work_items.source_paced_waits + 1
WHERE stage = 'projector'
  AND scope_id = $6
  AND generation_id = $7
  AND lease_owner = $8
  AND attempt_count = $9
  AND status IN ('claimed', 'running')
`
