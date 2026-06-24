// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

// ErrSemanticExtractionClaimRejected means a lifecycle mutation lost its lease
// fence or fingerprint match and did not update a row.
var ErrSemanticExtractionClaimRejected = errors.New("semantic extraction claim rejected")

// SucceedClaim persists a successful semantic extraction completion behind the
// lease fence.
func (s SemanticExtractionQueueStore) SucceedClaim(
	ctx context.Context,
	record semanticqueue.Record,
	leaseOwner string,
	now time.Time,
	responseHash string,
	budget semanticqueue.BudgetDecision,
) error {
	if s.db == nil {
		return errors.New("semantic extraction queue store db is required")
	}
	budgetMetadata, err := json.Marshal(budget)
	if err != nil {
		return fmt.Errorf("marshal semantic extraction success budget metadata: %w", err)
	}
	result, err := s.db.ExecContext(
		ctx,
		succeedSemanticQueueJobQuery,
		now.UTC(),
		responseHash,
		budgetMetadata,
		record.JobID,
		leaseOwner,
		record.Fingerprint,
	)
	if err != nil {
		return fmt.Errorf("succeed semantic extraction job: %w", err)
	}
	return semanticExtractionRowsAffected(result)
}

func semanticExtractionRowsAffected(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("semantic extraction rows affected: %w", err)
	}
	if rows == 0 {
		return ErrSemanticExtractionClaimRejected
	}
	return nil
}

const succeedSemanticQueueJobQuery = `
UPDATE semantic_extraction_jobs
SET status = 'succeeded',
    provider_job = false,
    retryable = false,
    claim_until = NULL,
    lease_owner = NULL,
    next_attempt_at = NULL,
    last_attempt_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    response_hash = $2,
    budget_metadata = $3,
    updated_at = $1
WHERE job_id = $4
  AND lease_owner = $5
  AND fingerprint = $6
`
