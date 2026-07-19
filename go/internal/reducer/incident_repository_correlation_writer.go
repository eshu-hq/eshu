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

const incidentRepositoryCorrelationFactKind = "reducer_incident_repository_correlation"

// incidentRepositoryCorrelationDomainDefinition declares the additive
// incident-repository correlation domain. Its truth contract spans the source
// declaration layer (the applied routing fact) and the observed-resource layer
// (the resolved owning repository), matching the two layers an edge-bearing
// decision carries; provenance-only decisions stay declaration-layer only.
func incidentRepositoryCorrelationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainIncidentRepositoryCorrelation,
		Summary: "correlate applied PagerDuty incident routing to its owning repository through the durable Terraform backend-locator join",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "incident_repository_correlation",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// PostgresIncidentRepositoryCorrelationWriter stores reducer-owned incident
// repository correlation decisions in the shared fact store. Every outcome is
// written — exact, derived, ambiguous, unresolved, and rejected — so callers can
// distinguish a durable edge from provenance-only routing. The fact id is
// deterministic over (scope, generation, provider, provider service id), so
// retries and concurrent workers converge on one row via the
// ON CONFLICT (fact_id) DO UPDATE idempotency of canonicalReducerFactInsertQuery.
type PostgresIncidentRepositoryCorrelationWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteIncidentRepositoryCorrelations persists every incident-repository
// correlation decision. Only exact and derived decisions carry a repository_id
// and source layers that include the observed-resource layer; weaker outcomes
// are written as provenance-only declaration-layer facts so a downstream scoped
// predicate can fail closed on the absence of a durable edge.
func (w PostgresIncidentRepositoryCorrelationWriter) WriteIncidentRepositoryCorrelations(
	ctx context.Context,
	write IncidentRepositoryCorrelationWrite,
) (IncidentRepositoryCorrelationWriteResult, error) {
	if w.DB == nil {
		return IncidentRepositoryCorrelationWriteResult{}, fmt.Errorf("incident repository correlation database is required")
	}
	now := reducerWriterNow(w.Now)
	rows := make([]reducerFactRow, 0, len(write.Decisions))
	for _, decision := range write.Decisions {
		payload := incidentRepositoryCorrelationPayload(write, decision)
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return IncidentRepositoryCorrelationWriteResult{}, fmt.Errorf("marshal incident repository correlation payload: %w", err)
		}
		rows = append(rows, reducerFactRow{
			FactID:           incidentRepositoryCorrelationFactID(write, decision),
			ScopeID:          write.ScopeID,
			GenerationID:     write.GenerationID,
			FactKind:         incidentRepositoryCorrelationFactKind,
			StableFactKey:    incidentRepositoryCorrelationStableFactKey(write, decision),
			CollectorKind:    reducerFactCollectorKind(write.SourceSystem),
			SourceConfidence: facts.SourceConfidenceInferred,
			SourceSystem:     write.SourceSystem,
			SourceFactKey:    write.IntentID,
			ObservedAt:       now,
			IngestedAt:       now,
			Payload:          string(payloadJSON),
		})
	}
	// Bounded chunked bulk insert: decisions are upserted in O(N/batchSize)
	// round-trips rather than one ExecContext per decision.
	if err := reducerBatchInsertFacts(ctx, w.DB, rows); err != nil {
		return IncidentRepositoryCorrelationWriteResult{}, fmt.Errorf("write incident repository correlation fact: %w", err)
	}
	return IncidentRepositoryCorrelationWriteResult{
		FactsWritten:    len(write.Decisions),
		EvidenceSummary: fmt.Sprintf("wrote incident repository correlations=%d", len(write.Decisions)),
	}, nil
}

func incidentRepositoryCorrelationFactID(
	write IncidentRepositoryCorrelationWrite,
	decision IncidentRepositoryCorrelationDecision,
) string {
	return incidentRepositoryCorrelationFactKind + ":" + facts.StableID(
		incidentRepositoryCorrelationFactKind,
		incidentRepositoryCorrelationIdentity(write, decision),
	)
}

func incidentRepositoryCorrelationStableFactKey(
	write IncidentRepositoryCorrelationWrite,
	decision IncidentRepositoryCorrelationDecision,
) string {
	identity := incidentRepositoryCorrelationIdentity(write, decision)
	return strings.Join([]string{
		"incident_repository_correlation",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["provider"])),
		strings.TrimSpace(fmt.Sprint(identity["provider_service_id"])),
	}, ":")
}

// incidentRepositoryCorrelationIdentity is the generation-scoped identity of one
// correlation fact. It deliberately excludes the resolved repository id: the
// identity is the provider service routing being classified, so an ambiguous or
// unresolved re-run for the same provider service id updates the same row rather
// than appending a stale duplicate. A blank provider service id (name-only
// rejected rows) falls back to the backend locator so distinct rejected rows do
// not collide.
func incidentRepositoryCorrelationIdentity(
	write IncidentRepositoryCorrelationWrite,
	decision IncidentRepositoryCorrelationDecision,
) map[string]any {
	providerServiceID := strings.TrimSpace(decision.ProviderServiceID)
	if providerServiceID == "" {
		providerServiceID = "name_only:" +
			strings.TrimSpace(decision.BackendKind) + ":" +
			strings.TrimSpace(decision.LocatorHash) + ":" +
			strings.Join(decision.EvidenceFactIDs, ",")
	}
	return map[string]any{
		"scope_id":            strings.TrimSpace(write.ScopeID),
		"generation_id":       strings.TrimSpace(write.GenerationID),
		"provider":            strings.TrimSpace(decision.Provider),
		"provider_service_id": providerServiceID,
	}
}

func incidentRepositoryCorrelationPayload(
	write IncidentRepositoryCorrelationWrite,
	decision IncidentRepositoryCorrelationDecision,
) map[string]any {
	return map[string]any{
		"reducer_domain":           string(DomainIncidentRepositoryCorrelation),
		"intent_id":                write.IntentID,
		"scope_id":                 write.ScopeID,
		"generation_id":            write.GenerationID,
		"source_system":            write.SourceSystem,
		"cause":                    write.Cause,
		"provider":                 decision.Provider,
		"provider_service_id":      decision.ProviderServiceID,
		"backend_kind":             decision.BackendKind,
		"locator_hash":             decision.LocatorHash,
		"repository_id":            decision.RepositoryID,
		"outcome":                  string(decision.Outcome),
		"reason":                   decision.Reason,
		"provenance_only":          decision.ProvenanceOnly,
		"candidate_repository_ids": uniqueSortedStrings(decision.CandidateRepositoryIDs),
		"evidence_fact_ids":        uniqueSortedStrings(decision.EvidenceFactIDs),
		"source_layers":            incidentRepositoryCorrelationSourceLayers(decision),
	}
}

// incidentRepositoryCorrelationSourceLayers marks an edge-bearing decision with
// the observed-resource layer in addition to the declaration layer. A
// provenance-only decision (every non-exact/derived outcome) stays
// declaration-layer only, so a downstream scoped predicate that admits on the
// observed-resource layer is fail-closed against ambiguous and unresolved
// routing.
func incidentRepositoryCorrelationSourceLayers(
	decision IncidentRepositoryCorrelationDecision,
) []string {
	layers := []string{string(truth.LayerSourceDeclaration)}
	if !decision.ProvenanceOnly && strings.TrimSpace(decision.RepositoryID) != "" {
		layers = append(layers, string(truth.LayerObservedResource))
	}
	return uniqueSortedStrings(layers)
}
