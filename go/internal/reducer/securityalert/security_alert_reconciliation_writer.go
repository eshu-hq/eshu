// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

const securityAlertReconciliationFactKind = "reducer_security_alert_reconciliation"

// SecurityAlertReconciliationWrite carries provider alert reconciliation
// decisions for durable reducer facts.
type SecurityAlertReconciliationWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Decisions    []SecurityAlertReconciliationDecision
}

// SecurityAlertReconciliationWriteResult summarizes durable reconciliation
// publication.
type SecurityAlertReconciliationWriteResult struct {
	CanonicalWrites int
	FactsWritten    int
	EvidenceSummary string
}

// SecurityAlertReconciliationWriter persists reducer-owned provider alert
// reconciliation facts.
type SecurityAlertReconciliationWriter interface {
	WriteSecurityAlertReconciliations(
		context.Context,
		SecurityAlertReconciliationWrite,
	) (SecurityAlertReconciliationWriteResult, error)
}

// PostgresSecurityAlertReconciliationWriter stores provider alert
// reconciliation decisions in the shared fact store.
type PostgresSecurityAlertReconciliationWriter struct {
	DB  factwrite.Execer
	Now func() time.Time
}

// WriteSecurityAlertReconciliations persists provider alert reconciliation
// decisions without admitting supply-chain impact truth.
func (w PostgresSecurityAlertReconciliationWriter) WriteSecurityAlertReconciliations(
	ctx context.Context,
	write SecurityAlertReconciliationWrite,
) (SecurityAlertReconciliationWriteResult, error) {
	if w.DB == nil {
		return SecurityAlertReconciliationWriteResult{}, fmt.Errorf("security alert reconciliation database is required")
	}
	now := factwrite.Now(w.Now)
	rows := make([]factwrite.Row, 0, len(write.Decisions))
	for _, decision := range write.Decisions {
		scopeID := securityAlertReconciliationWriteScopeID(write, decision)
		generationID := securityAlertReconciliationWriteGenerationID(write, decision)
		payload := securityAlertReconciliationPayload(write, decision)
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return SecurityAlertReconciliationWriteResult{}, fmt.Errorf("marshal security alert reconciliation payload: %w", err)
		}
		rows = append(rows, factwrite.Row{
			FactID:           securityAlertReconciliationFactID(write, decision),
			ScopeID:          scopeID,
			GenerationID:     generationID,
			FactKind:         securityAlertReconciliationFactKind,
			StableFactKey:    securityAlertReconciliationStableFactKey(write, decision),
			CollectorKind:    factwrite.CollectorKind(write.SourceSystem),
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
	if err := factwrite.BatchInsertFacts(ctx, w.DB, rows); err != nil {
		return SecurityAlertReconciliationWriteResult{}, fmt.Errorf("write security alert reconciliation fact: %w", err)
	}
	return SecurityAlertReconciliationWriteResult{
		CanonicalWrites: 0,
		FactsWritten:    len(write.Decisions),
		EvidenceSummary: fmt.Sprintf(
			"wrote security alert reconciliations=%d canonical_writes=0",
			len(write.Decisions),
		),
	}, nil
}

func securityAlertReconciliationFactID(
	write SecurityAlertReconciliationWrite,
	decision SecurityAlertReconciliationDecision,
) string {
	return securityAlertReconciliationFactKind + ":" + facts.StableID(
		securityAlertReconciliationFactKind,
		securityAlertReconciliationIdentity(write, decision),
	)
}

func securityAlertReconciliationStableFactKey(
	write SecurityAlertReconciliationWrite,
	decision SecurityAlertReconciliationDecision,
) string {
	identity := securityAlertReconciliationIdentity(write, decision)
	return "security_alert_reconciliation:" + facts.StableID(securityAlertReconciliationFactKind, identity)
}

func securityAlertReconciliationIdentity(
	write SecurityAlertReconciliationWrite,
	decision SecurityAlertReconciliationDecision,
) map[string]any {
	scopeID := securityAlertReconciliationWriteScopeID(write, decision)
	generationID := securityAlertReconciliationWriteGenerationID(write, decision)
	return map[string]any{
		"generation_id":         generationID,
		"provider":              strings.TrimSpace(decision.Provider),
		"provider_alert_id":     payloadcore.FirstNonBlank(decision.ProviderAlertID, fmt.Sprint(decision.ProviderAlertNumber)),
		"provider_alert_number": decision.ProviderAlertNumber,
		"provider_repository_id": payloadcore.FirstNonBlank(
			decision.ProviderRepositoryID,
			decision.ProviderAlertScopeID,
			scopeID,
		),
		"scope_id":   scopeID,
		"package_id": strings.TrimSpace(decision.PackageID),
		"cve_ids":    payloadcore.UniqueSortedStrings(decision.CVEIDs),
		"ghsa_ids":   payloadcore.UniqueSortedStrings(decision.GHSAIDs),
	}
}

func securityAlertReconciliationPayload(
	write SecurityAlertReconciliationWrite,
	decision SecurityAlertReconciliationDecision,
) map[string]any {
	scopeID := securityAlertReconciliationWriteScopeID(write, decision)
	generationID := securityAlertReconciliationWriteGenerationID(write, decision)
	return map[string]any{
		"reducer_domain":         string(reducercontract.DomainSecurityAlertReconciliation),
		"intent_id":              write.IntentID,
		"scope_id":               scopeID,
		"generation_id":          generationID,
		"source_system":          write.SourceSystem,
		"cause":                  write.Cause,
		"provider":               decision.Provider,
		"provider_alert_id":      decision.ProviderAlertID,
		"provider_alert_number":  decision.ProviderAlertNumber,
		"provider_alert_fact_id": decision.ProviderAlertFactID,
		"provider_state":         decision.ProviderState,
		"repository_id":          decision.RepositoryID,
		"provider_repository_id": decision.ProviderRepositoryID,
		"repository_name":        decision.RepositoryName,
		"package_id":             decision.PackageID,
		"ecosystem":              decision.Ecosystem,
		"package_name":           decision.PackageName,
		"manifest_path":          decision.ManifestPath,
		"dependency_scope":       decision.DependencyScope,
		"relationship":           decision.Relationship,
		"ghsa_ids":               payloadcore.UniqueSortedStrings(decision.GHSAIDs),
		"cve_ids":                payloadcore.UniqueSortedStrings(decision.CVEIDs),
		"vulnerable_range":       decision.VulnerableRange,
		"patched_version":        decision.PatchedVersion,
		"observed_version":       decision.ObservedVersion,
		"requested_range":        decision.RequestedRange,
		"dependency_range":       decision.DependencyRange,
		"severity":               decision.Severity,
		"cvss":                   decision.CVSS,
		"epss":                   decision.EPSS,
		"cwes":                   decision.CWEs,
		"summary":                decision.Summary,
		"source_url":             decision.SourceURL,
		"created_at":             decision.CreatedAt,
		"updated_at":             decision.UpdatedAt,
		"fixed_at":               decision.FixedAt,
		"dismissed_at":           decision.DismissedAt,
		"source_freshness":       payloadcore.FirstNonBlank(decision.SourceFreshness, "active"),
		"collection_coverage_state": payloadcore.FirstNonBlank(
			decision.CollectionCoverageState,
			"complete",
		),
		"collection_truncated":          decision.CollectionTruncated,
		"collection_pages_fetched":      decision.CollectionPagesFetched,
		"collection_state_filter":       decision.CollectionStateFilter,
		"collection_incomplete_reasons": payloadcore.UniqueSortedStrings(decision.CollectionIncompleteReasons),
		"reconciliation_status":         string(decision.Status),
		"eshu_impact_status":            decision.EshuImpactStatus,
		"eshu_impact_finding_id":        decision.EshuImpactFindingID,
		"reason":                        decision.Reason,
		"reason_code":                   decision.ReasonCode,
		"missing_evidence":              decision.MissingEvidence,
		"package_missing_evidence":      payloadcore.UniqueSortedStrings(decision.PackageMissingEvidence),
		"canonical_writes":              decision.CanonicalWrites,
		"evidence_fact_ids":             payloadcore.UniqueSortedStrings(decision.EvidenceFactIDs),
		"dependency_evidence_id":        decision.DependencyEvidenceID,
		"dependency_evidence_kind":      decision.DependencyEvidenceKind,
		"impact_evidence_id":            decision.ImpactEvidenceID,
		"correlation_kind":              securityAlertReconciliationFactKind,
		"source_layers": []string{
			string(truth.LayerSourceDeclaration),
			string(truth.LayerObservedResource),
		},
	}
}

func securityAlertReconciliationWriteScopeID(
	write SecurityAlertReconciliationWrite,
	decision SecurityAlertReconciliationDecision,
) string {
	if scopeID := strings.TrimSpace(decision.ProviderAlertScopeID); scopeID != "" {
		return scopeID
	}
	return strings.TrimSpace(write.ScopeID)
}

func securityAlertReconciliationWriteGenerationID(
	write SecurityAlertReconciliationWrite,
	decision SecurityAlertReconciliationDecision,
) string {
	if generationID := strings.TrimSpace(decision.ProviderAlertGenerationID); generationID != "" {
		return generationID
	}
	return strings.TrimSpace(write.GenerationID)
}
