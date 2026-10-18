// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection //nolint:filelength // 516 lines (base 467): the #7853 unit-level absent-acceptance drain and mark-count WARN live in processPartitionOnce next to the batch they mark; extracting them would split one cycle's mark path across files.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	codecall "github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	maxCodeCallPollInterval = 5 * time.Second
)

// DefaultLeaseOwnerPrefix is the default human-readable
// label prepended to the production reducer's process-unique code-call owner.
const DefaultLeaseOwnerPrefix = "code-call-projection-runner"

// DefaultAcceptanceScanLimit bounds how many pending code-call intents
// the runner may scan or load for one authoritative acceptance unit. The runner
// must see the complete unit before retracting and rewriting repo-wide CALLS
// edges; this guard prevents silent partial graph truth while allowing large
// real repositories to exceed the normal per-cycle batch size. The value
// lives in [codecall.AcceptanceScanLimit] so full-refresh file scoping shares it.
const DefaultAcceptanceScanLimit = codecall.AcceptanceScanLimit

// IntentReader reads code-call intents by domain and bounded
// acceptance unit.
type IntentReader interface {
	ListPendingDomainIntents(ctx context.Context, domain string, limit int) ([]sharedintent.Row, error)
	ListPendingAcceptanceUnitIntents(ctx context.Context, key sharedintent.AcceptanceKey, domain string, limit int) ([]sharedintent.Row, error)
	// MarkIntentsCompleted marks intents completed and returns the rows
	// matched, so the caller can log a batch whose rows vanished mid-cycle
	// instead of reporting silent success (#7853).
	MarkIntentsCompleted(ctx context.Context, intentIDs []string, completedAt time.Time) (int64, error)
}

// PartitionIntentReader reads pending code-call rows for one
// selected partition without scanning unrelated acceptance-unit partitions.
type PartitionIntentReader interface {
	ListPendingAcceptanceUnitPartitionIntents(
		ctx context.Context,
		key sharedintent.AcceptanceKey,
		domain string,
		partitionKey string,
		limit int,
	) ([]sharedintent.Row, error)
}

// HistoryLookup checks whether an acceptance unit has ever
// completed code-call projection before. Runners use it only to skip proven
// first-projection no-op retractions; absence or errors keep the conservative
// retract-before-write path.
type HistoryLookup interface {
	HasCompletedAcceptanceUnitDomainIntents(ctx context.Context, key sharedintent.AcceptanceKey, domain string) (bool, error)
}

// CurrentRunHistoryLookup checks whether the selected source
// run has already completed at least one code-call projection chunk.
type CurrentRunHistoryLookup interface {
	HasCompletedAcceptanceUnitSourceRunDomainIntents(ctx context.Context, key sharedintent.AcceptanceKey, domain string) (bool, error)
}

// CurrentRunPartitionHistoryLookup checks whether the
// selected partition for a source run has already completed.
type CurrentRunPartitionHistoryLookup interface {
	HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(
		ctx context.Context,
		key sharedintent.AcceptanceKey,
		partitionKey string,
		domain string,
	) (bool, error)
}

// CurrentRunRefreshHistoryLookup checks whether a completed
// repo-refresh intent from the selected source run already covers a file slice.
type CurrentRunRefreshHistoryLookup interface {
	HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents(
		ctx context.Context,
		key sharedintent.AcceptanceKey,
		filePaths []string,
		domain string,
	) (bool, error)
}

// RefreshFenceLookup checks whether a pending code-call row
// is blocked by an ordering fence without loading the whole acceptance unit.
type RefreshFenceLookup interface {
	CodeCallProjectionRowBlockedByRepoFence(
		ctx context.Context,
		key sharedintent.AcceptanceKey,
		row sharedintent.Row,
		domain string,
	) (bool, error)
}

// Runner processes code-call shared intents one repo/run at a time.
type Runner struct {
	IntentReader        IntentReader
	LeaseManager        sharedintent.PartitionLeaseManager
	EdgeWriter          sharedintent.EdgeWriter
	AcceptedGen         sharedintent.AcceptedGenerationLookup
	AcceptedGenPrefetch sharedintent.AcceptedGenerationPrefetch
	ReadinessLookup     gpphase.ReadinessLookup
	ReadinessPrefetch   gpphase.ReadinessPrefetch
	ReducerGraphDrain   ReducerGraphDrain
	CanonicalQuiescence CanonicalCodeQuiescenceChecker
	Config              RunnerConfig
	Wait                func(context.Context, time.Duration) error

	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Logger      *slog.Logger

	blockedLane laneBlockState // lane-wide gate episode, for rate-limited reporting (#7133)
}

