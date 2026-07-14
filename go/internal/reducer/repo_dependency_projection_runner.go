// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	defaultRepoDependencyLeaseOwner      = "repo-dependency-projection-runner"
	maxRepoDependencyPollInterval        = 5 * time.Second
	maxRepoDependencyAcceptanceScanLimit = 10_000
)

// RepoDependencyProjectionIntentReader reads repo-dependency intents by domain
// and by source-repo-owned acceptance unit.
type RepoDependencyProjectionIntentReader interface {
	ListPendingDomainIntents(ctx context.Context, domain string, limit int) ([]SharedProjectionIntentRow, error)
	ListAcceptanceUnitDomainIntents(ctx context.Context, acceptanceUnitID, domain string, limit int) ([]SharedProjectionIntentRow, error)
	MarkIntentsCompleted(ctx context.Context, intentIDs []string, completedAt time.Time) error
}

// RepoDependencyProjectionRunner processes repo-dependency shared intents one
// source repository at a time so repo-wide retractions cannot race with
// partition-sliced snapshots.
type RepoDependencyProjectionRunner struct {
	IntentReader                    RepoDependencyProjectionIntentReader
	LeaseManager                    PartitionLeaseManager
	AcceptanceUnitGate              RepoDependencyAcceptanceUnitGate
	EdgeWriter                      SharedProjectionEdgeWriter
	WorkloadMaterializationReplayer WorkloadMaterializationReplayer
	AcceptedGen                     AcceptedGenerationLookup
	AcceptedGenPrefetch             AcceptedGenerationPrefetch
	Config                          RepoDependencyProjectionRunnerConfig
	Wait                            func(context.Context, time.Duration) error

	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Run drains repo-dependency work until the context is canceled.
func (r *RepoDependencyProjectionRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	return runRepoDependencyProjection(ctx, r)
}

func (r *RepoDependencyProjectionRunner) runSerial(ctx context.Context) error {
	consecutiveEmpty := 0
	for {
		if ctx.Err() != nil {
			return nil
		}

		cycleStart := time.Now()
		didWork, err := r.runOneCycle(ctx)
		if err != nil {
			consecutiveEmpty++
			r.recordRepoDependencyCycleFailure(ctx, err, time.Since(cycleStart).Seconds())
			delay := repoDependencyQuarantineDelay(
				err,
				repoDependencyPollBackoff(r.Config.pollInterval(), consecutiveEmpty),
			)
			if err := r.wait(ctx, delay); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("wait for repo dependency work: %w", err)
			}
			continue
		}
		if didWork {
			consecutiveEmpty = 0
			continue
		}

		consecutiveEmpty++
		if err := r.wait(ctx, repoDependencyPollBackoff(r.Config.pollInterval(), consecutiveEmpty)); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wait for repo dependency work: %w", err)
		}
	}
}

func (r *RepoDependencyProjectionRunner) runOneCycle(ctx context.Context) (bool, error) {
	result, err := r.processOnce(ctx, time.Now().UTC())
	if err != nil {
		return true, err
	}
	return result.ProcessedIntents > 0, nil
}

func (r *RepoDependencyProjectionRunner) processOnce(ctx context.Context, now time.Time) (PartitionProcessResult, error) {
	cycleStart := time.Now()
	claimStart := time.Now()
	result := PartitionProcessResult{}
	claimed, err := r.LeaseManager.ClaimPartitionLease(
		ctx,
		DomainRepoDependency,
		r.Config.partitionID(),
		r.Config.partitionCount(),
		r.Config.leaseOwner(),
		r.Config.leaseTTL(),
	)
	if r.Instruments != nil {
		r.Instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
			attribute.String("queue", "repo_dependency"),
		))
	}
	result.LeaseClaimDurationSeconds = time.Since(claimStart).Seconds()
	if err != nil {
		return PartitionProcessResult{}, fmt.Errorf("claim repo dependency lease: %w", err)
	}
	if !claimed {
		return PartitionProcessResult{LeaseAcquired: false}, nil
	}
	releaseLease := false
	defer func() {
		if releaseLease {
			_ = r.LeaseManager.ReleasePartitionLease(
				ctx,
				DomainRepoDependency,
				r.Config.partitionID(),
				r.Config.partitionCount(),
				r.Config.leaseOwner(),
			)
		}
	}()
	cycleCtx, cancelCycle := context.WithTimeout(ctx, r.Config.cycleTimeout())
	defer cancelCycle()
	workCtx, stopHeartbeat := r.startLeaseHeartbeat(cycleCtx)
	defer func() { _ = stopHeartbeat() }()

	selectionStart := time.Now()
	acceptanceUnitID, err := r.selectAcceptanceUnitWork(workCtx)
	result.SelectionDurationSeconds = time.Since(selectionStart).Seconds()
	if err != nil {
		result.LeaseAcquired = true
		return result, r.quarantineLease(err)
	}
	if acceptanceUnitID == "" {
		result.LeaseAcquired = true
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			return result, r.quarantineLease(heartbeatErr)
		}
		releaseLease = true
		return result, nil
	}
	if r.AcceptanceUnitGate == nil {
		result.LeaseAcquired = true
		return result, r.quarantineLease(errors.New("repo dependency projection runner: acceptance unit gate is required"))
	}

	var (
		processed     PartitionProcessResult
		active        []SharedProjectionIntentRow
		writtenGroups int
	)
	ran, gateErr := r.AcceptanceUnitGate.WithAcceptanceUnit(
		workCtx,
		RepoDependencyAcceptanceUnitGateKey{
			Domain:           DomainRepoDependency,
			AcceptanceUnitID: acceptanceUnitID,
			PartitionID:      r.Config.partitionID(),
			PartitionCount:   r.Config.partitionCount(),
			LeaseOwner:       r.Config.leaseOwner(),
		},
		func(gateCtx context.Context, reader RepoDependencyProjectionIntentReader) error {
			var processErr error
			processed, active, writtenGroups, processErr = r.processAcceptanceUnit(
				gateCtx, now, acceptanceUnitID, reader, result, cycleStart,
			)
			return processErr
		},
	)
	if gateErr != nil {
		return processed, r.quarantineLease(gateErr)
	}
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		return processed, r.quarantineLease(heartbeatErr)
	}
	if !ran {
		result.LeaseAcquired = true
		releaseLease = true
		return result, nil
	}
	if len(active) > 0 {
		r.recordRepoDependencyCycle(ctx, acceptanceUnitID, active, processed.UpsertedRows, writtenGroups, cycleStart, processed)
	}
	releaseLease = true
	return processed, nil
}

