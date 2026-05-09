package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	defaultCodeCallLeaseOwner = "code-call-projection-runner"
	maxCodeCallPollInterval   = 5 * time.Second
)

// DefaultCodeCallAcceptanceScanLimit bounds how many pending code-call intents
// the runner may scan or load for one authoritative acceptance unit. The runner
// must see the complete unit before retracting and rewriting repo-wide CALLS
// edges; this guard prevents silent partial graph truth while allowing large
// real repositories to exceed the normal per-cycle batch size.
const DefaultCodeCallAcceptanceScanLimit = 250_000

// CodeCallProjectionIntentReader reads code-call intents by domain and bounded
// acceptance unit.
type CodeCallProjectionIntentReader interface {
	ListPendingDomainIntents(ctx context.Context, domain string, limit int) ([]SharedProjectionIntentRow, error)
	ListPendingAcceptanceUnitIntents(ctx context.Context, key SharedProjectionAcceptanceKey, domain string, limit int) ([]SharedProjectionIntentRow, error)
	MarkIntentsCompleted(ctx context.Context, intentIDs []string, completedAt time.Time) error
}

// CodeCallProjectionHistoryLookup checks whether an acceptance unit has ever
// completed code-call projection before. Runners use it only to skip proven
// first-projection no-op retractions; absence or errors keep the conservative
// retract-before-write path.
type CodeCallProjectionHistoryLookup interface {
	HasCompletedAcceptanceUnitDomainIntents(ctx context.Context, key SharedProjectionAcceptanceKey, domain string) (bool, error)
}

// CodeCallProjectionCurrentRunHistoryLookup checks whether the selected source
// run has already completed at least one code-call projection chunk.
type CodeCallProjectionCurrentRunHistoryLookup interface {
	HasCompletedAcceptanceUnitSourceRunDomainIntents(ctx context.Context, key SharedProjectionAcceptanceKey, domain string) (bool, error)
}

// ReducerGraphDrain reports whether reducer graph-writing domains are still
// active, letting local single-backend runners avoid graph write contention.
type ReducerGraphDrain interface {
	HasActiveReducerGraphWork(ctx context.Context) (bool, error)
}

// CodeCallProjectionRunnerConfig configures the controlled code-calls lane.
type CodeCallProjectionRunnerConfig struct {
	LeaseOwner          string
	PollInterval        time.Duration
	LeaseTTL            time.Duration
	BatchLimit          int
	AcceptanceScanLimit int
}

func (c CodeCallProjectionRunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultSharedPollInterval
	}
	return c.PollInterval
}

func (c CodeCallProjectionRunnerConfig) leaseTTL() time.Duration {
	if c.LeaseTTL <= 0 {
		return defaultLeaseTTL
	}
	return c.LeaseTTL
}

func (c CodeCallProjectionRunnerConfig) batchLimit() int {
	if c.BatchLimit <= 0 {
		return defaultBatchLimit
	}
	return c.BatchLimit
}

func (c CodeCallProjectionRunnerConfig) acceptanceScanLimit() int {
	if c.AcceptanceScanLimit <= 0 {
		return DefaultCodeCallAcceptanceScanLimit
	}
	if c.AcceptanceScanLimit < c.batchLimit() {
		return c.batchLimit()
	}
	return c.AcceptanceScanLimit
}

func (c CodeCallProjectionRunnerConfig) leaseOwner() string {
	if c.LeaseOwner == "" {
		return defaultCodeCallLeaseOwner
	}
	return c.LeaseOwner
}

