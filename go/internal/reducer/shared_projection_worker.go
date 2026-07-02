// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const maxSharedSelectionScanLimit = 10_000

// SharedProjectionEdgeWriter writes and retracts canonical graph edges for one
// shared projection domain.
type SharedProjectionEdgeWriter interface {
	RetractEdges(ctx context.Context, domain string, rows []SharedProjectionIntentRow, evidenceSource string) error
	WriteEdges(ctx context.Context, domain string, rows []SharedProjectionIntentRow, evidenceSource string) error
}

// PartitionLeaseManager manages partition leases for shared projection workers.
type PartitionLeaseManager interface {
	ClaimPartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string, leaseTTL time.Duration) (bool, error)
	ReleasePartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string) error
}

// SharedIntentReader reads and marks shared projection intents.
type SharedIntentReader interface {
	ListPendingDomainIntents(ctx context.Context, domain string, limit int) ([]SharedProjectionIntentRow, error)
	MarkIntentsCompleted(ctx context.Context, intentIDs []string, completedAt time.Time) error
}

// AcceptedGenerationLookup returns the accepted generation for one bounded
// acceptance key. Returns empty string and false when no accepted generation is
// known.
type AcceptedGenerationLookup func(key SharedProjectionAcceptanceKey) (string, bool)

// AcceptedGenerationPrefetch batches acceptance resolution for a set of
// intents and returns an in-memory lookup closure for the current cycle.
type AcceptedGenerationPrefetch func(ctx context.Context, intents []SharedProjectionIntentRow) (AcceptedGenerationLookup, error)

// PartitionBatchResult holds the result of selecting one partition batch.
type PartitionBatchResult struct {
	LatestRows  []SharedProjectionIntentRow
	BlockedRows []SharedProjectionIntentRow
	// TerminalRows are phase-ready rows that are complete with no edge — the
	// handles_route #2809 terminal-no-endpoint set. They are retracted (to clear
	// any stale edge whose endpoint vanished) and marked complete, but never
	// written and never deferred, so a route-only repo cannot stall the backlog.
	TerminalRows  []SharedProjectionIntentRow
	StaleIDs      []string
	StaleCount    int
	SupersededIDs []string
	BlockedCount  int
	TerminalCount int
	// IndexedSelection is true when candidates were read through the indexed
	// partition predicate rather than the in-memory domain scan. It is a bounded
	// operator signal for diagnosing which selection path a domain used.
	IndexedSelection bool
	// UnhashedFallbackRows counts legacy partition-matched rows from the unhashed
	// lane that were selected into this cycle's candidate batch (after limit
	// truncation). A non-zero value during steady state means pre-hash rows are
	// still draining for the domain.
	UnhashedFallbackRows int
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
	LeaseAcquired                     bool
	ProcessedIntents                  int
	UpsertedRows                      int
	RetractedRows                     int
	StaleIntents                      int
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
}

