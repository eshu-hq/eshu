package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type codeCallSelectionResult struct {
	Key                         SharedProjectionAcceptanceKey
	PartitionKey                string
	BlockedReadiness            int
	MaxBlockedIntentWaitSeconds float64
	SelectionDurationSeconds    float64
	SelectionPhases             SelectionPhaseDurations
}

type acceptedGenerationCacheEntry struct {
	generationID string
	found        bool
}

type readinessCacheEntry struct {
	ready bool
	found bool
}

func (r *CodeCallProjectionRunner) selectAcceptanceUnitWork(ctx context.Context) (SharedProjectionAcceptanceKey, error) {
	result, err := r.selectAcceptanceUnitWorkWithStats(ctx, time.Now().UTC())
	return result.Key, err
}

func (r *CodeCallProjectionRunner) selectAcceptanceUnitWorkWithStats(
	ctx context.Context,
	now time.Time,
) (codeCallSelectionResult, error) {
	return r.selectAcceptanceUnitPartitionWorkWithStats(ctx, now, 0, 1)
}

func (r *CodeCallProjectionRunner) selectAcceptanceUnitPartitionWorkWithStats(
	ctx context.Context,
	now time.Time,
	partitionID int,
	partitionCount int,
) (codeCallSelectionResult, error) {
	start := time.Now()
	acceptanceTelemetry := sharedAcceptanceTelemetry{
		Instruments: r.Instruments,
		Logger:      r.Logger,
	}
	scanLimit := r.Config.batchLimit()
	acceptanceScanLimit := r.Config.acceptanceScanLimit()
	if scanLimit > acceptanceScanLimit {
		scanLimit = acceptanceScanLimit
	}

	acceptedGenerationsByKey := make(map[SharedProjectionAcceptanceKey]acceptedGenerationCacheEntry)
	readinessByKey := make(map[GraphProjectionPhaseKey]readinessCacheEntry)
	acceptanceRowsByKey := make(map[SharedProjectionAcceptanceKey][]SharedProjectionIntentRow)
	selectionPhases := SelectionPhaseDurations{}
	for {
		candidateLoadStart := time.Now()
		pending, err := r.listPendingPartitionCandidates(ctx, partitionID, partitionCount, scanLimit)
		selectionPhases.CandidateLoadSeconds += time.Since(candidateLoadStart).Seconds()
		if err != nil {
			acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "error",
				Duration: time.Since(start).Seconds(),
				Err:      err,
			})
			return codeCallSelectionResult{}, fmt.Errorf("list pending code call intents: %w", err)
		}
		if len(pending) == 0 {
			acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "miss",
				Duration: time.Since(start).Seconds(),
			})
			return codeCallSelectionResult{
				SelectionDurationSeconds: time.Since(start).Seconds(),
				SelectionPhases:          selectionPhases,
			}, nil
		}

		phase, gated := sharedProjectionReadinessPhase(DomainCodeCalls)
		acceptedByKey := make(map[SharedProjectionAcceptanceKey]string, len(pending))
		missingAcceptedRows := make([]SharedProjectionIntentRow, 0, len(pending))
		seen := make(map[SharedProjectionAcceptanceKey]struct{}, len(pending))
		for _, row := range pending {
			key, ok := row.AcceptanceKey()
			if !ok {
				return codeCallSelectionResult{}, fmt.Errorf(
					"pending code call intent %q is missing scope, acceptance unit, or source run",
					row.IntentID,
				)
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			if cached, ok := acceptedGenerationsByKey[key]; ok {
				if cached.found {
					acceptedByKey[key] = cached.generationID
				}
				continue
			}
			missingAcceptedRows = append(missingAcceptedRows, row)
		}

		if len(missingAcceptedRows) > 0 {
			lookup := r.AcceptedGen
			if r.AcceptedGenPrefetch != nil {
				prefetchStart := time.Now()
				resolvedLookup, err := r.AcceptedGenPrefetch(ctx, missingAcceptedRows)
				selectionPhases.AcceptancePrefetchSeconds += time.Since(prefetchStart).Seconds()
				if err != nil {
					acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
						Runner:   "code_call_projection",
						Result:   "error",
						Duration: time.Since(start).Seconds(),
						Err:      err,
					})
					return codeCallSelectionResult{}, fmt.Errorf("prefetch accepted generations: %w", err)
				}
				lookup = resolvedLookup
			}
			for _, row := range missingAcceptedRows {
				key, _ := row.AcceptanceKey()
				acceptedGeneration, ok := lookup(key)
				acceptedGenerationsByKey[key] = acceptedGenerationCacheEntry{
					generationID: acceptedGeneration,
					found:        ok,
				}
				if ok {
					acceptedByKey[key] = acceptedGeneration
				}
			}
		}

		readinessLookup := r.ReadinessLookup
		if gated && r.ReadinessPrefetch != nil {
			readinessKeys := make([]GraphProjectionPhaseKey, 0, len(acceptedByKey))
			for key, acceptedGeneration := range acceptedByKey {
				readinessKey, ok := graphProjectionPhaseKeyForAcceptance(
					key,
					acceptedGeneration,
					GraphProjectionKeyspaceCodeEntitiesUID,
				)
				if !ok {
					continue
				}
				if _, ok := readinessByKey[readinessKey]; ok {
					continue
				}
				readinessKeys = append(readinessKeys, readinessKey)
			}
			if len(readinessKeys) > 0 {
				readinessPrefetchStart := time.Now()
				resolvedLookup, err := r.ReadinessPrefetch(ctx, readinessKeys, phase)
				selectionPhases.ReadinessPrefetchSeconds += time.Since(readinessPrefetchStart).Seconds()
				if err != nil {
					acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
						Runner:   "code_call_projection",
						Result:   "error",
						Duration: time.Since(start).Seconds(),
						Err:      err,
					})
					return codeCallSelectionResult{}, fmt.Errorf("prefetch graph projection readiness: %w", err)
				}
				for _, readinessKey := range readinessKeys {
					ready, found := resolvedLookup(readinessKey, phase)
					readinessByKey[readinessKey] = readinessCacheEntry{
						ready: ready,
						found: found,
					}
				}
			}
		}

		blockedCount := 0
		maxBlockedWait := 0.0
		for i, row := range pending {
			key, ok := row.AcceptanceKey()
			if !ok {
				return codeCallSelectionResult{}, fmt.Errorf(
					"pending code call intent %q is missing scope, acceptance unit, or source run",
					row.IntentID,
				)
			}
			acceptedGeneration, ok := acceptedByKey[key]
			if !ok {
				continue
			}
			if gated && (readinessLookup != nil || len(readinessByKey) > 0) {
				readinessKey, ok := graphProjectionPhaseKeyForAcceptance(
					key,
					acceptedGeneration,
					GraphProjectionKeyspaceCodeEntitiesUID,
				)
				if !ok {
					continue
				}
				readiness, ok := readinessByKey[readinessKey]
				if !ok {
					if readinessLookup == nil {
						continue
					}
					ready, found := readinessLookup(readinessKey, phase)
					readiness = readinessCacheEntry{
						ready: ready,
						found: found,
					}
					readinessByKey[readinessKey] = readiness
				}
				if !readiness.found || !readiness.ready {
					blockedCount++
					if wait := maxSharedIntentWaitSeconds(now, []SharedProjectionIntentRow{row}); wait > maxBlockedWait {
						maxBlockedWait = wait
					}
					continue
				}
			}
			if !codeCallProjectionPartitionMatches(row, partitionID, partitionCount) {
				continue
			}
			fenceStart := time.Now()
			blocked, err := r.codeCallProjectionRowBlockedByRepoFence(
				ctx,
				row,
				pending,
				i,
				acceptanceRowsByKey,
			)
			selectionPhases.RefreshFenceCheckSeconds += time.Since(fenceStart).Seconds()
			if err != nil {
				acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
					Runner:   "code_call_projection",
					Result:   "error",
					Duration: time.Since(start).Seconds(),
					Err:      err,
				})
				return codeCallSelectionResult{}, err
			}
			if blocked {
				continue
			}

			acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "hit",
				Duration: time.Since(start).Seconds(),
			})
			return codeCallSelectionResult{
				Key:                         key,
				PartitionKey:                row.PartitionKey,
				BlockedReadiness:            blockedCount,
				MaxBlockedIntentWaitSeconds: maxBlockedWait,
				SelectionDurationSeconds:    time.Since(start).Seconds(),
				SelectionPhases:             selectionPhases,
			}, nil
		}

		if blockedCount > 0 && r.Logger != nil {
			r.Logger.InfoContext(
				ctx,
				"code call projection skipped acceptance units until canonical node readiness is committed",
				slog.Int("blocked_count", blockedCount),
				slog.Float64("blocked_intent_wait_seconds", maxBlockedWait),
				slog.String("domain", DomainCodeCalls),
				telemetry.PhaseAttr(telemetry.PhaseShared),
			)
		}

		if len(pending) < scanLimit {
			acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "miss",
				Duration: time.Since(start).Seconds(),
			})
			return codeCallSelectionResult{
				BlockedReadiness:            blockedCount,
				MaxBlockedIntentWaitSeconds: maxBlockedWait,
				SelectionDurationSeconds:    time.Since(start).Seconds(),
				SelectionPhases:             selectionPhases,
			}, nil
		}
		if scanLimit >= acceptanceScanLimit {
			acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "error",
				Duration: time.Since(start).Seconds(),
				Err: fmt.Errorf(
					"scan limit cap reached before finding accepted code call work (%d)",
					acceptanceScanLimit,
				),
			})
			return codeCallSelectionResult{}, fmt.Errorf(
				"code call acceptance scan reached cap (%d) before locating accepted work",
				acceptanceScanLimit,
			)
		}

		nextLimit := scanLimit * 2
		if nextLimit > acceptanceScanLimit {
			nextLimit = acceptanceScanLimit
		}
		scanLimit = nextLimit
	}
}

