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

const serviceCatalogCorrelationFactKind = "reducer_service_catalog_correlation"

// PostgresServiceCatalogCorrelationWriter stores reducer-owned service catalog
// correlation decisions in the shared fact store.
type PostgresServiceCatalogCorrelationWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteServiceCatalogCorrelations persists every service-catalog outcome so
// callers can distinguish correlated, ambiguous, stale, unresolved, and
// rejected catalog declarations.
func (w PostgresServiceCatalogCorrelationWriter) WriteServiceCatalogCorrelations(
	ctx context.Context,
	write ServiceCatalogCorrelationWrite,
) (ServiceCatalogCorrelationWriteResult, error) {
	if w.DB == nil {
		return ServiceCatalogCorrelationWriteResult{}, fmt.Errorf("service catalog correlation database is required")
	}
	now := reducerWriterNow(w.Now)
	for _, decision := range write.Decisions {
		payload := serviceCatalogCorrelationPayload(write, decision)
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return ServiceCatalogCorrelationWriteResult{}, fmt.Errorf("marshal service catalog correlation payload: %w", err)
		}
		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			serviceCatalogCorrelationFactID(write, decision),
			write.ScopeID,
			write.GenerationID,
			serviceCatalogCorrelationFactKind,
			serviceCatalogCorrelationStableFactKey(write, decision),
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
			return ServiceCatalogCorrelationWriteResult{}, fmt.Errorf("write service catalog correlation fact: %w", err)
		}
	}
	return ServiceCatalogCorrelationWriteResult{
		FactsWritten:    len(write.Decisions),
		EvidenceSummary: fmt.Sprintf("wrote service catalog correlations=%d", len(write.Decisions)),
	}, nil
}

func serviceCatalogCorrelationFactID(
	write ServiceCatalogCorrelationWrite,
	decision ServiceCatalogCorrelationDecision,
) string {
	return serviceCatalogCorrelationFactKind + ":" + facts.StableID(
		serviceCatalogCorrelationFactKind,
		serviceCatalogCorrelationIdentity(write, decision),
	)
}

func serviceCatalogCorrelationStableFactKey(
	write ServiceCatalogCorrelationWrite,
	decision ServiceCatalogCorrelationDecision,
) string {
	identity := serviceCatalogCorrelationIdentity(write, decision)
	return strings.Join([]string{
		"service_catalog_correlation",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["provider"])),
		strings.TrimSpace(fmt.Sprint(identity["entity_ref"])),
	}, ":")
}

func serviceCatalogCorrelationIdentity(
	write ServiceCatalogCorrelationWrite,
	decision ServiceCatalogCorrelationDecision,
) map[string]any {
	return map[string]any{
		"scope_id":      strings.TrimSpace(write.ScopeID),
		"generation_id": strings.TrimSpace(write.GenerationID),
		"provider":      strings.TrimSpace(decision.Provider),
		"entity_ref":    strings.TrimSpace(decision.EntityRef),
	}
}

func serviceCatalogCorrelationPayload(
	write ServiceCatalogCorrelationWrite,
	decision ServiceCatalogCorrelationDecision,
) map[string]any {
	return map[string]any{
		"reducer_domain":           string(DomainServiceCatalogCorrelation),
		"intent_id":                write.IntentID,
		"scope_id":                 write.ScopeID,
		"generation_id":            write.GenerationID,
		"source_system":            write.SourceSystem,
		"cause":                    write.Cause,
		"provider":                 decision.Provider,
		"entity_ref":               decision.EntityRef,
		"entity_type":              decision.EntityType,
		"display_name":             decision.DisplayName,
		"repository_id":            decision.RepositoryID,
		"service_id":               decision.ServiceID,
		"workload_id":              decision.WorkloadID,
		"owner_ref":                decision.OwnerRef,
		"lifecycle":                decision.Lifecycle,
		"tier":                     decision.Tier,
		"outcome":                  string(decision.Outcome),
		"reason":                   decision.Reason,
		"provenance_only":          decision.ProvenanceOnly,
		"drift_kind":               decision.DriftKind,
		"drift_status":             decision.DriftStatus,
		"candidate_repository_ids": uniqueSortedStrings(decision.CandidateRepositoryIDs),
		"evidence_fact_ids":        uniqueSortedStrings(decision.EvidenceFactIDs),
		"required_anchor_keys":     append([]string(nil), decision.RequiredAnchorKeys...),
		"source_layers":            serviceCatalogCorrelationSourceLayers(decision),
	}
}

func serviceCatalogCorrelationSourceLayers(decision ServiceCatalogCorrelationDecision) []string {
	layers := []string{string(truth.LayerSourceDeclaration)}
	if !decision.ProvenanceOnly && decision.RepositoryID != "" {
		layers = append(layers, string(truth.LayerObservedResource))
	}
	return uniqueSortedStrings(layers)
}
