// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// WorkloadIdentityWrite captures the bounded canonical reconciliation request
// for one workload identity reducer intent.
type WorkloadIdentityWrite struct {
	IntentID        string
	ScopeID         string
	GenerationID    string
	SourceSystem    string
	Cause           string
	EntityKeys      []string
	RelatedScopeIDs []string
}

// WorkloadIdentityWriteResult captures the canonical workload identity write
// outcome returned by the backend adapter.
type WorkloadIdentityWriteResult struct {
	CanonicalID      string
	CanonicalWrites  int
	ReconciledScopes int
	EvidenceSummary  string
}

// WorkloadIdentityWriter persists one workload identity reconciliation request
// into a canonical reducer-owned target.
type WorkloadIdentityWriter interface {
	WriteWorkloadIdentity(context.Context, WorkloadIdentityWrite) (WorkloadIdentityWriteResult, error)
}

// WorkloadIdentityHandler reduces one workload identity intent into a bounded
// canonical write request.
type WorkloadIdentityHandler struct {
	Writer         WorkloadIdentityWriter
	PhasePublisher GraphProjectionPhasePublisher
}

// workloadIdentityTiming records success-path stage timings for the
// workload_identity reducer domain. Timing wrappers are time.Now diffs around
// existing work and add negligible overhead (sub-microsecond per intent).
type workloadIdentityTiming struct {
	graphWriteDuration   time.Duration
	phasePublishDuration time.Duration
	totalDuration        time.Duration
}

// Handle executes the workload identity reduction path.
func (h WorkloadIdentityHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStarted := time.Now()
	var timing workloadIdentityTiming

	if intent.Domain != DomainWorkloadIdentity {
		return Result{}, fmt.Errorf(
			"workload identity handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("workload identity writer is required")
	}

	request, err := workloadIdentityWriteFromIntent(intent)
	if err != nil {
		return Result{}, err
	}

	graphWriteStarted := time.Now()
	writeResult, err := h.Writer.WriteWorkloadIdentity(ctx, request)
	timing.graphWriteDuration = time.Since(graphWriteStarted)
	if err != nil {
		return Result{}, err
	}

	phaseStarted := time.Now()
	if err := publishIntentGraphPhase(
		ctx,
		h.PhasePublisher,
		intent,
		GraphProjectionKeyspaceServiceUID,
		GraphProjectionPhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	); err != nil {
		return Result{}, err
	}
	timing.phasePublishDuration = time.Since(phaseStarted)
	timing.totalDuration = time.Since(totalStarted)

	evidenceSummary := strings.TrimSpace(writeResult.EvidenceSummary)
	if evidenceSummary == "" {
		evidenceSummary = fmt.Sprintf(
			"reconciled %d workload identity key(s) across %d scope(s)",
			len(request.EntityKeys),
			len(request.RelatedScopeIDs),
		)
	}

	// input_ready reflects INPUT PRESENCE, not write count: the identity writer
	// runs unconditionally, so CanonicalWrites==0 is genuine empty work, not an
	// ordering stall. A workload_identity intent always carries the entity keys
	// it must reconcile (validated in workloadIdentityWriteFromIntent), so input
	// is present whenever the request has entity keys.
	inputReady := len(request.EntityKeys) > 0
	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainWorkloadIdentity,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: evidenceSummary,
		CanonicalWrites: writeResult.CanonicalWrites,
		SubDurations:    workloadIdentitySubDurations(timing),
		SubSignals:      materializationDiagnosticSignals(inputReady, writeResult.CanonicalWrites),
	}, nil
}

// workloadIdentitySubDurations converts the per-phase timing struct into the
// Result.SubDurations map so the service layer emits sub_duration_<key>_seconds
// log attributes alongside handler_duration_seconds. Keys follow the
// workload_materialization naming convention for cross-domain log correlation.
// Non-duration diagnostic signals (input_ready, written_rows) are carried
// separately in Result.SubSignals so the _seconds suffix stays honest.
func workloadIdentitySubDurations(t workloadIdentityTiming) map[string]float64 {
	return map[string]float64{
		"graph_write":   t.graphWriteDuration.Seconds(),
		"phase_publish": t.phasePublishDuration.Seconds(),
		"total":         t.totalDuration.Seconds(),
	}
}

func workloadIdentityWriteFromIntent(intent Intent) (WorkloadIdentityWrite, error) {
	entityKeys := uniqueSortedStrings(intent.EntityKeys)
	if len(entityKeys) == 0 {
		return WorkloadIdentityWrite{}, fmt.Errorf(
			"workload identity intent %q must include at least one entity key",
			intent.IntentID,
		)
	}

	relatedScopeIDs := uniqueSortedStrings(append(intent.RelatedScopeIDs, intent.ScopeID))
	if len(relatedScopeIDs) == 0 {
		return WorkloadIdentityWrite{}, fmt.Errorf(
			"workload identity intent %q must include at least one related scope id",
			intent.IntentID,
		)
	}

	return WorkloadIdentityWrite{
		IntentID:        intent.IntentID,
		ScopeID:         intent.ScopeID,
		GenerationID:    intent.GenerationID,
		SourceSystem:    intent.SourceSystem,
		Cause:           intent.Cause,
		EntityKeys:      entityKeys,
		RelatedScopeIDs: relatedScopeIDs,
	}, nil
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	if len(normalized) == 0 {
		return nil
	}

	slices.Sort(normalized)
	return normalized
}
