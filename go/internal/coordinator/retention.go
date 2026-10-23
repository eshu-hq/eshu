// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/environment"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	// defaultWorkItemRetentionInterval paces retention passes. At the default
	// policy one pass examines at most 100,000 rows, so a 5-minute cadence
	// walks a million-row table in under an hour.
	defaultWorkItemRetentionInterval = 5 * time.Minute

	workItemRetentionOutcomeSuccess = "success"
	workItemRetentionOutcomeError   = "error"
	// workItemRetentionOutcomeCanceled marks a pass stopped by coordinator
	// shutdown. Its committed batches stay committed; it is not a failure.
	workItemRetentionOutcomeCanceled = "canceled"
)

// WorkItemRetentionStore deletes retention-eligible workflow_work_items rows
// in bounded batches and reports the table's catalog size. The Postgres
// implementation is items.Sweeper.
type WorkItemRetentionStore interface {
	PruneWorkItems(context.Context, time.Time, workflow.WorkItemRetentionPolicy) (workflow.WorkItemRetentionResult, error)
	WorkItemTableSize(context.Context) (workflow.WorkItemTableSize, error)
}

// WorkItemRetentionConfig controls the workflow_work_items retention loop
// (#8829). LoadConfig turns it on by default at a 720h age; setting
// ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE=0 turns it off. Deleted
// history cannot be restored. The zero value, which tests and callers that
// build Config by hand get, is off.
type WorkItemRetentionConfig struct {
	// Enabled turns the loop on. It runs only in active deployment mode.
	// LoadConfig sets it unless the retention age is explicitly 0.
	Enabled bool
	// Interval is the time between passes.
	Interval time.Duration
	// Policy bounds each pass.
	Policy workflow.WorkItemRetentionPolicy
}

// WorkItemRetentionObservation captures one retention pass for metrics.
type WorkItemRetentionObservation struct {
	Outcome  string
	Duration time.Duration
	// Result holds the batches committed before any failure.
	Result workflow.WorkItemRetentionResult
	// TableSize is the post-pass catalog size; TableSizeKnown is false when
	// the catalog read failed.
	TableSize      workflow.WorkItemTableSize
	TableSizeKnown bool
}

// WorkItemRetentionMetrics is the optional metrics surface for the retention
// loop. Metrics implementations that do not provide it record nothing.
type WorkItemRetentionMetrics interface {
	RecordWorkItemRetention(context.Context, WorkItemRetentionObservation)
}

// loadWorkItemRetentionConfig reads the two work item retention settings.
// Retention is on by default at the documented 720h policy; an explicit age
// of 0 turns it off. Both settings are validated whether or not retention is
// on, so a bad value fails startup before an operator relies on it. The pass
// interval, batches per pass and lock timeout use the documented defaults.
func loadWorkItemRetentionConfig(getenv func(string) string) (WorkItemRetentionConfig, error) {
	policy := workflow.DefaultWorkItemRetentionPolicy()
	age, err := environment.Duration(getenv, "ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE", policy.Age)
	if err != nil {
		return WorkItemRetentionConfig{}, err
	}
	if age < 0 {
		return WorkItemRetentionConfig{}, fmt.Errorf("workflow work item retention age %s must not be negative", age)
	}
	policy.BatchSize, err = environment.Int(getenv, "ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_BATCH_SIZE", policy.BatchSize)
	if err != nil {
		return WorkItemRetentionConfig{}, err
	}
	cfg := WorkItemRetentionConfig{Interval: defaultWorkItemRetentionInterval, Policy: policy}
	if age > 0 {
		cfg.Enabled = true
		cfg.Policy.Age = age
	}
	if err := cfg.validate(); err != nil {
		return WorkItemRetentionConfig{}, err
	}
	return cfg, nil
}

func (c WorkItemRetentionConfig) validate() error {
	if c.Interval <= 0 {
		return fmt.Errorf("workflow coordinator work item retention interval must be positive")
	}
	return c.Policy.Validate()
}

// workItemRetentionActive reports whether Run should schedule retention
// passes. A zero-value config (tests and callers that build Config by hand)
// stays off.
func (s Service) workItemRetentionActive() bool {
	return s.Config.DeploymentMode == deploymentModeActive && s.Config.WorkItemRetention.Enabled
}

