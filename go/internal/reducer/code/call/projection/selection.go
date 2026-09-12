// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

type codeCallSelectionResult struct {
	Key                         sharedintent.AcceptanceKey
	PartitionKey                string
	BlockedReadiness            int
	MaxBlockedIntentWaitSeconds float64
	SelectionDurationSeconds    float64
	SelectionPhases             worker.SelectionPhaseDurations
}

type acceptedGenerationCacheEntry struct {
	generationID string
	found        bool
}

type readinessCacheEntry struct {
	ready bool
	found bool
}

func (r *Runner) selectAcceptanceUnitWork(ctx context.Context) (sharedintent.AcceptanceKey, error) {
	result, err := r.selectAcceptanceUnitWorkWithStats(ctx, time.Now().UTC())
	return result.Key, err
}

func (r *Runner) selectAcceptanceUnitWorkWithStats(
	ctx context.Context,
	now time.Time,
) (codeCallSelectionResult, error) {
	return r.selectAcceptanceUnitPartitionWorkWithStats(ctx, now, 0, 1)
}

func (r *Runner) selectAcceptanceUnitPartitionWorkWithStats(
	ctx context.Context,
	now time.Time,
	partitionID int,
	partitionCount int,
) (codeCallSelectionResult, error) {
	start := time.Now()
	acceptanceTelemetry := worker.AcceptanceTelemetry{
		Instruments: r.Instruments,
		Logger:      r.Logger,
	}
	scanLimit := r.Config.batchLimit()
	acceptanceScanLimit := r.Config.acceptanceScanLimit()
	if scanLimit > acceptanceScanLimit {
		scanLimit = acceptanceScanLimit
	}

	acceptedGenerationsByKey := make(map[sharedintent.AcceptanceKey]acceptedGenerationCacheEntry)
	readinessByKey := make(map[gpphase.PhaseKey]readinessCacheEntry)
	acceptanceRowsByKey := make(map[sharedintent.AcceptanceKey][]sharedintent.Row)
	selectionPhases := worker.SelectionPhaseDurations{}
	for {
		candidateLoadStart := time.Now()
		pending, err := r.listPendingPartitionCandidates(ctx, partitionID, partitionCount, scanLimit)
		selectionPhases.CandidateLoadSeconds += time.Since(candidateLoadStart).Seconds()
		if err != nil {
			acceptanceTelemetry.RecordLookup(ctx, worker.AcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "error",
				Duration: time.Since(start).Seconds(),
				Err:      err,
			})
			return codeCallSelectionResult{}, fmt.Errorf("list pending code call intents: %w", err)
		}
		if len(pending) == 0 {
			acceptanceTelemetry.RecordLookup(ctx, worker.AcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "miss",
				Duration: time.Since(start).Seconds(),
			})
			return codeCallSelectionResult{
				SelectionDurationSeconds: time.Since(start).Seconds(),
				SelectionPhases:          selectionPhases,
			}, nil
		}

		phase, gated := worker.ReadinessPhase(reducercontract.DomainCodeCalls)
		acceptedByKey := make(map[sharedintent.AcceptanceKey]string, len(pending))
		missingAcceptedRows := make([]sharedintent.Row, 0, len(pending))
		seen := make(map[sharedintent.AcceptanceKey]struct{}, len(pending))
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
					acceptanceTelemetry.RecordLookup(ctx, worker.AcceptanceLookupEvent{
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
			readinessKeys := make([]gpphase.PhaseKey, 0, len(acceptedByKey))
			for key, acceptedGeneration := range acceptedByKey {
				readinessKey, ok := worker.GraphProjectionPhaseKeyForAcceptance(
					key,
					acceptedGeneration,
					gpphase.KeyspaceCodeEntitiesUID,
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
					acceptanceTelemetry.RecordLookup(ctx, worker.AcceptanceLookupEvent{
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
				readinessKey, ok := worker.GraphProjectionPhaseKeyForAcceptance(
					key,
					acceptedGeneration,
					gpphase.KeyspaceCodeEntitiesUID,
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
					if wait := worker.MaxIntentWaitSeconds(now, []sharedintent.Row{row}); wait > maxBlockedWait {
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
				acceptanceTelemetry.RecordLookup(ctx, worker.AcceptanceLookupEvent{
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

			acceptanceTelemetry.RecordLookup(ctx, worker.AcceptanceLookupEvent{
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
				log.Domain(reducercontract.DomainCodeCalls),
				telemetry.PhaseAttr(telemetry.PhaseShared),
			)
		}

		if len(pending) < scanLimit {
			acceptanceTelemetry.RecordLookup(ctx, worker.AcceptanceLookupEvent{
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
			acceptanceTelemetry.RecordLookup(ctx, worker.AcceptanceLookupEvent{
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

func (r *Runner) listPendingPartitionCandidates(
	ctx context.Context,
	partitionID int,
	partitionCount int,
	limit int,
) ([]sharedintent.Row, error) {
	if reader, ok := r.IntentReader.(PartitionCandidateReader); ok {
		rows, err := reader.ListPendingDomainPartitionIntents(ctx, reducercontract.DomainCodeCalls, partitionID, partitionCount, limit)
		if err != nil {
			return nil, err
		}
		return r.appendUnhashedPartitionCandidates(ctx, rows, partitionID, partitionCount, limit)
	}
	return r.IntentReader.ListPendingDomainIntents(ctx, reducercontract.DomainCodeCalls, limit)
}

func (r *Runner) appendUnhashedPartitionCandidates(
	ctx context.Context,
	rows []sharedintent.Row,
	partitionID int,
	partitionCount int,
	limit int,
) ([]sharedintent.Row, error) {
	reader, ok := r.IntentReader.(UnhashedCandidateReader)
	if !ok {
		return rows, nil
	}

	legacyRows, err := reader.ListPendingDomainUnhashedIntents(
		ctx,
		reducercontract.DomainCodeCalls,
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

func (r *Runner) codeCallProjectionRowBlockedByRepoFence(
	ctx context.Context,
	row sharedintent.Row,
	pending []sharedintent.Row,
	rowIndex int,
	acceptanceRowsByKey map[sharedintent.AcceptanceKey][]sharedintent.Row,
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
	if lookup, ok := r.IntentReader.(RefreshFenceLookup); ok {
		blocked, err := lookup.CodeCallProjectionRowBlockedByRepoFence(ctx, key, row, reducercontract.DomainCodeCalls)
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
	row sharedintent.Row,
	pending []sharedintent.Row,
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

func codeCallProjectionSameAcceptanceUnit(a sharedintent.Row, b sharedintent.Row) bool {
	return a.ScopeID == b.ScopeID &&
		a.AcceptanceUnitID == b.AcceptanceUnitID &&
		a.SourceRunID == b.SourceRunID
}