// SelectPartitionBatch selects one accepted partition batch, matching the
// Python _select_partition_batch function. It scans pending intents, filters
// by partition, checks authoritative generation state, and deduplicates to
// latest per repo/partition pair.
func SelectPartitionBatch(
	ctx context.Context,
	reader SharedIntentReader,
	domain string,
	partitionID, partitionCount int,
	batchLimit int,
	acceptedGen AcceptedGenerationLookup,
	prefetch AcceptedGenerationPrefetch,
	readinessLookup GraphProjectionReadinessLookup,
	readinessPrefetch GraphProjectionReadinessPrefetch,
	endpointPresence EndpointPresenceLookup,
) (PartitionBatchResult, error) {
	if batchLimit < 1 {
		batchLimit = 1
	}

	// Indexed candidate readers (Postgres) return only this partition's pending
	// rows, so the scan never dilutes across partitions and cannot starve at the
	// scan cap. Readers without the candidate interface keep the in-memory
	// domain scan with its widen-and-cap behavior unchanged.
	_, indexed := reader.(SharedProjectionPartitionCandidateReader)

	scanLimit := batchLimit * max(partitionCount, 1) * 2
	if scanLimit > maxSharedSelectionScanLimit {
		scanLimit = maxSharedSelectionScanLimit
	}

	for {
		if err := ctx.Err(); err != nil {
			return PartitionBatchResult{}, err
		}

		partitionRows, loadedCount, unhashedFallback, err := loadPartitionRows(
			ctx, reader, domain, partitionID, partitionCount, scanLimit, indexed,
		)
		if err != nil {
			return PartitionBatchResult{}, err
		}

		seenAll := loadedCount < scanLimit
		if len(partitionRows) == 0 {
			if seenAll {
				return PartitionBatchResult{IndexedSelection: indexed}, nil
			}
			if scanLimit >= maxSharedSelectionScanLimit {
				if indexed {
					return PartitionBatchResult{IndexedSelection: indexed}, nil
				}
				return PartitionBatchResult{}, scanCapError(domain, partitionID, partitionCount)
			}
			scanLimit = widenScanLimit(scanLimit)
			continue
		}

		lookup := acceptedGen
		if prefetch != nil {
			resolvedLookup, err := prefetch(ctx, partitionRows)
			if err != nil {
				return PartitionBatchResult{}, fmt.Errorf("prefetch accepted generations: %w", err)
			}
			lookup = resolvedLookup
		}

		active, staleIDs := FilterAuthoritativeIntents(partitionRows, lookup)
		latest, supersededIDs := LatestIntentsByRepoAndPartition(active)
		readyRows, blockedRows, terminalRows, err := filterRowsByReadiness(
			ctx,
			domain,
			latest,
			readinessLookup,
			readinessPrefetch,
			endpointPresence,
		)
		if err != nil {
			return PartitionBatchResult{}, err
		}

		// Terminal rows are complete with no edge; draining them promptly (rather
		// than widening the scan in search of more ready rows) is what keeps a
		// route-only backlog from stalling, so they count toward returning a batch.
		if len(readyRows) >= batchLimit || len(terminalRows) > 0 || seenAll {
			if len(readyRows) > batchLimit {
				readyRows = readyRows[:batchLimit]
			}
			return PartitionBatchResult{
				LatestRows:           readyRows,
				BlockedRows:          blockedRows,
				TerminalRows:         terminalRows,
				StaleIDs:             staleIDs,
				StaleCount:           len(staleIDs),
				SupersededIDs:        supersededIDs,
				BlockedCount:         len(blockedRows),
				TerminalCount:        len(terminalRows),
				IndexedSelection:     indexed,
				UnhashedFallbackRows: unhashedFallback,
			}, nil
		}

		if scanLimit >= maxSharedSelectionScanLimit {
			if indexed {
				return PartitionBatchResult{
					LatestRows:           readyRows,
					BlockedRows:          blockedRows,
					TerminalRows:         terminalRows,
					StaleIDs:             staleIDs,
					StaleCount:           len(staleIDs),
					SupersededIDs:        supersededIDs,
					BlockedCount:         len(blockedRows),
					TerminalCount:        len(terminalRows),
					IndexedSelection:     indexed,
					UnhashedFallbackRows: unhashedFallback,
				}, nil
			}
			return PartitionBatchResult{}, scanCapError(domain, partitionID, partitionCount)
		}
		scanLimit = widenScanLimit(scanLimit)
	}
}