func (r *RepoDependencyProjectionRunner) quarantineLease(err error) error {
	if err == nil {
		return nil
	}
	return &repoDependencyLeaseQuarantineError{delay: r.Config.leaseTTL(), cause: err}
}

// startLeaseHeartbeat renews the source-repo lane lease while graph writes are
// in flight so slow backend calls cannot make active work appear abandoned.
func (r *RepoDependencyProjectionRunner) startLeaseHeartbeat(ctx context.Context) (context.Context, func() error) {
	interval := repoDependencyLeaseHeartbeatInterval(r.Config.leaseTTL())
	if interval <= 0 {
		return ctx, func() error { return nil }
	}
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	stopped := make(chan struct{})
	var failureMu sync.Mutex
	var failure error
	var stopping atomic.Bool
	var once sync.Once
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				claimed, err := r.LeaseManager.ClaimPartitionLease(
					heartbeatCtx,
					DomainRepoDependency,
					r.Config.partitionID(),
					r.Config.partitionCount(),
					r.Config.leaseOwner(),
					r.Config.leaseTTL(),
				)
				if err != nil || !claimed {
					if stopping.Load() {
						return
					}
					if err == nil {
						err = errors.New("repo dependency lease heartbeat lost ownership")
					}
					failureMu.Lock()
					failure = fmt.Errorf("repo dependency lease heartbeat failed: %w", err)
					failureMu.Unlock()
					if r.Logger != nil {
						attrs := []any{
							log.Domain(DomainRepoDependency),
							telemetry.PhaseAttr(telemetry.PhaseReduction),
						}
						if err != nil {
							attrs = append(attrs, log.Err(err))
						}
						r.Logger.WarnContext(heartbeatCtx, "repo dependency lease heartbeat failed", attrs...)
					}
					cancel()
					return
				}
			}
		}
	}()
	var stopErr error
	return heartbeatCtx, func() error {
		once.Do(func() {
			stopping.Store(true)
			close(done)
			cancel()
			<-stopped
			failureMu.Lock()
			stopErr = failure
			failureMu.Unlock()
		})
		return stopErr
	}
}

// repoDependencyLeaseHeartbeatInterval renews before the lease reaches its
// deadline while capping idle wakeups for unusually long lease settings.
func repoDependencyLeaseHeartbeatInterval(leaseTTL time.Duration) time.Duration {
	if leaseTTL <= 0 {
		return 0
	}
	interval := leaseTTL / 3
	if interval <= 0 {
		return leaseTTL
	}
	if interval > time.Minute {
		return time.Minute
	}
	return interval
}

func repoDependencyNeedsRetract(rows []SharedProjectionIntentRow, staleIDs []string) bool {
	if len(staleIDs) > 0 {
		return true
	}
	for _, row := range rows {
		action := strings.TrimSpace(repoDependencyPayloadString(row, "action"))
		if action == "delete" || action == "retract" {
			return true
		}
	}
	return false
}

