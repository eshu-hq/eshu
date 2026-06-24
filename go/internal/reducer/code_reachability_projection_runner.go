// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultCodeReachabilityPollInterval = 5 * time.Second
	// defaultCodeReachabilityConcurrency bounds how many disjoint
	// (scope, generation, repository) partitions the runner projects at once.
	// The conflict domain is provably disjoint per partition (PK-scoped
	// DELETE+INSERT), so the default adds throughput without contention while
	// staying modest for the Postgres connection pool. Override via Config.
	defaultCodeReachabilityConcurrency = 4
)

// CodeReachabilityInputLoader loads bounded code reachability projection
// snapshots that are newer than the currently materialized read model.
type CodeReachabilityInputLoader interface {
	LoadPendingCodeReachabilityInputs(ctx context.Context, limit int) ([]CodeReachabilityProjectionInput, error)
}

// CodeReachabilityRowWriter writes materialized code reachability rows.
type CodeReachabilityRowWriter interface {
	ReplaceRepositoryRows(
		ctx context.Context,
		scopeID string,
		generationID string,
		repositoryID string,
		rows []CodeReachabilityRow,
		watermark time.Time,
		truncated bool,
	) error
}

// CodeReachabilityProjectionRunnerConfig configures the code reachability
// read-model runner.
type CodeReachabilityProjectionRunnerConfig struct {
	PollInterval time.Duration
	BatchLimit   int
	MaxDepth     int
	// Concurrency bounds how many disjoint conflict partitions project at once.
	// Zero selects defaultCodeReachabilityConcurrency (clamped to the host CPU
	// count). Inputs that share a (scope, generation, repository) conflict key
	// always run sequentially within one partition worker.
	Concurrency int
	// MaxVisited bounds the distinct reachable entities materialized per
	// snapshot. Zero selects defaultCodeReachabilityMaxVisited.
	MaxVisited int
}

// CodeReachabilityProjectionResult summarizes one runner cycle.
type CodeReachabilityProjectionResult struct {
	InputsProcessed int
	RowsWritten     int
	// SnapshotsTruncated counts snapshots whose traversal hit the MaxVisited
	// bound; the dead-code query falls back to the legacy lookup for entities
	// omitted from a truncated slice.
	SnapshotsTruncated int
	DurationSeconds    float64
}

// CodeReachabilityProjectionRunner maintains code_reachability_rows from the
// active code-call projection read model.
type CodeReachabilityProjectionRunner struct {
	InputLoader CodeReachabilityInputLoader
	RowWriter   CodeReachabilityRowWriter
	Config      CodeReachabilityProjectionRunnerConfig
	Wait        func(context.Context, time.Duration) error
	Logger      *slog.Logger
}

// Run drains code reachability projection work until the context is canceled.
func (r *CodeReachabilityProjectionRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		result, err := r.ProcessOnce(ctx, time.Now().UTC())
		if err != nil {
			return err
		}
		if result.InputsProcessed > 0 {
			continue
		}
		if err := r.wait(ctx, r.pollInterval()); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wait for code reachability projection work: %w", err)
		}
	}
}

// ProcessOnce processes one bounded batch of pending reachability snapshots.
func (r *CodeReachabilityProjectionRunner) ProcessOnce(
	ctx context.Context,
	now time.Time,
) (CodeReachabilityProjectionResult, error) {
	start := time.Now()
	inputs, err := r.InputLoader.LoadPendingCodeReachabilityInputs(ctx, r.batchLimit())
	if err != nil {
		return CodeReachabilityProjectionResult{}, fmt.Errorf("load code reachability inputs: %w", err)
	}
	if len(inputs) == 0 {
		return CodeReachabilityProjectionResult{DurationSeconds: time.Since(start).Seconds()}, nil
	}

	partitions := r.partitionInputsByConflictKey(inputs, now)
	totalRows, truncated, err := r.projectPartitions(ctx, partitions)
	if err != nil {
		return CodeReachabilityProjectionResult{}, err
	}

	result := CodeReachabilityProjectionResult{
		InputsProcessed:    len(inputs),
		RowsWritten:        int(totalRows),
		SnapshotsTruncated: int(truncated),
		DurationSeconds:    time.Since(start).Seconds(),
	}
	if r.Logger != nil {
		r.Logger.Info(
			"code reachability projection completed",
			slog.Int("input_count", result.InputsProcessed),
			slog.Int("row_count", result.RowsWritten),
			slog.Int("partition_count", len(partitions)),
			slog.Int("concurrency", r.concurrency()),
			slog.Int("snapshots_truncated", result.SnapshotsTruncated),
			slog.Float64("duration_seconds", result.DurationSeconds),
		)
	}
	return result, nil
}