func (r *CodeCallProjectionRunner) listPendingPartitionCandidates(
	ctx context.Context,
	partitionID int,
	partitionCount int,
	limit int,
) ([]SharedProjectionIntentRow, error) {
	if reader, ok := r.IntentReader.(CodeCallProjectionPartitionCandidateReader); ok {
		rows, err := reader.ListPendingDomainPartitionIntents(ctx, DomainCodeCalls, partitionID, partitionCount, limit)
		if err != nil {
			return nil, err
		}
		return r.appendUnhashedPartitionCandidates(ctx, rows, partitionID, partitionCount, limit)
	}
	return r.IntentReader.ListPendingDomainIntents(ctx, DomainCodeCalls, limit)
}

func (r *CodeCallProjectionRunner) appendUnhashedPartitionCandidates(
	ctx context.Context,
	rows []SharedProjectionIntentRow,
	partitionID int,
	partitionCount int,
	limit int,
) ([]SharedProjectionIntentRow, error) {
	reader, ok := r.IntentReader.(CodeCallProjectionUnhashedCandidateReader)
	if !ok {
		return rows, nil
	}

	legacyRows, err := reader.ListPendingDomainUnhashedIntents(
		ctx,
		DomainCodeCalls,
		r.Config.acceptanceScanLimit(),
	)
	if err != nil {
		return nil, err
	}
	for _, row := range legacyRows {
		if codeCallProjectionPartitionMatches(row, partitionID, partitionCount) {
			rows = append(rows, row)
		}
	}
	if len(legacyRows) == 0 {
		return rows, nil
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		}
		return rows[i].IntentID < rows[j].IntentID
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (r *CodeCallProjectionRunner) codeCallProjectionRowBlockedByRepoFence(
	ctx context.Context,
	row SharedProjectionIntentRow,
	pending []SharedProjectionIntentRow,
	rowIndex int,
	acceptanceRowsByKey map[SharedProjectionAcceptanceKey][]SharedProjectionIntentRow,
) (bool, error) {
	if codeCallProjectionRowBlockedByRepoFence(row, pending, rowIndex) {
		return true, nil
	}

	key, ok := row.AcceptanceKey()
	if !ok {
		return false, fmt.Errorf(
			"pending code call intent %q is missing scope, acceptance unit, or source run",
			row.IntentID,
		)
	}
	if lookup, ok := r.IntentReader.(CodeCallProjectionRefreshFenceLookup); ok {
		blocked, err := lookup.CodeCallProjectionRowBlockedByRepoFence(ctx, key, row, DomainCodeCalls)
		if err != nil {
			return false, fmt.Errorf("check code call refresh fence: %w", err)
		}
		return blocked, nil
	}
	rows, ok := acceptanceRowsByKey[key]
	if !ok {
		var err error
		rows, err = r.loadAcceptanceUnitRows(ctx, key)
		if err != nil {
			return false, fmt.Errorf("load code call acceptance unit rows for refresh fence: %w", err)
		}
		acceptanceRowsByKey[key] = rows
	}
	acceptanceRowIndex := -1
	for i, candidate := range rows {
		if candidate.IntentID == row.IntentID {
			acceptanceRowIndex = i
			break
		}
	}
	if acceptanceRowIndex < 0 {
		// The candidate disappeared between partition selection and full-unit
		// fence loading. Skip it instead of writing from a stale partition view.
		return true, nil
	}
	return codeCallProjectionRowBlockedByRepoFence(row, rows, acceptanceRowIndex), nil
}

func codeCallProjectionRowBlockedByRepoFence(
	row SharedProjectionIntentRow,
	pending []SharedProjectionIntentRow,
	rowIndex int,
) bool {
	repositoryID := codeCallProjectionRowRepository(row)
	if repositoryID == "" {
		return false
	}
	if codeCallProjectionIsFileScoped(row) {
		for candidateIndex, candidate := range pending {
			if candidateIndex == rowIndex {
				continue
			}
			if codeCallProjectionIsFileScoped(candidate) &&
				!codeCallProjectionIsRepoRefresh(row) &&
				codeCallProjectionRefreshCoversRow(candidate, row) {
				return true
			}
		}
		for _, candidate := range pending[:rowIndex] {
			if codeCallProjectionRowRepository(candidate) == repositoryID &&
				codeCallProjectionSameAcceptanceUnit(candidate, row) &&
				codeCallProjectionIsWholeScoped(candidate) {
				return true
			}
		}
		return false
	}

	for _, candidate := range pending[:rowIndex] {
		if codeCallProjectionRowRepository(candidate) == repositoryID &&
			codeCallProjectionSameAcceptanceUnit(candidate, row) &&
			(codeCallProjectionIsFileScoped(candidate) || codeCallProjectionIsWholeScoped(candidate)) {
			return true
		}
	}
	return false
}

func codeCallProjectionSameAcceptanceUnit(a SharedProjectionIntentRow, b SharedProjectionIntentRow) bool {
	return a.ScopeID == b.ScopeID &&
		a.AcceptanceUnitID == b.AcceptanceUnitID &&
		a.SourceRunID == b.SourceRunID
}