// ProcessPartitionOnce processes one partition cycle: claim lease, select
// batch, retract/write edges, mark completed, release lease. Matches the
// Python process_platform_partition_once and process_dependency_partition_once
// functions.
func ProcessPartitionOnce(
	ctx context.Context,
	now time.Time,
	cfg PartitionProcessorConfig,
	leaseManager PartitionLeaseManager,
	reader SharedIntentReader,
	edgeWriter SharedProjectionEdgeWriter,
	acceptedGen AcceptedGenerationLookup,
	prefetch AcceptedGenerationPrefetch,
	readinessLookup GraphProjectionReadinessLookup,
	readinessPrefetch GraphProjectionReadinessPrefetch,
	endpointPresence EndpointPresenceLookup,
	refreshFence SharedProjectionRefreshFenceLookup,
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
		}, fmt.Errorf("select batch: %w", err)
	}

	if len(batch.LatestRows) == 0 && len(batch.TerminalRows) == 0 && len(batch.StaleIDs) == 0 && len(batch.SupersededIDs) == 0 {
		return PartitionProcessResult{
			LeaseAcquired:               true,
			BlockedReadiness:            batch.BlockedCount,
			MaxBlockedIntentWaitSeconds: maxSharedIntentWaitSeconds(now, batch.BlockedRows),
			LeaseClaimDurationSeconds:   leaseDuration,
			SelectionDurationSeconds:    selectionDuration,
			IndexedSelection:            batch.IndexedSelection,
			UnhashedFallbackRows:        batch.UnhashedFallbackRows,
		}, nil
	}

	evidenceSource := cfg.EvidenceSource
	if evidenceSource == "" {
		evidenceSource = "finalization/workloads"
	}

	// Retract over the ready AND terminal rows: retraction is repo-scoped, so a
	// repo whose only handles_route rows are terminal (every endpoint absent) must
	// still contribute its repo_id to clear a stale edge from a prior generation
	// when the endpoint has since vanished. Writes re-add the ready rows only.
	retractRows := batch.LatestRows
	if len(batch.TerminalRows) > 0 {
		retractRows = make([]SharedProjectionIntentRow, 0, len(batch.LatestRows)+len(batch.TerminalRows))
		retractRows = append(retractRows, batch.LatestRows...)
		retractRows = append(retractRows, batch.TerminalRows...)
	}
	writeRows := batch.LatestRows
	completedLatestRows := batch.LatestRows
	deferred := 0

	// Repo-wide-retract domains (#2898/#2910): when a fence is wired, the single
	// repo-wide retract is owned by the per-repo refresh intent and per-edge rows
	// write only after that retract has committed. This removes the per-partition
	// repo-wide retract that wipes sibling partitions' edges. Other domains and the
	// nil-fence path keep the retract-then-write-everything behavior byte-identical.
	if refreshFence != nil && domainHasRepoWideRetract(cfg.Domain) {
		plan, planErr := planRepoWideRetractWork(ctx, cfg.Domain, batch.LatestRows, refreshFence)
		if planErr != nil {
			return PartitionProcessResult{
				LeaseAcquired:             true,
				LeaseClaimDurationSeconds: leaseDuration,
				SelectionDurationSeconds:  selectionDuration,
			}, planErr
		}
		retractRows = plan.retractRows
		writeRows = plan.writeRows
		completedLatestRows = plan.completedRows
		deferred = plan.deferred
	}

	processingStart := time.Now()
	retractStart := time.Now()
	if err := edgeWriter.RetractEdges(ctx, cfg.Domain, retractRows, evidenceSource); err != nil {
		return PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseDuration,
			SelectionDurationSeconds:  selectionDuration,
		}, fmt.Errorf("retract edges: %w", err)
	}
	retractDuration := time.Since(retractStart).Seconds()

	upsertRows := filterUpsertRows(writeRows)
	writeStart := time.Now()
	if err := edgeWriter.WriteEdges(ctx, cfg.Domain, upsertRows, evidenceSource); err != nil {
		return PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseDuration,
			SelectionDurationSeconds:  selectionDuration,
		}, fmt.Errorf("write edges: %w", err)
	}
	writeDuration := time.Since(writeStart).Seconds()

	var processedIDs []string
	processedIDs = append(processedIDs, batch.StaleIDs...)
	processedIDs = append(processedIDs, batch.SupersededIDs...)
	for _, row := range completedLatestRows {
		processedIDs = append(processedIDs, row.IntentID)
	}
	// Terminal rows are completed with no edge (drained, never deferred), so the
	// route-only backlog drains instead of re-enqueuing forever (#2809).
	for _, row := range batch.TerminalRows {
		processedIDs = append(processedIDs, row.IntentID)
	}

	var markCompletedDuration float64
	if len(processedIDs) > 0 {
		markStart := time.Now()
		if err := reader.MarkIntentsCompleted(ctx, processedIDs, now); err != nil {
			return PartitionProcessResult{
				LeaseAcquired:             true,
				LeaseClaimDurationSeconds: leaseDuration,
				SelectionDurationSeconds:  selectionDuration,
			}, fmt.Errorf("mark completed: %w", err)
		}
		markCompletedDuration = time.Since(markStart).Seconds()
	}
	processingDuration := time.Since(processingStart).Seconds()

	return PartitionProcessResult{
		LeaseAcquired:                true,
		ProcessedIntents:             len(processedIDs),
		UpsertedRows:                 len(upsertRows),
		RetractedRows:                len(retractRows),
		StaleIntents:                 len(batch.StaleIDs),
		BlockedReadiness:             batch.BlockedCount + deferred,
		RefreshFenceDeferred:         deferred,
		MaxIntentWaitSeconds:         maxSharedIntentWaitSeconds(now, batch.LatestRows),
		MaxBlockedIntentWaitSeconds:  maxSharedIntentWaitSeconds(now, batch.BlockedRows),
		LeaseClaimDurationSeconds:    leaseDuration,
		SelectionDurationSeconds:     selectionDuration,
		ProcessingDurationSeconds:    processingDuration,
		RetractDurationSeconds:       retractDuration,
		WriteDurationSeconds:         writeDuration,
		MarkCompletedDurationSeconds: markCompletedDuration,
		IndexedSelection:             batch.IndexedSelection,
		UnhashedFallbackRows:         batch.UnhashedFallbackRows,
		TerminalNoEndpoint:           len(batch.TerminalRows),
	}, nil
}