func (r *RepoDependencyProjectionRunner) selectAcceptanceUnitWork(ctx context.Context) (string, error) {
	scanLimit := r.Config.batchLimit()
	if scanLimit > maxRepoDependencyAcceptanceScanLimit {
		scanLimit = maxRepoDependencyAcceptanceScanLimit
	}

	for {
		pending, err := r.IntentReader.ListPendingDomainIntents(ctx, DomainRepoDependency, scanLimit)
		if err != nil {
			return "", fmt.Errorf("list pending repo dependency intents: %w", err)
		}
		if len(pending) == 0 {
			return "", nil
		}

		lookup := r.AcceptedGen
		if r.AcceptedGenPrefetch != nil {
			resolvedLookup, err := r.AcceptedGenPrefetch(ctx, pending)
			if err != nil {
				return "", fmt.Errorf("prefetch accepted generations: %w", err)
			}
			lookup = resolvedLookup
		}

		acceptedByUnit := make(map[string]bool, len(pending))
		order := make([]string, 0, len(pending))
		for _, row := range pending {
			unitID, ok := repoDependencyAcceptanceUnitID(row)
			if !ok {
				return "", fmt.Errorf("pending repo dependency intent %q is missing acceptance unit", row.IntentID)
			}
			if _, seen := acceptedByUnit[unitID]; !seen {
				order = append(order, unitID)
				acceptedByUnit[unitID] = false
			}
			key, ok := row.AcceptanceKey()
			if !ok {
				return "", fmt.Errorf(
					"pending repo dependency intent %q is missing scope, acceptance unit, or source run",
					row.IntentID,
				)
			}
			acceptedGeneration, ok := lookup(key)
			if !ok {
				continue
			}
			if strings.TrimSpace(row.GenerationID) == strings.TrimSpace(acceptedGeneration) {
				acceptedByUnit[unitID] = true
			}
		}

		for _, unitID := range order {
			if acceptedByUnit[unitID] {
				return unitID, nil
			}
		}

		if len(pending) < scanLimit {
			return "", nil
		}
		if scanLimit >= maxRepoDependencyAcceptanceScanLimit {
			return "", fmt.Errorf(
				"repo dependency acceptance scan reached cap (%d) before locating accepted work",
				maxRepoDependencyAcceptanceScanLimit,
			)
		}

		nextLimit := scanLimit * 2
		if nextLimit > maxRepoDependencyAcceptanceScanLimit {
			nextLimit = maxRepoDependencyAcceptanceScanLimit
		}
		scanLimit = nextLimit
	}
}

func (r *RepoDependencyProjectionRunner) loadAllAcceptanceUnitIntents(
	ctx context.Context,
	reader RepoDependencyProjectionIntentReader,
	acceptanceUnitID string,
) ([]SharedProjectionIntentRow, error) {
	limit := r.Config.batchLimit()
	if limit > maxRepoDependencyAcceptanceScanLimit {
		limit = maxRepoDependencyAcceptanceScanLimit
	}
	for {
		rows, err := reader.ListAcceptanceUnitDomainIntents(ctx, acceptanceUnitID, DomainRepoDependency, limit)
		if err != nil {
			return nil, fmt.Errorf("list repo dependency acceptance intents: %w", err)
		}
		if len(rows) < limit {
			return rows, nil
		}
		if limit >= maxRepoDependencyAcceptanceScanLimit {
			return nil, fmt.Errorf(
				"repo dependency acceptance intent scan reached cap (%d) for unit %q",
				maxRepoDependencyAcceptanceScanLimit,
				acceptanceUnitID,
			)
		}
		nextLimit := limit * 2
		if nextLimit > maxRepoDependencyAcceptanceScanLimit {
			nextLimit = maxRepoDependencyAcceptanceScanLimit
		}
		limit = nextLimit
	}
}

func (r *RepoDependencyProjectionRunner) retractRepo(ctx context.Context, rows []SharedProjectionIntentRow) (int, error) {
	retractRows := buildRepoDependencyRetractRows(uniqueRepositoryIDs(rows))
	if len(retractRows) == 0 {
		return 0, nil
	}
	sources := repoDependencyEvidenceSources(rows)
	for _, source := range sources {
		if err := r.EdgeWriter.RetractEdges(ctx, DomainRepoDependency, retractRows, source); err != nil {
			return 0, fmt.Errorf("retract repo dependency edges for %s: %w", source, err)
		}
	}
	return len(retractRows) * len(sources), nil
}

func (r *RepoDependencyProjectionRunner) writeActiveRows(ctx context.Context, rows []SharedProjectionIntentRow) (int, int, error) {
	groups := groupRepoDependencyUpsertRows(rows)
	if len(groups) == 0 {
		return 0, 0, nil
	}

	sources := make([]string, 0, len(groups))
	for source := range groups {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	writtenRows := 0
	for _, source := range sources {
		group := groups[source]
		if len(group) == 0 {
			continue
		}
		if err := r.EdgeWriter.WriteEdges(ctx, DomainRepoDependency, group, source); err != nil {
			return 0, 0, fmt.Errorf("write repo dependency edges for %s: %w", source, err)
		}
		writtenRows += len(group)
	}
	return writtenRows, len(sources), nil
}