// Run drains code-call work until the context is canceled.
func (r *Runner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}

	consecutiveEmpty := 0
	for {
		if ctx.Err() != nil {
			return nil
		}

		cycleStart := time.Now()
		result, err := r.runOneCycle(ctx)
		if err != nil {
			consecutiveEmpty++
			r.recordCodeCallCycleFailure(ctx, err, time.Since(cycleStart).Seconds())
			if err := r.wait(ctx, codeCallPollBackoff(r.Config.pollInterval(), consecutiveEmpty)); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("wait for code call work: %w", err)
			}
			continue
		}
		if result.ProcessedIntents > 0 {
			consecutiveEmpty = 0
			continue
		}
		if result.BlockedReadiness > 0 {
			consecutiveEmpty = 0
			if err := r.wait(ctx, r.Config.pollInterval()); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("wait for code call readiness: %w", err)
			}
			continue
		}

		consecutiveEmpty++
		if err := r.wait(ctx, codeCallPollBackoff(r.Config.pollInterval(), consecutiveEmpty)); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wait for code call work: %w", err)
		}
	}
}

func (r *Runner) runOneCycle(ctx context.Context) (worker.PartitionProcessResult, error) {
	now := time.Now().UTC()
	if r.Config.workers() <= 1 {
		return r.runOneCycleSequential(ctx, now)
	}
	return r.runOneCycleConcurrent(ctx, now)
}

func (r *Runner) processOnce(ctx context.Context, now time.Time) (result worker.PartitionProcessResult, retErr error) {
	return r.processPartitionOnce(ctx, now, 0, r.Config.partitionCount())
}

func (r *Runner) runOneCycleSequential(ctx context.Context, now time.Time) (worker.PartitionProcessResult, error) {
	var cycleResult worker.PartitionProcessResult
	for partitionID := 0; partitionID < r.Config.partitionCount(); partitionID++ {
		if ctx.Err() != nil {
			return cycleResult, nil
		}
		result, err := r.processPartitionOnce(ctx, now, partitionID, r.Config.partitionCount())
		worker.MergePartitionProcessResult(&cycleResult, result)
		if err != nil {
			return cycleResult, err
		}
	}
	return cycleResult, nil
}

