// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
	if err := validateRepoDependencySourceRepositoryIdentity(acceptanceUnitID, rows); err != nil {
		result.LeaseAcquired = true
		return result, nil, 0, err
	}

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
		freshness := r.newGenerationFreshness()
		ready, replayRequests, replayDuration, err := r.ensureRunsOnWorkloadReadiness(ctx, active, freshness)
		result.ReplayDurationSeconds = replayDuration.Seconds()
		result.ReplayRequests = replayRequests
		if err != nil {
			return result, active, writtenGroups, err
		}
		if !ready {
			result.BlockedReadiness = 1
			r.recordRepoDependencyWorkloadReadinessBlocked(ctx, acceptanceUnitID, cycleStart)
			return result, nil, writtenGroups, nil
		}
		if repoDependencyNeedsRetract(rows, staleIDs) {
			retractStart := time.Now()
			retractedRows, err := r.retractRepo(ctx, active)
			result.RetractDurationSeconds = time.Since(retractStart).Seconds()
			if err != nil {
				return result, active, writtenGroups, err
			}
			result.RetractedRows = retractedRows
		}
		r.recordInactiveAcceptedGenerationRows(ctx, active, freshness)
		writeStart := time.Now()
		writtenRows, writtenGroups, err = r.writeActiveRows(ctx, active)
		result.WriteDurationSeconds = time.Since(writeStart).Seconds()
		if err != nil {
			return result, active, writtenGroups, err
		}
		result.UpsertedRows = writtenRows
		if r.WorkloadMaterializationReplayer != nil {
			replayStart := time.Now()
			replayRequests, err := r.replayWorkloadMaterialization(ctx, active, freshness)
			result.ReplayDurationSeconds += time.Since(replayStart).Seconds()
			result.ReplayRequests += replayRequests
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

func validateRepoDependencySourceRepositoryIdentity(
	acceptanceUnitID string,
	rows []SharedProjectionIntentRow,
) error {
	expectedRepoID := strings.TrimSpace(acceptanceUnitID)
	if expectedRepoID == "" {
		return fmt.Errorf("repo dependency source repository identity is missing gated acceptance unit")
	}
	for _, row := range rows {
		rowAcceptanceUnitID := strings.TrimSpace(row.AcceptanceUnitID)
		repositoryID := strings.TrimSpace(row.RepositoryID)
		payloadRepoID := repoDependencyPayloadString(row, "repo_id")
		if rowAcceptanceUnitID == expectedRepoID &&
			repositoryID == expectedRepoID &&
			payloadRepoID == expectedRepoID {
			continue
		}
		return fmt.Errorf(
			"repo dependency intent %q source repository identity mismatch: gated acceptance_unit_id=%q, acceptance_unit_id=%q, repository_id=%q, payload.repo_id=%q",
			row.IntentID,
			expectedRepoID,
			rowAcceptanceUnitID,
			repositoryID,
			payloadRepoID,
		)
	}
	return nil
}

// replayWorkloadMaterializationRequest sends one replay request and reports
// the closed outcome. A replayer that does not implement
// [WorkloadMaterializationOutcomeReplayer] reports only a boolean, so its
// unscheduled answer stays [WorkloadMaterializationReplayNotScheduled] and the
// caller fails closed on it.
func (r *RepoDependencyProjectionRunner) replayWorkloadMaterializationRequest(
	ctx context.Context,
	request workloadMaterializationReplayRequest,
) (WorkloadMaterializationReplayOutcome, error) {
	if replayer, ok := r.WorkloadMaterializationReplayer.(WorkloadMaterializationOutcomeReplayer); ok {
		return replayer.ReplayWorkloadMaterializationOutcome(
			ctx, request.scopeID, request.generationID, request.entityKey,
		)
	}
	replayed, err := r.WorkloadMaterializationReplayer.ReplayWorkloadMaterialization(
		ctx, request.scopeID, request.generationID, request.entityKey,
	)
	if err != nil {
		return WorkloadMaterializationReplayNotScheduled, err
	}
	if replayed {
		return WorkloadMaterializationReplayScheduled, nil
	}
	return WorkloadMaterializationReplayNotScheduled, nil
}

// replayWorkloadMaterializationFenceRequest sends one fenced replay request
// and reports the closed outcome. A replayer that does not implement
// [WorkloadMaterializationFenceOutcomeReplayer] reports only a boolean, so
// its unscheduled answer stays [WorkloadMaterializationReplayNotScheduled]
// and the caller fails closed on it.
func replayWorkloadMaterializationFenceRequest(
	ctx context.Context,
	replayer WorkloadMaterializationFenceReplayer,
	request workloadMaterializationFenceRequest,
) (WorkloadMaterializationReplayOutcome, error) {
	if replayer, ok := replayer.(WorkloadMaterializationFenceOutcomeReplayer); ok {
		return replayer.ReplayWorkloadMaterializationForFenceOutcome(
			ctx, request.scopeID, request.generationID, request.entityKey, request.repoID, request.fence,
		)
	}
	replayed, err := replayer.ReplayWorkloadMaterializationForFence(
		ctx, request.scopeID, request.generationID, request.entityKey, request.repoID, request.fence,
	)
	if err != nil {
		return WorkloadMaterializationReplayNotScheduled, err
	}
	if replayed {
		return WorkloadMaterializationReplayScheduled, nil
	}
	return WorkloadMaterializationReplayNotScheduled, nil
}

// skipFencedReplayIfRetired re-checks freshness without the cache: a
// supersede that landed after the first check retires the request, so it is
// skipped. On the active generation the replay is owed and cannot run: it is
// counted under reason and the caller fails closed. It reports whether the
// request was skipped.
func (r *RepoDependencyProjectionRunner) skipFencedReplayIfRetired(
	ctx context.Context,
	freshness *repoDependencyGenerationFreshness,
	request workloadMaterializationFenceRequest,
	reason string,
) (bool, error) {
	retiredNow, err := freshness.retiredNow(ctx, request.scopeID, request.generationID)
	if err != nil {
		return false, err
	}
	if retiredNow {
		if freshness.firstSkip(request.workloadMaterializationReplayRequest) {
			r.recordRepoDependencyReplaySkipped(ctx, request.workloadMaterializationReplayRequest, true)
		}
		return true, nil
	}
	r.recordRepoDependencyGenerationAnomaly(
		ctx, request.scopeID, request.generationID, request.entityKey,
		reason, 1,
	)
	return false, nil
}

func errRepoDependencyFencedReplayNotScheduled(request workloadMaterializationFenceRequest) error {
	return fmt.Errorf(
		"workload materialization fenced replay was not scheduled for scope %q generation %q entity %q",
		request.scopeID,
		request.generationID,
		request.entityKey,
	)
}

// repoDependencyGenerationFreshness answers, for one acceptance-unit cycle,
// whether a scope generation is still the scope's active generation (#7670).
// The acceptance row the runner reads is written once and not rewritten when
// recover-generations retires the generation, so an accepted generation can
// name a retired one and its workload replay is then nothing to replay.
//
// It wraps [GenerationFreshnessCheck], the runtime's own guard: a scope with no
// active generation or an unknown scope reads as current, and a newer pending
// generation reports [reducercontract.GenerationNotYetActiveError], which is
// not retired because the replay is still owed once it activates. Any other
// lookup error is returned and fails the cycle closed. Answers are cached per
// scope generation for this cycle only: the value is built per cycle, so a
// scope that activates a new generation between cycles is re-checked.
type repoDependencyGenerationFreshness struct {
	check GenerationFreshnessCheck
	cache map[string]bool
	// skipped records the replay requests already counted as skipped this
	// cycle, so a RUNS_ON row's fenced and plain request for the same scope,
	// generation and entity count once.
	skipped map[string]bool
}

func (r *RepoDependencyProjectionRunner) newGenerationFreshness() *repoDependencyGenerationFreshness {
	return &repoDependencyGenerationFreshness{
		check:   r.GenerationFreshness,
		cache:   make(map[string]bool),
		skipped: make(map[string]bool),
	}
}

// firstSkip reports whether the request has not yet been counted as skipped
// this cycle, and marks it counted.
func (f *repoDependencyGenerationFreshness) firstSkip(request workloadMaterializationReplayRequest) bool {
	if f == nil {
		return true
	}
	key := request.scopeID + "\x00" + request.generationID + "\x00" + request.entityKey
	if f.skipped[key] {
		return false
	}
	f.skipped[key] = true
	return true
}

// retired reports whether the generation is no longer active, using the cycle
// cache. A nil check never reports retired, so every request stays on the
// replayer and fails closed when it cannot be scheduled.
func (f *repoDependencyGenerationFreshness) retired(
	ctx context.Context,
	scopeID, generationID string,
) (bool, error) {
	if f == nil || f.check == nil {
		return false, nil
	}
	key := scopeID + "\x00" + generationID
	if retired, ok := f.cache[key]; ok {
		return retired, nil
	}
	return f.retiredNow(ctx, scopeID, generationID)
}

// retiredNow runs the check without reading the cache and refreshes the cache
// with the answer. It re-checks a generation whose replay just reported a
// superseded stable item, so a supersede that landed after the first check is
// not reported as an active-generation failure.
func (f *repoDependencyGenerationFreshness) retiredNow(
	ctx context.Context,
	scopeID, generationID string,
) (bool, error) {
	if f == nil || f.check == nil {
		return false, nil
	}
	key := scopeID + "\x00" + generationID
	current, err := f.check(ctx, scopeID, generationID)
	if err != nil {
		if errors.Is(err, reducercontract.ErrGenerationNotYetActive) {
			f.cache[key] = false
			return false, nil
		}
		return false, fmt.Errorf(
			"check generation freshness for scope %q generation %q for workload materialization replay: %w",
			scopeID, generationID, err,
		)
	}
	f.cache[key] = !current
	return !current, nil
}

// dropRetiredFenceRequests removes the fenced replay requests whose generation
// is no longer the scope's active generation, counting each as skipped.
func (r *RepoDependencyProjectionRunner) dropRetiredFenceRequests(
	ctx context.Context,
	requests []workloadMaterializationFenceRequest,
	freshness *repoDependencyGenerationFreshness,
) ([]workloadMaterializationFenceRequest, error) {
	kept := make([]workloadMaterializationFenceRequest, 0, len(requests))
	for _, request := range requests {
		retired, err := freshness.retired(ctx, request.scopeID, request.generationID)
		if err != nil {
			return nil, err
		}
		if retired {
			if freshness.firstSkip(request.workloadMaterializationReplayRequest) {
				r.recordRepoDependencyReplaySkipped(ctx, request.workloadMaterializationReplayRequest, true)
			}
			continue
		}
		kept = append(kept, request)
	}
	return kept, nil
}

// recordInactiveAcceptedGenerationRows counts the active rows about to be
// written whose accepted generation is no longer the scope's active
// generation, and logs one WARN per scope generation for the cycle. It changes
// no behavior: the rows still project. A freshness lookup error is left to the
// replay path, which fails the cycle closed, so it is not reported here.
func (r *RepoDependencyProjectionRunner) recordInactiveAcceptedGenerationRows(
	ctx context.Context,
	rows []SharedProjectionIntentRow,
	freshness *repoDependencyGenerationFreshness,
) {
	type scopeGeneration struct{ scopeID, generationID string }
	counts := make(map[scopeGeneration]int, len(rows))
	order := make([]scopeGeneration, 0, len(rows))
	for _, row := range rows {
		key := scopeGeneration{
			scopeID:      strings.TrimSpace(row.ScopeID),
			generationID: strings.TrimSpace(row.GenerationID),
		}
		if key.scopeID == "" || key.generationID == "" {
			continue
		}
		if _, seen := counts[key]; !seen {
			order = append(order, key)
		}
		counts[key]++
	}
	for _, key := range order {
		retired, err := freshness.retired(ctx, key.scopeID, key.generationID)
		if err != nil || !retired {
			continue
		}
		r.recordRepoDependencyGenerationAnomaly(
			ctx, key.scopeID, key.generationID, "",
			telemetry.RepoDependencyAnomalyInactiveAcceptedGeneration, counts[key],
		)
	}
}

func (r *RepoDependencyProjectionRunner) replayWorkloadMaterializationForFence(
	ctx context.Context,
	requests []workloadMaterializationFenceRequest,
	freshness *repoDependencyGenerationFreshness,
) (int, error) {
	replayer, ok := r.WorkloadMaterializationReplayer.(WorkloadMaterializationFenceReplayer)
	if !ok {
		return 0, fmt.Errorf("repo dependency RUNS_ON workload replayer does not support readiness fences")
	}
	for i, request := range requests {
		outcome, err := replayWorkloadMaterializationFenceRequest(ctx, replayer, request)
		if err != nil {
			return i + 1, err
		}
		switch outcome {
		case WorkloadMaterializationReplayScheduled:
		case WorkloadMaterializationReplaySuperseded:
			// A superseded stable item is nothing to replay only when the
			// generation is retired. Re-check, because a supersede can land
			// between the first check and this replay. On the active
			// generation the queue never revives the item, so the owed
			// materialization cannot run: count it on the shared anomaly and
			// fail closed.
			skipped, err := r.skipFencedReplayIfRetired(ctx, freshness, request, telemetry.RepoDependencyAnomalySupersededItemOnActiveGeneration)
			if err != nil {
				return i + 1, err
			}
			if skipped {
				continue
			}
			return i + 1, errRepoDependencyFencedReplayNotScheduled(request)
		default:
			// Any other unscheduled replay keeps failing: re-check, skip
			// when retired, else count it under the fenced reason and fail
			// closed.
			skipped, err := r.skipFencedReplayIfRetired(ctx, freshness, request, telemetry.RepoDependencyAnomalyUnscheduledFencedReplayOnActiveGeneration)
			if err != nil {
				return i + 1, err
			}
			if skipped {
				continue
			}
			return i + 1, errRepoDependencyFencedReplayNotScheduled(request)
		}
	}
	return len(requests), nil
}
