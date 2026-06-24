// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

const observabilityCoverageCorrelationFactKind = "reducer_observability_coverage_correlation"

// PostgresObservabilityCoverageCorrelationWriter stores reducer-owned
// observability coverage correlation decisions in the shared fact store. It
// writes through the canonical reducer fact insert path with a stable,
// retry-idempotent identity — no new table and no schema DDL.
type PostgresObservabilityCoverageCorrelationWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteObservabilityCoverageCorrelations persists every coverage outcome so
// callers can distinguish covered, gap, ambiguous, stale, and rejected
// observability findings. Repeated writes of the same decision converge on one
// fact_id, making the write safe under reducer retries and reprojection.
func (w PostgresObservabilityCoverageCorrelationWriter) WriteObservabilityCoverageCorrelations(
	ctx context.Context,
	write ObservabilityCoverageCorrelationWrite,
) (ObservabilityCoverageCorrelationWriteResult, error) {
	if w.DB == nil {
		return ObservabilityCoverageCorrelationWriteResult{}, fmt.Errorf("observability coverage correlation database is required")
	}
	now := reducerWriterNow(w.Now)
	for _, decision := range write.Decisions {
		payload := observabilityCoverageCorrelationPayload(write, decision)
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return ObservabilityCoverageCorrelationWriteResult{}, fmt.Errorf("marshal observability coverage correlation payload: %w", err)
		}
		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			observabilityCoverageCorrelationFactID(write, decision),
			write.ScopeID,
			write.GenerationID,
			observabilityCoverageCorrelationFactKind,
			observabilityCoverageCorrelationStableFactKey(write, decision),
			reducerFactCollectorKind(write.SourceSystem),
			facts.SourceConfidenceInferred,
			write.SourceSystem,
			write.IntentID,
			nil,
			nil,
			now,
			now,
			false,
			payloadJSON,
		); err != nil {
			return ObservabilityCoverageCorrelationWriteResult{}, fmt.Errorf("write observability coverage correlation fact: %w", err)
		}
	}
	return ObservabilityCoverageCorrelationWriteResult{
		FactsWritten:    len(write.Decisions),
		EvidenceSummary: fmt.Sprintf("wrote observability coverage correlations=%d", len(write.Decisions)),
	}, nil
}

func observabilityCoverageCorrelationFactID(
	write ObservabilityCoverageCorrelationWrite,
	decision ObservabilityCoverageCorrelationDecision,
) string {
	return observabilityCoverageCorrelationFactKind + ":" + facts.StableID(
		observabilityCoverageCorrelationFactKind,
		observabilityCoverageCorrelationIdentity(write, decision),
	)
}

func observabilityCoverageCorrelationStableFactKey(
	write ObservabilityCoverageCorrelationWrite,
	decision ObservabilityCoverageCorrelationDecision,
) string {
	identity := observabilityCoverageCorrelationIdentity(write, decision)
	return strings.Join([]string{
		"observability_coverage_correlation",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["provider"])),
		strings.TrimSpace(fmt.Sprint(identity["coverage_signal"])),
		strings.TrimSpace(fmt.Sprint(identity["observability_object_ref"])),
		strings.TrimSpace(fmt.Sprint(identity["target_uid"])),
	}, ":")
}

// observabilityCoverageCorrelationIdentity is the stable identity tuple for a
// coverage decision. Coverage-edge decisions key on the observability object plus
// resolved target; gap findings (empty observability_object_ref) key on the
// uncovered target uid plus target service ref so gaps de-duplicate across
// retries.
func observabilityCoverageCorrelationIdentity(
	write ObservabilityCoverageCorrelationWrite,
	decision ObservabilityCoverageCorrelationDecision,
) map[string]any {
	target := strings.TrimSpace(decision.TargetUID)
	if target == "" {
		target = strings.TrimSpace(decision.TargetServiceRef)
	}
	return map[string]any{
		"scope_id":                 strings.TrimSpace(write.ScopeID),
		"generation_id":            strings.TrimSpace(write.GenerationID),
		"provider":                 strings.TrimSpace(decision.Provider),
		"coverage_signal":          strings.TrimSpace(decision.CoverageSignal),
		"observability_object_ref": strings.TrimSpace(decision.ObservabilityObjectRef),
		"target_uid":               target,
	}
}

func observabilityCoverageCorrelationPayload(
	write ObservabilityCoverageCorrelationWrite,
	decision ObservabilityCoverageCorrelationDecision,
) map[string]any {
	return map[string]any{
		"reducer_domain":             string(DomainObservabilityCoverageCorrelation),
		"intent_id":                  write.IntentID,
		"scope_id":                   write.ScopeID,
		"generation_id":              write.GenerationID,
		"source_system":              write.SourceSystem,
		"cause":                      write.Cause,
		"provider":                   decision.Provider,
		"coverage_signal":            decision.CoverageSignal,
		"observability_object_ref":   decision.ObservabilityObjectRef,
		"observability_resource_uid": decision.ObservabilityUID,
		"target_uid":                 decision.TargetUID,
		"target_service_ref":         decision.TargetServiceRef,
		"outcome":                    string(decision.Outcome),
		"reason":                     decision.Reason,
		"coverage_status":            decision.CoverageStatus,
		"provenance_only":            decision.ProvenanceOnly,
		"resolution_mode":            decision.ResolutionMode,
		"source_class":               decision.SourceClass,
		"source_classes":             uniqueSortedStrings(decision.SourceClasses),
		"source_kind":                decision.SourceKind,
		"source_kinds":               uniqueSortedStrings(decision.SourceKinds),
		"source_outcome":             decision.SourceOutcome,
		"source_outcomes":            uniqueSortedStrings(decision.SourceOutcomes),
		"resource_class":             decision.ResourceClass,
		"freshness_state":            decision.FreshnessState,
		"candidate_target_uids":      uniqueSortedStrings(decision.CandidateTargetUIDs),
		"evidence_fact_ids":          uniqueSortedStrings(decision.EvidenceFactIDs),
		"source_layers":              observabilityCoverageSourceLayers(decision),
	}
}

func observabilityCoverageSourceLayers(decision ObservabilityCoverageCorrelationDecision) []string {
	classes := decision.SourceClasses
	if len(classes) == 0 && strings.TrimSpace(decision.SourceClass) != "" {
		classes = []string{decision.SourceClass}
	}
	layers := make([]string, 0, len(classes))
	for _, class := range classes {
		switch strings.TrimSpace(class) {
		case "declared":
			layers = append(layers, string(truth.LayerSourceDeclaration))
		case "applied":
			layers = append(layers, string(truth.LayerAppliedDeclaration))
		case "observed":
			layers = append(layers, string(truth.LayerObservedResource))
		}
	}
	if len(layers) == 0 {
		layers = append(layers, string(truth.LayerObservedResource))
	}
	return uniqueSortedStrings(layers)
}