// checkWorkItemRetention fails Run when retention is on but has no store or
// an invalid config, instead of silently never pruning.
func (s Service) checkWorkItemRetention() error {
	if !s.workItemRetentionActive() {
		return nil
	}
	if s.WorkItemRetention == nil {
		return fmt.Errorf("workflow coordinator work item retention is enabled but no retention store is wired")
	}
	return s.Config.WorkItemRetention.validate()
}

// startWorkItemRetention starts the retention loop when retention is active
// and returns a function that stops it. The loop is one goroutine that runs a
// pass at start and then one per interval, so at most one pass is ever in
// flight; ticks that arrive during a long pass are dropped, not queued. It
// runs apart from Run's select loop so a slow pass (up to 100 batches, each
// bounded by a 30 s statement timeout) cannot delay reaping or run
// reconciliation. The returned function cancels an in-flight pass and waits
// for the loop to exit, so Run never returns with a pass still running.
func (s Service) startWorkItemRetention(parent context.Context) func() {
	if !s.workItemRetentionActive() {
		return func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(s.Config.WorkItemRetention.Interval)
		defer ticker.Stop()
		s.runWorkItemRetention(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runWorkItemRetention(ctx)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// runWorkItemRetention runs one bounded pass, then reads the table size, and
// reports both through metrics and one structured log line. A failure is
// logged at ERROR and counted with outcome=error but does not stop the
// coordinator: retention is maintenance, and the claim lifecycle, reaping and
// run reconciliation must keep running. The next tick retries the failed
// window, because the sweeper keeps its cursor before it. A pass stopped by
// coordinator shutdown is not a failure: it logs at INFO and counts
// outcome=canceled, and its committed batches stay committed.
func (s Service) runWorkItemRetention(ctx context.Context) {
	policy := s.Config.WorkItemRetention.Policy
	asOf := s.now().UTC()
	startedAt := time.Now()
	result, pruneErr := s.WorkItemRetention.PruneWorkItems(ctx, asOf, policy)
	duration := time.Since(startedAt)
	canceled := pruneErr != nil && ctx.Err() != nil

	observation := WorkItemRetentionObservation{
		Outcome:  workItemRetentionOutcomeSuccess,
		Duration: duration,
		Result:   result,
	}
	var sizeErr error
	if canceled {
		observation.Outcome = workItemRetentionOutcomeCanceled
	} else {
		var size workflow.WorkItemTableSize
		size, sizeErr = s.WorkItemRetention.WorkItemTableSize(ctx)
		observation.TableSize = size
		observation.TableSizeKnown = sizeErr == nil
		if pruneErr != nil {
			observation.Outcome = workItemRetentionOutcomeError
		}
	}
	if recorder, ok := s.Metrics.(WorkItemRetentionMetrics); ok {
		recorder.RecordWorkItemRetention(ctx, observation)
	}
	if s.Logger == nil {
		return
	}
	var longestBatch time.Duration
	for _, batch := range result.BatchDurations {
		if batch > longestBatch {
			longestBatch = batch
		}
	}
	attrs := []any{
		"outcome", observation.Outcome,
		"deleted_completed", result.DeletedCompleted,
		"deleted_failed_terminal", result.DeletedFailedTerminal,
		"scanned_rows", result.ScannedRows,
		"batches", result.Batches,
		"wrapped", result.Wrapped,
		"longest_batch_ms", longestBatch.Milliseconds(),
		"duration_ms", duration.Milliseconds(),
		"retention_age", policy.Age.String(),
		"batch_size", policy.BatchSize,
		"max_batches", policy.MaxBatches,
	}
	switch {
	case observation.TableSizeKnown:
		attrs = append(attrs, "table_bytes", observation.TableSize.Bytes,
			"table_estimated_rows", observation.TableSize.EstimatedRows)
	case sizeErr != nil:
		attrs = append(attrs, "table_size_error", sizeErr.Error())
	}
	if canceled {
		s.Logger.Info("workflow work item retention pass canceled", attrs...)
		return
	}
	if pruneErr != nil {
		s.Logger.Error("workflow work item retention pass failed", append(attrs, "error", pruneErr.Error())...)
		return
	}
	s.Logger.Info("workflow work item retention pass", attrs...)
}
