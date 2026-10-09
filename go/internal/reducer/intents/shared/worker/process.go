// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// IntentReader reads and marks shared projection intents.
type IntentReader interface {
	ListPendingDomainIntents(ctx context.Context, domain string, limit int) ([]sharedintent.Row, error)
	MarkIntentsCompleted(ctx context.Context, intentIDs []string, completedAt time.Time) error
}

// PartitionProcessorConfig holds configuration for one partition processor
// cycle.
type PartitionProcessorConfig struct {
	Domain         string
	PartitionID    int
	PartitionCount int
	LeaseOwner     string
	LeaseTTL       time.Duration
	BatchLimit     int
	EvidenceSource string

	// Instruments and Logger are optional telemetry sinks for the partition
	// lease heartbeat loop (#4449). A nil Instruments disables the
	// eshu_dp_shared_projection_partition_heartbeat_missed_total counter; a
	// nil Logger disables the heartbeat-failure log line. Neither is
	// required for the heartbeat renewal itself to run.
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// PartitionProcessResult captures the outcome of one partition processing
// cycle.
type PartitionProcessResult struct {
	LeaseAcquired    bool
	ProcessedIntents int
	UpsertedRows     int
	RetractedRows    int
	StaleIntents     int
	// SupersededGenerationIntents is the subset of StaleIntents drained because
	// the intent's scope generation is superseded (#7121). The rest are
	// acceptance mismatches.
	SupersededGenerationIntents int
	// CoveredByFullSuccessorIntents is the subset of StaleIntents drained
	// because the intent's superseded generation is covered by a newer
	// emitted full generation (#7165). Only the code_calls and
	// repo_dependency lanes set it; the shared runner leaves it zero.
	CoveredByFullSuccessorIntents     int
	BlockedReadiness                  int
	MaxIntentWaitSeconds              float64
	MaxBlockedIntentWaitSeconds       float64
	LeaseClaimDurationSeconds         float64
	SelectionDurationSeconds          float64
	LoadAllDurationSeconds            float64
	AcceptancePrefetchDurationSeconds float64
	SelectionPhases                   SelectionPhaseDurations
	ProcessingDurationSeconds         float64
	RetractDurationSeconds            float64
	WriteDurationSeconds              float64
	ReplayDurationSeconds             float64
	MarkCompletedDurationSeconds      float64
	ActiveIntents                     int
	AcceptanceUnitRows                int
	ReplayRequests                    int
	IndexedSelection                  bool
	UnhashedFallbackRows              int
	// TerminalNoEndpoint counts symbol→runtime rows drained with no edge because
	// their runtime target will never commit: handles_route on an absent
	// (repo_id, path) :Endpoint (#2809), runs_in on a repo with no :Workload
	// (#2855). A non-zero value during steady state is the operator signal for
	// handlers whose target was not materialized — distinct from readiness-blocked
	// rows. The runner logs the originating `domain` alongside the count.
	TerminalNoEndpoint int
	// RefreshFenceDeferred counts per-edge rows held this cycle by the repo-wide
	// retract fence (#2898): their repo's single repo-wide retract (the per-repo
	// refresh intent) has not completed yet, so writing now could be wiped. They
	// are left pending and re-selected next cycle. A persistently non-zero value
	// for a repo means its refresh intent is not completing — a stall signal,
	// distinct from readiness-blocked and terminal-no-endpoint.
	RefreshFenceDeferred int
	// SelectionRounds counts the widen passes SelectPartitionBatch ran
	// for this visit, including the final one (#7724 telemetry:
	// per-visit widen rounds).
	SelectionRounds int
	// PrefetchStats accumulates this visit's selection prefetch
	// behavior: per-kind keys, queries, rows, readiness cache hits, and
	// durations (#7724 telemetry: per-visit prefetch stats).
	PrefetchStats sharedintent.PrefetchStats
	// PartitionsVisited counts visits that ran and were neither
	// backoff-skipped nor lease-held: successful visits plus every error
	// visit, including lease-claim errors that acquired no lease and ran
	// no selection. MergePartitionProcessResult sums these three counters
	// across the cycle so the runner can report partitions visited vs
	// skipped by reason (#7724 telemetry: per-cycle visited vs skipped).
	PartitionsVisited int
	// PartitionsBackoffSkipped counts visits skipped by per-partition
	// backoff.
	PartitionsBackoffSkipped int
	// PartitionsLeaseHeld counts visits whose lease claim lost to
	// another owner.
	PartitionsLeaseHeld int
}

// ProcessPartitionOnce processes one partition cycle: claim lease, select
// batch, retract/write edges, mark completed, release lease. Matches the
// Python process_platform_partition_once and process_dependency_partition_once
// functions.
func ProcessPartitionOnce(
	ctx context.Context,
	now time.Time,
	cfg PartitionProcessorConfig,
	leaseManager sharedintent.PartitionLeaseManager,
	reader IntentReader,
	edgeWriter sharedintent.EdgeWriter,
	acceptedGen sharedintent.AcceptedGenerationLookup,
	prefetch sharedintent.AcceptedGenerationPrefetch,
	readinessLookup gpphase.ReadinessLookup,
	readinessPrefetch gpphase.ReadinessPrefetch,
	endpointPresence gpphase.EndpointPresenceLookup,
	refreshFence RefreshFenceLookup,
	firstProjection FirstProjectionLookup,
	unroutableWriter UnroutableWriter,
) (result PartitionProcessResult, retErr error) {
	leaseStart := time.Now()
	claimed, err := leaseManager.ClaimPartitionLease(
		ctx, cfg.Domain, cfg.PartitionID, cfg.PartitionCount,
		cfg.LeaseOwner, cfg.LeaseTTL,
	)
	leaseDuration := time.Since(leaseStart).Seconds()
	if err != nil {
		return PartitionProcessResult{}, fmt.Errorf("claim lease: %w", err)
	}
	if !claimed {
		return PartitionProcessResult{LeaseAcquired: false, LeaseClaimDurationSeconds: leaseDuration}, nil
	}

	// Renew the partition lease at TTL/2 for the rest of this cycle (#4449).
	// Without this, a slow backend or large partition whose
	// selection/retract/edge-write/mark-completed work exceeds the lease TTL
	// lets the lease be reclaimed by another worker while this call is still
	// writing, causing a double-write.
	//
	// releaseCtx is the pre-heartbeat context, deliberately NOT the
	// heartbeat-derived leaseCtx assigned to ctx below. stopHeartbeat()
	// cancels leaseCtx before this defer's ReleasePartitionLease call runs,
	// so releasing through leaseCtx (or a ctx variable reassigned to it)
	// would hand Postgres an already-cancelled context: the release query
	// fails, the error is swallowed, and the lease sits held until it
	// expires on its own TTL -- defeating the point of releasing early.
	releaseCtx := ctx
	leaseCtx, stopHeartbeat := startSharedProjectionLeaseHeartbeat(ctx, cfg, leaseManager, cfg.Instruments, cfg.Logger)
	defer func() {
		// stopHeartbeat() already wraps a claim/rejection failure in
		// "heartbeat shared projection partition lease: ...";
		// re-wrapping here would double the prefix.
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			if retErr == nil {
				retErr = heartbeatErr
			} else {
				retErr = errors.Join(retErr, heartbeatErr)
			}
		}
		_ = leaseManager.ReleasePartitionLease(
			releaseCtx, cfg.Domain, cfg.PartitionID, cfg.PartitionCount, cfg.LeaseOwner,
		)
	}()
	ctx = leaseCtx

	batchLimit := cfg.BatchLimit
	if batchLimit < 1 {
		batchLimit = 100
	}

	selectionStart := time.Now()
	batch, err := SelectPartitionBatch(
		ctx, reader, cfg.Domain,
		cfg.PartitionID, cfg.PartitionCount,
		batchLimit, acceptedGen, prefetch,
		readinessLookup, readinessPrefetch,
		endpointPresence,
	)
	selectionDuration := time.Since(selectionStart).Seconds()
	if err != nil {
		return PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseDuration,
			SelectionDurationSeconds:  selectionDuration,
			SelectionRounds:           batch.SelectionRounds,
			PrefetchStats:             batch.PrefetchStats,
		}, fmt.Errorf("select batch: %w", err)
	}

	if len(batch.LatestRows) == 0 && len(batch.TerminalRows) == 0 && len(batch.StaleIDs) == 0 && len(batch.SupersededIDs) == 0 {
		return PartitionProcessResult{
			LeaseAcquired:               true,
			BlockedReadiness:            batch.BlockedCount,
			MaxBlockedIntentWaitSeconds: MaxIntentWaitSeconds(now, batch.BlockedRows),
			LeaseClaimDurationSeconds:   leaseDuration,
			SelectionDurationSeconds:    selectionDuration,
			IndexedSelection:            batch.IndexedSelection,
			UnhashedFallbackRows:        batch.UnhashedFallbackRows,
			SelectionRounds:             batch.SelectionRounds,
			PrefetchStats:               batch.PrefetchStats,
		}, nil
	}

	evidenceSource := cfg.EvidenceSource
	if evidenceSource == "" {
		evidenceSource = "finalization/workloads"
	}

	rwPlan, planErr := resolveRetractWriteRows(ctx, cfg, batch, refreshFence, firstProjection)
	if planErr != nil {
		return PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseDuration,
			SelectionDurationSeconds:  selectionDuration,
			SelectionRounds:           batch.SelectionRounds,
			PrefetchStats:             batch.PrefetchStats,
		}, planErr
	}
	retractRows, writeRows := rwPlan.retractRows, rwPlan.writeRows
	completedLatestRows, deferred := rwPlan.completedLatestRows, rwPlan.deferred

	processingStart := time.Now()
	writeResult, err := writeEdgesAndUnroutable(ctx, cfg, edgeWriter, unroutableWriter, retractRows, writeRows, evidenceSource)
	if err != nil {
		return PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseDuration,
			SelectionDurationSeconds:  selectionDuration,
			SelectionRounds:           batch.SelectionRounds,
			PrefetchStats:             batch.PrefetchStats,
		}, err
	}
	retractDuration, writeDuration, upsertRows := writeResult.retractDuration, writeResult.writeDuration, writeResult.upsertRows

	processedIDs, markCompletedDuration, err := markSelectedBatchCompleted(ctx, reader, now, batch, completedLatestRows)
	if err != nil {
		return PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseDuration,
			SelectionDurationSeconds:  selectionDuration,
			SelectionRounds:           batch.SelectionRounds,
			PrefetchStats:             batch.PrefetchStats,
		}, err
	}
	processingDuration := time.Since(processingStart).Seconds()

	return PartitionProcessResult{
		LeaseAcquired:                true,
		ProcessedIntents:             len(processedIDs),
		UpsertedRows:                 len(upsertRows),
		RetractedRows:                len(retractRows),
		StaleIntents:                 len(batch.StaleIDs),
		SupersededGenerationIntents:  batch.SupersededGenerationCount,
		BlockedReadiness:             batch.BlockedCount + deferred,
		RefreshFenceDeferred:         deferred,
		MaxIntentWaitSeconds:         MaxIntentWaitSeconds(now, batch.LatestRows),
		MaxBlockedIntentWaitSeconds:  MaxIntentWaitSeconds(now, batch.BlockedRows),
		LeaseClaimDurationSeconds:    leaseDuration,
		SelectionDurationSeconds:     selectionDuration,
		ProcessingDurationSeconds:    processingDuration,
		RetractDurationSeconds:       retractDuration,
		WriteDurationSeconds:         writeDuration,
		MarkCompletedDurationSeconds: markCompletedDuration,
		IndexedSelection:             batch.IndexedSelection,
		UnhashedFallbackRows:         batch.UnhashedFallbackRows,
		TerminalNoEndpoint:           len(batch.TerminalRows),
		SelectionRounds:              batch.SelectionRounds,
		PrefetchStats:                batch.PrefetchStats,
	}, nil
}

// markSelectedBatchCompleted assembles the completed intent ids — stale,
// superseded, projected-latest, and terminal — and marks them completed,
// returning the ids and the mark duration. Terminal rows drain with no
// edge (never deferred), so a route-only backlog drains instead of
// re-enqueuing forever (#2809).
func markSelectedBatchCompleted(
	ctx context.Context,
	reader IntentReader,
	now time.Time,
	batch PartitionBatchResult,
	completedLatestRows []sharedintent.Row,
) (processedIDs []string, markCompletedDuration float64, err error) {
	processedIDs = append(processedIDs, batch.StaleIDs...)
	processedIDs = append(processedIDs, batch.SupersededIDs...)
	for _, row := range completedLatestRows {
		processedIDs = append(processedIDs, row.IntentID)
	}
	for _, row := range batch.TerminalRows {
		processedIDs = append(processedIDs, row.IntentID)
	}

	if len(processedIDs) > 0 {
		markStart := time.Now()
		if err := reader.MarkIntentsCompleted(ctx, processedIDs, now); err != nil {
			return nil, 0, fmt.Errorf("mark completed: %w", err)
		}
		markCompletedDuration = time.Since(markStart).Seconds()
	}
	return processedIDs, markCompletedDuration, nil
}
