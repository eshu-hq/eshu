// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

// SkipClaimByPolicy transitions a leased semantic extraction job to a terminal
// policy-skip state behind the lease fence without ever creating provider work.
//
// The semantic-provider execution worker calls this when its claim-path egress
// re-check denies or finds no allow rule for the claimed provider profile and
// source class. The transition is fail-closed: the row becomes
// StatusSkippedPolicy, provider_job and retryable are cleared, and the lease is
// released so the row is never re-dispatched. The reason code is recorded in the
// failure_class column for redacted operator readback; no provider host,
// endpoint, URL, or credential is written.
func (s SemanticExtractionQueueStore) SkipClaimByPolicy(
	ctx context.Context,
	record semanticqueue.Record,
	leaseOwner string,
	now time.Time,
	reasonCode string,
) error {
	if s.db == nil {
		return errors.New("semantic extraction queue store db is required")
	}
	if strings.TrimSpace(leaseOwner) == "" {
		return errors.New("lease owner is required")
	}
	result, err := s.db.ExecContext(
		ctx,
		skipSemanticQueueJobByPolicyQuery,
		now.UTC(),
		strings.TrimSpace(reasonCode),
		record.JobID,
		leaseOwner,
		record.Fingerprint,
	)
	if err != nil {
		return fmt.Errorf("skip semantic extraction job by policy: %w", err)
	}
	return semanticExtractionRowsAffected(result)
}

const skipSemanticQueueJobByPolicyQuery = `
UPDATE semantic_extraction_jobs
SET status = 'skipped_policy',
    provider_job = false,
    retryable = false,
    claim_until = NULL,
    lease_owner = NULL,
    next_attempt_at = NULL,
    last_attempt_at = $1,
    failure_class = $2,
    failure_message = NULL,
    failure_details = NULL,
    updated_at = $1
WHERE job_id = $3
  AND lease_owner = $4
  AND fingerprint = $5
`
