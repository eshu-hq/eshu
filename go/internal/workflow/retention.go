// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"fmt"
	"time"
)

const (
	// defaultWorkItemRetentionAge keeps a month of terminal work-item history,
	// far longer than every status read window (the coordinator's 30-minute
	// recent-failure window and the registry status 24-hour window) and every
	// collector schedule interval, so no planner re-enqueues a deleted id.
	defaultWorkItemRetentionAge = 30 * 24 * time.Hour
	// minWorkItemRetentionAge is the shortest age Validate accepts. Two days
	// keeps every row the 24-hour registry status window counts, with a full
	// day of margin for a coordinator clock that runs ahead of the database
	// or a status read that lags its pass.
	minWorkItemRetentionAge = 48 * time.Hour
	// defaultWorkItemRetentionBatchSize is the rows one batch transaction
	// examines and may delete. The #8829 EXPLAIN on a 300k-row seed measured
	// 14 to 16 ms and 14.1k to 14.7k shared buffers per 1,000-row batch.
	defaultWorkItemRetentionBatchSize = 1000
	// maxWorkItemRetentionBatchSize caps one batch transaction so its row
	// locks stay short-lived.
	maxWorkItemRetentionBatchSize = 10000
	// defaultWorkItemRetentionMaxBatches bounds one pass to 100 batch
	// transactions (100,000 rows examined at the default batch size).
	defaultWorkItemRetentionMaxBatches = 100
	// defaultWorkItemRetentionLockTimeout bounds any lock wait inside a batch.
	// Candidate rows are taken with SKIP LOCKED, so only the cascade into
	// workflow_claims could wait, and nothing else locks a terminal item's
	// finished claims; a timeout here is an anomaly, not back-pressure.
	defaultWorkItemRetentionLockTimeout = 2 * time.Second
)

// WorkItemRetentionPolicy bounds one retention pass over workflow_work_items.
//
// A pass deletes only completed and failed_terminal rows of a closed run
// (status complete or failed) whose updated_at is older than Cutoff. It never
// deletes claimed, pending, failed_retryable, or expired rows; the newest row
// of a (collector_kind, collector_instance_id, scope_id) target; the newest
// completed row of that target, which the status registry's last_completed_at
// reads; the newest failed_terminal row of that target, which failure
// reporting reads; or the row of a scope's active generation, which collector
// readiness evidence joins.
type WorkItemRetentionPolicy struct {
	// Age is how long a terminal row is kept after its last update.
	Age time.Duration
	// BatchSize is the number of rows one batch transaction examines, in
	// primary-key order, and the most it can delete.
	BatchSize int
	// MaxBatches bounds the batch transactions of one pass. A pass that stops
	// here resumes from its cursor on the next pass.
	MaxBatches int
	// LockTimeout bounds any lock wait inside one batch transaction.
	LockTimeout time.Duration
}

// DefaultWorkItemRetentionPolicy returns the conservative documented policy:
// 30-day age, 1,000-row batches, 100 batches per pass, 2-second lock timeout.
func DefaultWorkItemRetentionPolicy() WorkItemRetentionPolicy {
	return WorkItemRetentionPolicy{
		Age:         defaultWorkItemRetentionAge,
		BatchSize:   defaultWorkItemRetentionBatchSize,
		MaxBatches:  defaultWorkItemRetentionMaxBatches,
		LockTimeout: defaultWorkItemRetentionLockTimeout,
	}
}

// Validate rejects a policy that could delete rows a status read still needs
// or hold row locks for an unbounded time.
func (p WorkItemRetentionPolicy) Validate() error {
	if p.Age < minWorkItemRetentionAge {
		return fmt.Errorf("workflow work item retention age %s must be at least %s", p.Age, minWorkItemRetentionAge)
	}
	if p.BatchSize <= 0 || p.BatchSize > maxWorkItemRetentionBatchSize {
		return fmt.Errorf("workflow work item retention batch size %d must be between 1 and %d",
			p.BatchSize, maxWorkItemRetentionBatchSize)
	}
	if p.MaxBatches <= 0 {
		return fmt.Errorf("workflow work item retention max batches %d must be positive", p.MaxBatches)
	}
	if p.LockTimeout < time.Millisecond {
		return fmt.Errorf("workflow work item retention lock timeout %s must be at least 1ms", p.LockTimeout)
	}
	return nil
}

// Cutoff returns the UTC instant before which a terminal row's updated_at
// makes it a retention candidate.
func (p WorkItemRetentionPolicy) Cutoff(asOf time.Time) time.Time {
	return asOf.UTC().Add(-p.Age)
}

// WorkItemRetentionResult reports one retention pass. A pass that fails
// mid-way still reports the batches it committed before the failure.
type WorkItemRetentionResult struct {
	// Batches is the number of committed batch transactions.
	Batches int
	// ScannedRows is the number of rows the committed batches examined.
	ScannedRows int
	// DeletedCompleted counts deleted completed rows.
	DeletedCompleted int
	// DeletedFailedTerminal counts deleted superseded failed_terminal rows.
	DeletedFailedTerminal int
	// Wrapped is true when the pass reached the end of the table; the next
	// pass starts again from the first row.
	Wrapped bool
	// BatchDurations holds each committed batch transaction's wall time,
	// which is also how long it held its row locks.
	BatchDurations []time.Duration
}

// Deleted returns the total rows the pass deleted.
func (r WorkItemRetentionResult) Deleted() int {
	return r.DeletedCompleted + r.DeletedFailedTerminal
}

// WorkItemTableSize is the catalog's view of workflow_work_items: total
// relation bytes (heap, indexes, and TOAST) and the planner's live-row
// estimate from the last ANALYZE or VACUUM.
type WorkItemTableSize struct {
	Bytes         int64
	EstimatedRows int64
}