// partitionInputsByConflictKey groups loaded inputs by their
// (scope, generation, repository) conflict key, preserving load order within a
// partition. Defaults are applied here so each worker sees a fully-normalized
// input. Distinct partitions write disjoint rows and run concurrently; inputs
// sharing one key (e.g. two source runs for the same repository generation)
// stay in one ordered partition so their DELETE+INSERT replacements never race.
func (r *CodeReachabilityProjectionRunner) partitionInputsByConflictKey(
	inputs []CodeReachabilityProjectionInput,
	now time.Time,
) [][]CodeReachabilityProjectionInput {
	order := make([]string, 0, len(inputs))
	byKey := make(map[string][]CodeReachabilityProjectionInput, len(inputs))
	for _, input := range inputs {
		if input.MaxDepth <= 0 {
			input.MaxDepth = r.maxDepth()
		}
		if input.MaxVisited <= 0 {
			input.MaxVisited = r.maxVisited()
		}
		if input.ObservedAt.IsZero() {
			input.ObservedAt = now
		}
		if input.UpdatedAt.IsZero() {
			input.UpdatedAt = now
		}
		key := codeReachabilityConflictKey(input)
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], input)
	}
	sort.Strings(order)
	partitions := make([][]CodeReachabilityProjectionInput, 0, len(order))
	for _, key := range order {
		partitions = append(partitions, byKey[key])
	}
	return partitions
}

// projectPartitions projects each conflict partition, running up to
// r.concurrency() partitions at once. It returns total rows written and the
// count of truncated snapshots, or the first write error after canceling
// in-flight workers.
func (r *CodeReachabilityProjectionRunner) projectPartitions(
	ctx context.Context,
	partitions [][]CodeReachabilityProjectionInput,
) (int64, int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		totalRows int64
		truncated int64
		wg        sync.WaitGroup
		errOnce   sync.Once
		firstErr  error
	)
	sem := make(chan struct{}, r.concurrency())
	for _, partition := range partitions {
		if ctx.Err() != nil {
			break
		}
		// Acquire a slot, but stop launching work if the context is canceled
		// (e.g. a peer partition failed) while we wait for one.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(partition []CodeReachabilityProjectionInput) {
			defer wg.Done()
			defer func() { <-sem }()
			for _, input := range partition {
				if ctx.Err() != nil {
					return
				}
				rows, stats := BuildCodeReachabilityRowsWithStats(input)
				if err := r.RowWriter.ReplaceRepositoryRows(
					ctx, input.ScopeID, input.GenerationID, input.RepositoryID, rows, input.UpdatedAt, stats.Truncated,
				); err != nil {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("write code reachability rows: %w", err)
						cancel()
					})
					return
				}
				atomic.AddInt64(&totalRows, int64(len(rows)))
				if stats.Truncated {
					atomic.AddInt64(&truncated, 1)
					if r.Logger != nil {
						r.Logger.Warn(
							"code reachability snapshot truncated at max visited bound",
							slog.String("scope_id", input.ScopeID),
							slog.String("generation_id", input.GenerationID),
							slog.String("repository_id", input.RepositoryID),
							slog.Int("visited", stats.Visited),
						)
					}
				}
			}
		}(partition)
	}
	wg.Wait()
	if firstErr != nil {
		return 0, 0, firstErr
	}
	return totalRows, truncated, nil
}

// codeReachabilityConflictKey is the durable per-partition claim fence for the
// projection: the (scope, generation, repository) triple that ReplaceRepositoryRows
// deletes and re-inserts as one disjoint, idempotent unit.
func codeReachabilityConflictKey(input CodeReachabilityProjectionInput) string {
	return input.ScopeID + "\x00" + input.GenerationID + "\x00" + input.RepositoryID
}

func (r *CodeReachabilityProjectionRunner) validate() error {
	if r == nil {
		return fmt.Errorf("code reachability projection runner is nil")
	}
	if r.InputLoader == nil {
		return fmt.Errorf("code reachability projection input loader is nil")
	}
	if r.RowWriter == nil {
		return fmt.Errorf("code reachability projection row writer is nil")
	}
	return nil
}

func (r *CodeReachabilityProjectionRunner) wait(ctx context.Context, d time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *CodeReachabilityProjectionRunner) pollInterval() time.Duration {
	if r.Config.PollInterval <= 0 {
		return defaultCodeReachabilityPollInterval
	}
	return r.Config.PollInterval
}

func (r *CodeReachabilityProjectionRunner) batchLimit() int {
	if r.Config.BatchLimit <= 0 {
		return 10
	}
	return r.Config.BatchLimit
}

func (r *CodeReachabilityProjectionRunner) maxDepth() int {
	if r.Config.MaxDepth <= 0 {
		return defaultCodeReachabilityMaxDepth
	}
	return r.Config.MaxDepth
}

func (r *CodeReachabilityProjectionRunner) maxVisited() int {
	if r.Config.MaxVisited <= 0 {
		return defaultCodeReachabilityMaxVisited
	}
	return r.Config.MaxVisited
}

// concurrency returns the bounded partition fan-out, clamped to at least one
// and never above the host CPU count so the runner cannot oversubscribe the
// reducer process or the Postgres connection pool.
func (r *CodeReachabilityProjectionRunner) concurrency() int {
	limit := r.Config.Concurrency
	if limit <= 0 {
		limit = defaultCodeReachabilityConcurrency
	}
	if cpus := runtime.NumCPU(); limit > cpus {
		limit = cpus
	}
	if limit < 1 {
		limit = 1
	}
	return limit
}