func (r *Runner) runOneCycleConcurrent(ctx context.Context, now time.Time) (worker.PartitionProcessResult, error) {
	partitionCount := r.Config.partitionCount()
	work := make(chan int, partitionCount)
	for partitionID := 0; partitionID < partitionCount; partitionID++ {
		work <- partitionID
	}
	close(work)

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		cycleResult worker.PartitionProcessResult
		errs        []error
	)
	for i := 0; i < r.Config.workers(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for partitionID := range work {
				if ctx.Err() != nil {
					return
				}
				result, err := r.processPartitionOnce(ctx, now, partitionID, partitionCount)
				mu.Lock()
				worker.MergePartitionProcessResult(&cycleResult, result)
				if err != nil {
					errs = append(errs, err)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return cycleResult, errors.Join(errs...)
}

func (r *Runner) processPartitionOnce(
	ctx context.Context,
	now time.Time,
	partitionID int,
	partitionCount int,
) (result worker.PartitionProcessResult, retErr error) {
	cycleStart := time.Now()
	acceptanceTelemetry := worker.AcceptanceTelemetry{
		Instruments: r.Instruments,
		Logger:      r.Logger,
	}
	blockedReason, err := r.projectionLaneBlocked(ctx)
	if err != nil {
		return worker.PartitionProcessResult{}, err
	}
	if blockedReason != "" {
		r.recordCodeCallLaneBlocked(ctx, blockedReason)
		result := worker.PartitionProcessResult{BlockedReadiness: 1}
		r.recordCodeCallTiming(ctx, result)
		return result, nil
	}
	r.recordCodeCallLaneReleased(ctx)

	claimStart := time.Now()
	claimed, err := r.LeaseManager.ClaimPartitionLease(
		ctx,
		reducercontract.DomainCodeCalls,
		partitionID,
		partitionCount,
		r.Config.leaseOwner(),
		r.Config.leaseTTL(),
	)
	if r.Instruments != nil {
		r.Instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
			attribute.String("queue", "code_calls"),
		))
	}
	if err != nil {
		return worker.PartitionProcessResult{}, fmt.Errorf("claim code call lease: %w", err)
	}
	if !claimed {
		return worker.PartitionProcessResult{LeaseAcquired: false}, nil
	}
	leaseClaimDuration := time.Since(claimStart).Seconds()

	// Preserve the caller context for release. stopHeartbeat cancels leaseCtx
	// before the deferred release runs, and Postgres rejects an ExecContext
	// handed that canceled context, leaving the partition leased until TTL.
	releaseCtx := ctx
	leaseCtx, stopHeartbeat := r.startLeaseHeartbeat(ctx, partitionID, partitionCount)
	defer func() {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			if retErr == nil {
				retErr = fmt.Errorf("heartbeat code call lease: %w", heartbeatErr)
			} else {
				retErr = errors.Join(retErr, fmt.Errorf("heartbeat code call lease: %w", heartbeatErr))
			}
		}
		_ = r.LeaseManager.ReleasePartitionLease(releaseCtx, reducercontract.DomainCodeCalls, partitionID, partitionCount, r.Config.leaseOwner())
	}()
	ctx = leaseCtx

	selection, err := r.selectAcceptanceUnitPartitionWorkWithStats(ctx, now, partitionID, partitionCount)
	if err != nil {
		return worker.PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseClaimDuration,
		}, err
	}
	if selection.Key == (sharedintent.AcceptanceKey{}) {
		result := worker.PartitionProcessResult{
			LeaseAcquired:               true,
			BlockedReadiness:            selection.BlockedReadiness,
			MaxBlockedIntentWaitSeconds: selection.MaxBlockedIntentWaitSeconds,
			LeaseClaimDurationSeconds:   leaseClaimDuration,
			SelectionDurationSeconds:    selection.SelectionDurationSeconds,
			SelectionPhases:             selection.SelectionPhases,
		}
		r.recordCodeCallTiming(ctx, result)
		return result, nil
	}

	rows, err := r.loadAcceptanceUnitPartitionIntents(ctx, selection.Key, selection.PartitionKey)
	if err != nil {
		return worker.PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseClaimDuration,
			SelectionDurationSeconds:  selection.SelectionDurationSeconds,
		}, err
	}

	lookup := r.AcceptedGen
	if r.AcceptedGenPrefetch != nil {
		resolvedLookup, err := r.AcceptedGenPrefetch(ctx, rows)
		if err != nil {
			return worker.PartitionProcessResult{
				LeaseAcquired:             true,
				LeaseClaimDurationSeconds: leaseClaimDuration,
				SelectionDurationSeconds:  selection.SelectionDurationSeconds,
			}, fmt.Errorf("prefetch accepted generations: %w", err)
		}
		lookup = resolvedLookup
	}

	active, staleIDs := worker.FilterAuthoritativeIntents(rows, lookup)
	acceptanceTelemetry.RecordStaleIntents(ctx, "code_call_projection", reducercontract.DomainCodeCalls, len(staleIDs))

	// Absent-acceptance drain for the selected unit (#7853): the acceptance
	// row can vanish between selection and this load. Drain the
	// generation-gone subset inline and keep torn writes pending. The
	// metric is recorded in the drain, exactly once.
	unitAbsent := make(map[string]sharedintent.Row, len(rows))
	for _, row := range worker.AbsentAcceptanceRows(rows, active, staleIDs) {
		unitAbsent[row.IntentID] = row
	}
	unitDrained, unitTorn, err := worker.DrainAbsentAcceptance(
		ctx, now, acceptanceTelemetry,
		"code_call_projection", reducercontract.DomainCodeCalls, r.IntentReader, unitAbsent,
	)
	if err != nil {
		return worker.PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseClaimDuration,
			SelectionDurationSeconds:  selection.SelectionDurationSeconds,
		}, fmt.Errorf("drain absent acceptance rows: %w", err)
	}
	// Drained unit rows stay OUT of the stale set: they are already marked
	// completed inside the drain, and joining staleIDs would mark them
	// twice and double-count them as processed. They count under the
	// absent-generation-gone reason recorded in the drain. Torn rows stay
	// pending and were WARNed in the drain.
	unitAbsentDrained := len(unitDrained)
	unitAbsentTorn := unitTorn

	// Drain rows covered by a newer emitted full generation (#7165): the
	// successor re-emits their edges, so replaying them would be a wasted
	// retract/write cycle. Drained rows join the stale set and are marked
	// completed without a write; a pure-drain cycle still runs the forced
	// retract below (#7736 F5). A lookup error fails the cycle instead of
	// guessing.
	kept, drainable, err := worker.SplitCoveredByFullSuccessorRows(ctx, r.IntentReader, reducercontract.DomainCodeCalls, active)
	if err != nil {
		return worker.PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseClaimDuration,
			SelectionDurationSeconds:  selection.SelectionDurationSeconds,
		}, fmt.Errorf("split generations covered by emitted full successor: %w", err)
	}
	for _, row := range drainable {
		staleIDs = append(staleIDs, row.IntentID)
	}
	active = kept
	acceptanceTelemetry.RecordCoveredByFullSuccessorIntents(ctx, "code_call_projection", reducercontract.DomainCodeCalls, len(drainable))
	if len(active) == 0 && len(staleIDs) == 0 && unitAbsentDrained == 0 {
		result := worker.PartitionProcessResult{
			LeaseAcquired:               true,
			BlockedReadiness:            selection.BlockedReadiness,
			MaxBlockedIntentWaitSeconds: selection.MaxBlockedIntentWaitSeconds,
			LeaseClaimDurationSeconds:   leaseClaimDuration,
			SelectionDurationSeconds:    selection.SelectionDurationSeconds,
			SelectionPhases:             selection.SelectionPhases,
			AbsentGenerationGoneIntents: selection.AbsentDrained,
			AbsentAcceptanceTornWrites:  selection.AbsentTorn + unitAbsentTorn,
		}
		r.recordCodeCallTiming(ctx, result)
		return result, nil
	}

	result = worker.PartitionProcessResult{
		LeaseAcquired:                 true,
		BlockedReadiness:              selection.BlockedReadiness,
		MaxBlockedIntentWaitSeconds:   selection.MaxBlockedIntentWaitSeconds,
		LeaseClaimDurationSeconds:     leaseClaimDuration,
		SelectionDurationSeconds:      selection.SelectionDurationSeconds,
		SelectionPhases:               selection.SelectionPhases,
		StaleIntents:                  len(staleIDs),
		CoveredByFullSuccessorIntents: len(drainable),
		AbsentGenerationGoneIntents:   selection.AbsentDrained + unitAbsentDrained,
		AbsentAcceptanceTornWrites:    selection.AbsentTorn + unitAbsentTorn,
	}
	processingStart := time.Now()
	writtenGroups := 0
	if len(active) > 0 {
		skipRetract, err := r.shouldSkipCodeCallRetract(ctx, selection.Key, selection.PartitionKey, active, staleIDs)
		if err != nil {
			return result, err
		}
		if !skipRetract {
			retractStart := time.Now()
			if err := r.retractRepo(ctx, active); err != nil {
				return result, err
			}
			result.RetractDurationSeconds = time.Since(retractStart).Seconds()
			result.RetractedRows = len(active)
		}

		writeStart := time.Now()
		writtenRows, groups, err := r.writeActiveRows(ctx, active)
		if err != nil {
			return result, err
		}
		result.WriteDurationSeconds = time.Since(writeStart).Seconds()
		writtenGroups = groups
		result.UpsertedRows = writtenRows
	}
	// #7736 F5 drain-forced-retract marker: a pure-drain cycle (nothing
	// kept, drainable rows present) runs retract on the drained scope before
	// marking completed, with no write and no history check. A crash-window
	// partial write (write ok, mark failed) ahead of the drain would
	// otherwise linger: the drained rows complete without a write, and the
	// successor's own cycle has no stale IDs to force its retract. The
	// drain only fires for the accepted generation, which the successor has
	// not yet superseded, so this retract cannot wipe the successor's
	// edges: the drain always precedes the successor's write. All-stale
	// cycles with no drainable rows keep the old skip.
	if len(active) == 0 && len(drainable) > 0 {
		retractStart := time.Now()
		if err := r.retractRepo(ctx, drainable); err != nil {
			return result, err
		}
		result.RetractDurationSeconds = time.Since(retractStart).Seconds()
		result.RetractedRows = len(drainable)
	}

	processedIDs := make([]string, 0, len(staleIDs)+len(active))
	processedIDs = append(processedIDs, staleIDs...)
	for _, row := range active {
		processedIDs = append(processedIDs, row.IntentID)
	}
	if len(processedIDs) > 0 {
		markStart := time.Now()
		affected, err := r.IntentReader.MarkIntentsCompleted(ctx, processedIDs, now)
		if err != nil {
			return result, fmt.Errorf("mark code call intents completed: %w", err)
		}
		result.MarkCompletedDurationSeconds = time.Since(markStart).Seconds()
		// A short mark is a WARN, not an error: the missing rows were
		// deleted or completed concurrently (#7853).
		if affected < int64(len(processedIDs)) && r.Logger != nil {
			r.Logger.WarnContext(
				ctx,
				"code call projection marked fewer intents completed than requested; rows were deleted or completed concurrently",
				slog.String("runner", "code_call_projection"),
				log.Domain(reducercontract.DomainCodeCalls),
				slog.Int("requested_intents", len(processedIDs)),
				slog.Int64("completed_intents", affected),
				telemetry.PhaseAttr(telemetry.PhaseShared),
			)
		}
	}

	result.ProcessedIntents = len(processedIDs)
	result.MaxIntentWaitSeconds = worker.MaxIntentWaitSeconds(now, rows)
	result.ProcessingDurationSeconds = time.Since(processingStart).Seconds()
	if len(active) > 0 {
		if err := r.recordCodeCallCycle(
			ctx,
			selection.Key,
			acceptedGenerationID(active),
			result.UpsertedRows,
			writtenGroups,
			cycleStart,
			result,
		); err != nil {
			return result, err
		}
	}
	r.recordCodeCallTiming(ctx, result)
	return result, nil
}
