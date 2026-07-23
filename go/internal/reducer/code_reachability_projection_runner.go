// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cpubudget"
	log "github.com/eshu-hq/eshu/go/pkg/log"
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

// CodeReachabilityRowWriter writes materialized code reachability rows and the
// co-owned #5376 code-root verdict rows. Both are replaced in one transaction
// per partition so a downgraded controller root and the reachability rows built
// from the downgraded-filtered root set can never disagree.
type CodeReachabilityRowWriter interface {
	ReplaceRepositoryRows(
		ctx context.Context,
		scopeID string,
		generationID string,
		repositoryID string,
		rows []CodeReachabilityRow,
		verdicts []CodeRootVerdictRow,
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
	// VerdictsWritten is the total #5376 code-root verdict rows written
	// (confirmed + downgraded) across the cycle.
	VerdictsWritten int
	// VerdictsDowngraded counts controller-action roots the repo-wide decision
	// positively resolved onward to a reject branch this cycle.
	VerdictsDowngraded int
	// VerdictsInconclusiveMissingContext counts controller-action roots skipped
	// because they carried no class_context bridge (kept, no row written).
	VerdictsInconclusiveMissingContext int
	// VerdictsSuffixAmbiguousKept counts controller-action roots kept by the
	// #5376 P0 rev-2 suffix-ambiguity floor (a base resolved only by a proper
	// namespace suffix, or a conventional ambiguous simple name).
	VerdictsSuffixAmbiguousKept int
	// VerdictsRouteDowngraded counts ancestry-confirmed controller-action
	// roots the #5494 route-liveness check additionally downgraded because
	// the repo's exact-only, observed route surface proved no route reaches
	// them. Included in VerdictsDowngraded.
	VerdictsRouteDowngraded int
	DurationSeconds         float64
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
	agg, err := r.projectPartitions(ctx, partitions)
	if err != nil {
		return CodeReachabilityProjectionResult{}, err
	}

	result := CodeReachabilityProjectionResult{
		InputsProcessed:                    len(inputs),
		RowsWritten:                        int(agg.totalRows),
		SnapshotsTruncated:                 int(agg.truncated),
		VerdictsWritten:                    int(agg.verdictsWritten),
		VerdictsDowngraded:                 int(agg.verdictsDowngraded),
		VerdictsInconclusiveMissingContext: int(agg.verdictsInconclusiveMissingContext),
		VerdictsSuffixAmbiguousKept:        int(agg.verdictsSuffixAmbiguousKept),
		VerdictsRouteDowngraded:            int(agg.verdictsRouteDowngraded),
		DurationSeconds:                    time.Since(start).Seconds(),
	}
	if r.Logger != nil {
		r.Logger.Info(
			"code reachability projection completed",
			slog.Int("input_count", result.InputsProcessed),
			slog.Int("row_count", result.RowsWritten),
			slog.Int("partition_count", len(partitions)),
			slog.Int("concurrency", r.concurrency()),
			slog.Int("snapshots_truncated", result.SnapshotsTruncated),
			slog.Int("verdicts_written", result.VerdictsWritten),
			slog.Int("verdicts_downgraded", result.VerdictsDowngraded),
			slog.Int("verdicts_inconclusive_missing_context", result.VerdictsInconclusiveMissingContext),
			slog.Int("verdicts_suffix_ambiguous_kept", result.VerdictsSuffixAmbiguousKept),
			slog.Int("verdicts_route_downgraded", result.VerdictsRouteDowngraded),
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

// codeReachabilityProjectionAggregate accumulates per-cycle counters across the
// concurrent partition workers.
type codeReachabilityProjectionAggregate struct {
	totalRows                          int64
	truncated                          int64
	verdictsWritten                    int64
	verdictsDowngraded                 int64
	verdictsInconclusiveMissingContext int64
	verdictsSuffixAmbiguousKept        int64
	verdictsRouteDowngraded            int64
}

// projectPartitions projects each conflict partition, running up to
// r.concurrency() partitions at once. It returns the aggregated cycle counters,
// or the first write error after canceling in-flight workers.
func (r *CodeReachabilityProjectionRunner) projectPartitions(
	ctx context.Context,
	partitions [][]CodeReachabilityProjectionInput,
) (codeReachabilityProjectionAggregate, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		agg      codeReachabilityProjectionAggregate
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
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
				if err := r.projectInput(ctx, input, &agg); err != nil {
					errOnce.Do(func() {
						firstErr = err
						cancel()
					})
					return
				}
			}
		}(partition)
	}
	wg.Wait()
	if firstErr != nil {
		return codeReachabilityProjectionAggregate{}, firstErr
	}
	return agg, nil
}

// projectInput computes and atomically persists one repository-generation
// snapshot: the #5376 controller verdicts, the reachability rows built from the
// downgraded-filtered root set, and both writes in one transaction. Filtering
// the roots BEFORE the BFS is what keeps the materialized reachability rows from
// asserting everything under a downgraded controller reachable while the query
// calls those actions dead.
func (r *CodeReachabilityProjectionRunner) projectInput(
	ctx context.Context,
	input CodeReachabilityProjectionInput,
	agg *codeReachabilityProjectionAggregate,
) error {
	verdicts, downgraded, verdictStats := BuildCodeRootVerdicts(input)

	reachabilityInput := input
	reachabilityInput.Roots = removeDowngradedRailsControllerRoots(input.Roots, downgraded)
	rows, stats := BuildCodeReachabilityRowsWithStats(reachabilityInput)

	if err := r.RowWriter.ReplaceRepositoryRows(
		ctx, input.ScopeID, input.GenerationID, input.RepositoryID, rows, verdicts, input.UpdatedAt, stats.Truncated,
	); err != nil {
		return fmt.Errorf("write code reachability rows: %w", err)
	}

	atomic.AddInt64(&agg.totalRows, int64(len(rows)))
	atomic.AddInt64(&agg.verdictsWritten, int64(len(verdicts)))
	atomic.AddInt64(&agg.verdictsDowngraded, int64(verdictStats.Downgraded))
	atomic.AddInt64(&agg.verdictsInconclusiveMissingContext, int64(verdictStats.InconclusiveMissingContext))
	atomic.AddInt64(&agg.verdictsSuffixAmbiguousKept, int64(verdictStats.SuffixAmbiguousKept))
	atomic.AddInt64(&agg.verdictsRouteDowngraded, int64(verdictStats.RouteDowngraded))
	if stats.Truncated {
		atomic.AddInt64(&agg.truncated, 1)
		if r.Logger != nil {
			r.Logger.Warn(
				"code reachability snapshot truncated at max visited bound",
				log.ScopeID(input.ScopeID),
				log.GenerationID(input.GenerationID),
				log.RepositoryID(input.RepositoryID),
				slog.Int("visited", stats.Visited),
			)
		}
	}
	if verdictStats.Downgraded > 0 && r.Logger != nil {
		r.Logger.Info(
			"code root controller verdicts downgraded",
			log.ScopeID(input.ScopeID),
			log.GenerationID(input.GenerationID),
			log.RepositoryID(input.RepositoryID),
			slog.Int("downgraded", verdictStats.Downgraded),
			slog.Int("confirmed", verdictStats.Confirmed),
			slog.Int("inconclusive_missing_context", verdictStats.InconclusiveMissingContext),
			slog.Int("route_downgraded", verdictStats.RouteDowngraded),
		)
	}
	return nil
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
// and never above the usable (cgroup-aware) CPU count so the runner cannot
// oversubscribe the reducer process or the Postgres connection pool. Uses
// cpubudget.UsableCPUs(), not internal/runtime's UsableCPUs(): internal/reducer
// cannot import internal/runtime without an import cycle (internal/runtime ->
// internal/recovery -> internal/projector -> internal/reducer). cpubudget has
// zero internal dependencies, so it is safe to import here.
func (r *CodeReachabilityProjectionRunner) concurrency() int {
	limit := r.Config.Concurrency
	if limit <= 0 {
		limit = defaultCodeReachabilityConcurrency
	}
	if cpus := cpubudget.UsableCPUs(); limit > cpus {
		limit = cpus
	}
	if limit < 1 {
		limit = 1
	}
	return limit
}
