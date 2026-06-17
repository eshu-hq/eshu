package reducer

import (
	"context"
	"fmt"
	"sort"
	"time"
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
	LatestRows    []SharedProjectionIntentRow
	BlockedRows   []SharedProjectionIntentRow
	StaleIDs      []string
	StaleCount    int
	SupersededIDs []string
	BlockedCount  int
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
}

// LatestIntentsByRepoAndPartition deduplicates intents to the most recent per
// bounded acceptance key and partition, matching the Python
// _latest_intents_by_repo_and_partition function.
func LatestIntentsByRepoAndPartition(intents []SharedProjectionIntentRow) ([]SharedProjectionIntentRow, []string) {
	if len(intents) == 0 {
		return nil, nil
	}

	sorted := make([]SharedProjectionIntentRow, len(intents))
	copy(sorted, intents)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
		}
		return sorted[i].IntentID < sorted[j].IntentID
	})

	type repoPartitionKey struct {
		scopeID          string
		acceptanceUnitID string
		sourceRunID      string
		repositoryID     string
		partitionKey     string
	}

	latestByKey := make(map[repoPartitionKey]SharedProjectionIntentRow)
	order := make([]repoPartitionKey, 0)
	var supersededIDs []string

	for _, intent := range sorted {
		k := repoPartitionKey{
			scopeID:      intent.ScopeID,
			sourceRunID:  intent.SourceRunID,
			repositoryID: intent.RepositoryID,
			partitionKey: intent.PartitionKey,
		}
		if acceptanceKey, ok := intent.AcceptanceKey(); ok {
			k.scopeID = acceptanceKey.ScopeID
			k.acceptanceUnitID = acceptanceKey.AcceptanceUnitID
			k.sourceRunID = acceptanceKey.SourceRunID
		}
		if prev, ok := latestByKey[k]; ok {
			supersededIDs = append(supersededIDs, prev.IntentID)
		} else {
			order = append(order, k)
		}
		latestByKey[k] = intent
	}

	result := make([]SharedProjectionIntentRow, 0, len(order))
	for _, k := range order {
		result = append(result, latestByKey[k])
	}

	return result, supersededIDs
}

// FilterAuthoritativeIntents splits intents into active (matching accepted
// generation) and stale (mismatching generation) sets, matching the Python
// _filter_authoritative_intents function.
func FilterAuthoritativeIntents(
	intents []SharedProjectionIntentRow,
	acceptedGen AcceptedGenerationLookup,
) (active []SharedProjectionIntentRow, staleIDs []string) {
	for _, intent := range intents {
		key, ok := intent.AcceptanceKey()
		if !ok {
			continue
		}

		accepted, ok := acceptedGen(key)
		if !ok {
			continue
		}
		if intent.GenerationID != accepted {
			staleIDs = append(staleIDs, intent.IntentID)
			continue
		}
		active = append(active, intent)
	}
	return active, staleIDs
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
		readyRows, blockedRows, err := filterRowsByReadiness(
			ctx,
			domain,
			latest,
			readinessLookup,
			readinessPrefetch,
		)
		if err != nil {
			return PartitionBatchResult{}, err
		}

		if len(readyRows) >= batchLimit || seenAll {
			if len(readyRows) > batchLimit {
				readyRows = readyRows[:batchLimit]
			}
			return PartitionBatchResult{
				LatestRows:           readyRows,
				BlockedRows:          blockedRows,
				StaleIDs:             staleIDs,
				StaleCount:           len(staleIDs),
				SupersededIDs:        supersededIDs,
				BlockedCount:         len(blockedRows),
				IndexedSelection:     indexed,
				UnhashedFallbackRows: unhashedFallback,
			}, nil
		}

		if scanLimit >= maxSharedSelectionScanLimit {
			if indexed {
				return PartitionBatchResult{
					LatestRows:           readyRows,
					BlockedRows:          blockedRows,
					StaleIDs:             staleIDs,
					StaleCount:           len(staleIDs),
					SupersededIDs:        supersededIDs,
					BlockedCount:         len(blockedRows),
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
) (PartitionProcessResult, error) {
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

	defer func() {
		_ = leaseManager.ReleasePartitionLease(
			ctx, cfg.Domain, cfg.PartitionID, cfg.PartitionCount, cfg.LeaseOwner,
		)
	}()

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
	)
	selectionDuration := time.Since(selectionStart).Seconds()
	if err != nil {
		return PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseDuration,
			SelectionDurationSeconds:  selectionDuration,
		}, fmt.Errorf("select batch: %w", err)
	}

	if len(batch.LatestRows) == 0 && len(batch.StaleIDs) == 0 && len(batch.SupersededIDs) == 0 {
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

	processingStart := time.Now()
	retractStart := time.Now()
	if err := edgeWriter.RetractEdges(ctx, cfg.Domain, batch.LatestRows, evidenceSource); err != nil {
		return PartitionProcessResult{
			LeaseAcquired:             true,
			LeaseClaimDurationSeconds: leaseDuration,
			SelectionDurationSeconds:  selectionDuration,
		}, fmt.Errorf("retract edges: %w", err)
	}
	retractDuration := time.Since(retractStart).Seconds()

	upsertRows := filterUpsertRows(batch.LatestRows)
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
	for _, row := range batch.LatestRows {
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
		RetractedRows:                len(batch.LatestRows),
		StaleIntents:                 len(batch.StaleIDs),
		BlockedReadiness:             batch.BlockedCount,
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
	}, nil
}