// CodeCallProjectionRunner processes code-call shared intents one repo/run at a time.
type CodeCallProjectionRunner struct {
	IntentReader        CodeCallProjectionIntentReader
	LeaseManager        PartitionLeaseManager
	EdgeWriter          SharedProjectionEdgeWriter
	AcceptedGen         AcceptedGenerationLookup
	AcceptedGenPrefetch AcceptedGenerationPrefetch
	ReadinessLookup     GraphProjectionReadinessLookup
	ReadinessPrefetch   GraphProjectionReadinessPrefetch
	ReducerGraphDrain   ReducerGraphDrain
	Config              CodeCallProjectionRunnerConfig
	Wait                func(context.Context, time.Duration) error

	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Run drains code-call work until the context is canceled.
func (r *CodeCallProjectionRunner) Run(ctx context.Context) error {
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

func (r *CodeCallProjectionRunner) runOneCycle(ctx context.Context) (PartitionProcessResult, error) {
	result, err := r.processOnce(ctx, time.Now().UTC())
	if err != nil {
		return result, err
	}
	return result, nil
}

func (r *CodeCallProjectionRunner) processOnce(ctx context.Context, now time.Time) (result PartitionProcessResult, retErr error) {
	cycleStart := time.Now()
	acceptanceTelemetry := sharedAcceptanceTelemetry{
		Instruments: r.Instruments,
		Logger:      r.Logger,
	}
	if r.ReducerGraphDrain != nil {
		active, err := r.ReducerGraphDrain.HasActiveReducerGraphWork(ctx)
		if err != nil {
			return PartitionProcessResult{}, fmt.Errorf("check reducer graph drain: %w", err)
		}
		if active {
			result := PartitionProcessResult{BlockedReadiness: 1}
			r.recordCodeCallTiming(ctx, result)
			return result, nil
		}
	}

	claimStart := time.Now()
	claimed, err := r.LeaseManager.ClaimPartitionLease(
		ctx,
		DomainCodeCalls,
		0,
		1,
		r.Config.leaseOwner(),
		r.Config.leaseTTL(),
	)
	if r.Instruments != nil {
		r.Instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
			attribute.String("queue", "code_calls"),
		))
	}
	if err != nil {
		return PartitionProcessResult{}, fmt.Errorf("claim code call lease: %w", err)
	}
	if !claimed {
		return PartitionProcessResult{LeaseAcquired: false}, nil
	}
	leaseClaimDuration := time.Since(claimStart).Seconds()

	leaseCtx, stopHeartbeat := r.startLeaseHeartbeat(ctx)
	defer func() {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			if retErr == nil {
				retErr = fmt.Errorf("heartbeat code call lease: %w", heartbeatErr)
			} else {
				retErr = errors.Join(retErr, fmt.Errorf("heartbeat code call lease: %w", heartbeatErr))
			}
		}
		_ = r.LeaseManager.ReleasePartitionLease(ctx, DomainCodeCalls, 0, 1, r.Config.leaseOwner())
	}()
	ctx = leaseCtx

	selection, err := r.selectAcceptanceUnitWorkWithStats(ctx, now)
	if err != nil {
		return PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseClaimDuration,
		}, err
	}
	if selection.Key == (SharedProjectionAcceptanceKey{}) {
		result := PartitionProcessResult{
			LeaseAcquired:               true,
			BlockedReadiness:            selection.BlockedReadiness,
			MaxBlockedIntentWaitSeconds: selection.MaxBlockedIntentWaitSeconds,
			LeaseClaimDurationSeconds:   leaseClaimDuration,
			SelectionDurationSeconds:    selection.SelectionDurationSeconds,
		}
		r.recordCodeCallTiming(ctx, result)
		return result, nil
	}

	rows, err := r.loadAllAcceptanceUnitIntents(ctx, selection.Key)
	if err != nil {
		return PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseClaimDuration,
			SelectionDurationSeconds:  selection.SelectionDurationSeconds,
		}, err
	}

	lookup := r.AcceptedGen
	if r.AcceptedGenPrefetch != nil {
		resolvedLookup, err := r.AcceptedGenPrefetch(ctx, rows)
		if err != nil {
			return PartitionProcessResult{
				LeaseAcquired:             true,
				LeaseClaimDurationSeconds: leaseClaimDuration,
				SelectionDurationSeconds:  selection.SelectionDurationSeconds,
			}, fmt.Errorf("prefetch accepted generations: %w", err)
		}
		lookup = resolvedLookup
	}

	active, staleIDs := FilterAuthoritativeIntents(rows, lookup)
	acceptanceTelemetry.RecordStaleIntents(ctx, "code_call_projection", DomainCodeCalls, len(staleIDs))
	if len(active) == 0 && len(staleIDs) == 0 {
		result := PartitionProcessResult{
			LeaseAcquired:               true,
			BlockedReadiness:            selection.BlockedReadiness,
			MaxBlockedIntentWaitSeconds: selection.MaxBlockedIntentWaitSeconds,
			LeaseClaimDurationSeconds:   leaseClaimDuration,
			SelectionDurationSeconds:    selection.SelectionDurationSeconds,
		}
		r.recordCodeCallTiming(ctx, result)
		return result, nil
	}

	result = PartitionProcessResult{
		LeaseAcquired:               true,
		BlockedReadiness:            selection.BlockedReadiness,
		MaxBlockedIntentWaitSeconds: selection.MaxBlockedIntentWaitSeconds,
		LeaseClaimDurationSeconds:   leaseClaimDuration,
		SelectionDurationSeconds:    selection.SelectionDurationSeconds,
	}
	processingStart := time.Now()
	writtenGroups := 0
	if len(active) > 0 {
		skipRetract, err := r.shouldSkipCodeCallRetract(ctx, selection.Key, staleIDs)
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

	processedIDs := make([]string, 0, len(staleIDs)+len(active))
	processedIDs = append(processedIDs, staleIDs...)
	for _, row := range active {
		processedIDs = append(processedIDs, row.IntentID)
	}
	if len(processedIDs) > 0 {
		markStart := time.Now()
		if err := r.IntentReader.MarkIntentsCompleted(ctx, processedIDs, now); err != nil {
			return result, fmt.Errorf("mark code call intents completed: %w", err)
		}
		result.MarkCompletedDurationSeconds = time.Since(markStart).Seconds()
	}

	result.ProcessedIntents = len(processedIDs)
	result.MaxIntentWaitSeconds = maxSharedIntentWaitSeconds(now, rows)
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
