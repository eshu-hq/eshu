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
	ProcessingDurationSeconds         float64
	RetractDurationSeconds            float64
	WriteDurationSeconds              float64
	ReplayDurationSeconds             float64
	MarkCompletedDurationSeconds      float64
	ActiveIntents                     int
	AcceptanceUnitRows                int
	ReplayRequests                    int
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

	scanLimit := batchLimit * max(partitionCount, 1) * 2
	if scanLimit > maxSharedSelectionScanLimit {
		scanLimit = maxSharedSelectionScanLimit
	}

	for {
		if err := ctx.Err(); err != nil {
			return PartitionBatchResult{}, err
		}

		pending, err := reader.ListPendingDomainIntents(ctx, domain, scanLimit)
		if err != nil {
			return PartitionBatchResult{}, fmt.Errorf("list pending intents: %w", err)
		}

		partitionRows := RowsForPartition(pending, partitionID, partitionCount)
		if len(partitionRows) == 0 {
			if len(pending) < scanLimit {
				return PartitionBatchResult{}, nil
			}
			if scanLimit >= maxSharedSelectionScanLimit {
				return PartitionBatchResult{}, fmt.Errorf(
					"shared partition selection reached scan cap (%d) for domain %q partition %d/%d",
					maxSharedSelectionScanLimit,
					domain,
					partitionID,
					partitionCount,
				)
			}
			nextLimit := scanLimit * 2
			if nextLimit > maxSharedSelectionScanLimit {
				nextLimit = maxSharedSelectionScanLimit
			}
			scanLimit = nextLimit
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

		if len(readyRows) >= batchLimit || len(pending) < scanLimit {
			if len(readyRows) > batchLimit {
				readyRows = readyRows[:batchLimit]
			}
			return PartitionBatchResult{
				LatestRows:    readyRows,
				BlockedRows:   blockedRows,
				StaleIDs:      staleIDs,
				StaleCount:    len(staleIDs),
				SupersededIDs: supersededIDs,
				BlockedCount:  len(blockedRows),
			}, nil
		}

		if scanLimit >= maxSharedSelectionScanLimit {
			return PartitionBatchResult{}, fmt.Errorf(
				"shared partition selection reached scan cap (%d) for domain %q partition %d/%d",
				maxSharedSelectionScanLimit,
				domain,
				partitionID,
				partitionCount,
			)
		}
		nextLimit := scanLimit * 2
		if nextLimit > maxSharedSelectionScanLimit {
			nextLimit = maxSharedSelectionScanLimit
		}
		scanLimit = nextLimit
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
	}, nil
}

func filterRowsByReadiness(
	ctx context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	readinessLookup GraphProjectionReadinessLookup,
	readinessPrefetch GraphProjectionReadinessPrefetch,
) ([]SharedProjectionIntentRow, []SharedProjectionIntentRow, error) {
	phase, gated := sharedProjectionReadinessPhase(domain)
	if !gated || len(rows) == 0 {
		return rows, nil, nil
	}

	lookup := readinessLookup
	if readinessPrefetch != nil {
		seen := make(map[GraphProjectionPhaseKey]struct{}, len(rows))
		keys := make([]GraphProjectionPhaseKey, 0, len(rows))
		for _, row := range rows {
			key, ok := graphProjectionPhaseKeyForIntent(row, row.GenerationID, GraphProjectionKeyspaceCodeEntitiesUID)
			if !ok {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
		resolvedLookup, err := readinessPrefetch(ctx, keys, phase)
		if err != nil {
			return nil, nil, fmt.Errorf("prefetch graph projection readiness: %w", err)
		}
		lookup = resolvedLookup
	}

	if lookup == nil {
		return rows, nil, nil
	}

	readyRows := make([]SharedProjectionIntentRow, 0, len(rows))
	blockedRows := make([]SharedProjectionIntentRow, 0)
	for _, row := range rows {
		key, ok := graphProjectionPhaseKeyForIntent(row, row.GenerationID, GraphProjectionKeyspaceCodeEntitiesUID)
		if !ok {
			continue
		}
		ready, found := lookup(key, phase)
		if !found || !ready {
			blockedRows = append(blockedRows, row)
			continue
		}
		readyRows = append(readyRows, row)
	}
	return readyRows, blockedRows, nil
}

func maxSharedIntentWaitSeconds(now time.Time, rows []SharedProjectionIntentRow) float64 {
	var maxWait float64
	for _, row := range rows {
		if row.CreatedAt.IsZero() {
			continue
		}
		wait := now.Sub(row.CreatedAt).Seconds()
		if wait < 0 {
			wait = 0
		}
		if wait > maxWait {
			maxWait = wait
		}
	}
	return maxWait
}

// filterUpsertRows returns rows whose payload action is "upsert" or absent.
func filterUpsertRows(rows []SharedProjectionIntentRow) []SharedProjectionIntentRow {
	var result []SharedProjectionIntentRow
	for _, row := range rows {
		action, ok := row.Payload["action"]
		if ok {
			if s, isStr := action.(string); isStr && s != "upsert" {
				continue
			}
		}
		result = append(result, row)
	}
	return result
}
