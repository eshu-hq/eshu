// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"time"
)

func (r *RepoDependencyProjectionRunner) processAcceptanceUnit(
	ctx context.Context,
	now time.Time,
	acceptanceUnitID string,
	reader RepoDependencyProjectionIntentReader,
	result PartitionProcessResult,
	cycleStart time.Time,
) (PartitionProcessResult, []SharedProjectionIntentRow, int, error) {
	loadAllStart := time.Now()
	rows, err := r.loadAllAcceptanceUnitIntents(ctx, reader, acceptanceUnitID)
	result.LoadAllDurationSeconds = time.Since(loadAllStart).Seconds()
	if err != nil {
		result.LeaseAcquired = true
		return result, nil, 0, err
	}
	result.AcceptanceUnitRows = len(rows)

	lookup := r.AcceptedGen
	if r.AcceptedGenPrefetch != nil {
		prefetchStart := time.Now()
		resolvedLookup, err := r.AcceptedGenPrefetch(ctx, rows)
		result.AcceptancePrefetchDurationSeconds = time.Since(prefetchStart).Seconds()
		if err != nil {
			result.LeaseAcquired = true
			return result, nil, 0, fmt.Errorf("prefetch accepted generations: %w", err)
		}
		lookup = resolvedLookup
	}

	active, staleIDs := FilterAuthoritativeIntents(rows, lookup)
	result.StaleIntents = len(staleIDs)
	result.ActiveIntents = len(active)
	if len(active) == 0 && len(staleIDs) == 0 {
		result.LeaseAcquired = true
		return result, active, 0, nil
	}

	result.LeaseAcquired = true
	writtenRows := 0
	writtenGroups := 0
	if len(active) > 0 {
		if repoDependencyNeedsRetract(rows, staleIDs) {
			retractStart := time.Now()
			retractedRows, err := r.retractRepo(ctx, active)
			result.RetractDurationSeconds = time.Since(retractStart).Seconds()
			if err != nil {
				return result, active, writtenGroups, err
			}
			result.RetractedRows = retractedRows
		}
		writeStart := time.Now()
		writtenRows, writtenGroups, err = r.writeActiveRows(ctx, active)
		result.WriteDurationSeconds = time.Since(writeStart).Seconds()
		if err != nil {
			return result, active, writtenGroups, err
		}
		result.UpsertedRows = writtenRows
		if r.WorkloadMaterializationReplayer != nil {
			replayStart := time.Now()
			replayRequests, err := r.replayWorkloadMaterialization(ctx, active)
			result.ReplayDurationSeconds = time.Since(replayStart).Seconds()
			result.ReplayRequests = replayRequests
			if err != nil {
				return result, active, writtenGroups, fmt.Errorf("replay workload materialization after repo dependency projection: %w", err)
			}
		}
	}

	processedIDs := make([]string, 0, len(staleIDs)+len(active))
	processedIDs = append(processedIDs, staleIDs...)
	for _, row := range active {
		processedIDs = append(processedIDs, row.IntentID)
	}
	if len(processedIDs) > 0 {
		markCompletedStart := time.Now()
		if err := reader.MarkIntentsCompleted(ctx, processedIDs, now); err != nil {
			return result, active, writtenGroups, fmt.Errorf("mark repo dependency intents completed: %w", err)
		}
		result.MarkCompletedDurationSeconds = time.Since(markCompletedStart).Seconds()
	}
	result.ProcessedIntents = len(processedIDs)
	result.ProcessingDurationSeconds = time.Since(cycleStart).Seconds()
	return result, active, writtenGroups, nil
}
