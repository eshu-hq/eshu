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
	DB  workloadIdentityExecer
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
	now := reducerWriterNow(w.Now)
	for _, decision := range write.Decisions {
		payload := securityAlertReconciliationPayload(write, decision)
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return SecurityAlertReconciliationWriteResult{}, fmt.Errorf("marshal security alert reconciliation payload: %w", err)
		}
		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			securityAlertReconciliationFactID(write, decision),
			write.ScopeID,
			write.GenerationID,
			securityAlertReconciliationFactKind,
			securityAlertReconciliationStableFactKey(write, decision),
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
			return SecurityAlertReconciliationWriteResult{}, fmt.Errorf("write security alert reconciliation fact: %w", err)
		}
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
	return strings.Join([]string{
		"security_alert_reconciliation",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["provider"])),
		strings.TrimSpace(fmt.Sprint(identity["provider_alert_number"])),
	}, ":")
}

func securityAlertReconciliationIdentity(
	write SecurityAlertReconciliationWrite,
	decision SecurityAlertReconciliationDecision,
) map[string]any {
	return map[string]any{
		"generation_id":          strings.TrimSpace(write.GenerationID),
		"provider":               strings.TrimSpace(decision.Provider),
		"provider_alert_number":  decision.ProviderAlertNumber,
		"provider_alert_fact_id": strings.TrimSpace(decision.ProviderAlertFactID),
		"repository_id":          strings.TrimSpace(decision.RepositoryID),
		"scope_id":               strings.TrimSpace(write.ScopeID),
	}
}

func securityAlertReconciliationPayload(
	write SecurityAlertReconciliationWrite,
	decision SecurityAlertReconciliationDecision,
) map[string]any {
	return map[string]any{
		"reducer_domain":         string(DomainSecurityAlertReconciliation),
		"intent_id":              write.IntentID,
		"scope_id":               write.ScopeID,
		"generation_id":          write.GenerationID,
		"source_system":          write.SourceSystem,
		"cause":                  write.Cause,
		"provider":               decision.Provider,
		"provider_alert_id":      decision.ProviderAlertID,
		"provider_alert_number":  decision.ProviderAlertNumber,
		"provider_alert_fact_id": decision.ProviderAlertFactID,
		"provider_state":         decision.ProviderState,
		"repository_id":          decision.RepositoryID,
		"package_id":             decision.PackageID,
		"ecosystem":              decision.Ecosystem,
		"package_name":           decision.PackageName,
		"manifest_path":          decision.ManifestPath,
		"dependency_scope":       decision.DependencyScope,
		"relationship":           decision.Relationship,
		"ghsa_ids":               uniqueSortedStrings(decision.GHSAIDs),
		"cve_ids":                uniqueSortedStrings(decision.CVEIDs),
		"vulnerable_range":       decision.VulnerableRange,
		"patched_version":        decision.PatchedVersion,
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
		"reconciliation_status":  string(decision.Status),
		"eshu_impact_status":     decision.EshuImpactStatus,
		"eshu_impact_finding_id": decision.EshuImpactFindingID,
		"reason":                 decision.Reason,
		"canonical_writes":       decision.CanonicalWrites,
		"evidence_fact_ids":      uniqueSortedStrings(decision.EvidenceFactIDs),
		"dependency_evidence_id": decision.DependencyEvidenceID,
		"impact_evidence_id":     decision.ImpactEvidenceID,
		"correlation_kind":       securityAlertReconciliationFactKind,
		"source_layers": []string{
			string(truth.LayerSourceDeclaration),
			string(truth.LayerObservedResource),
		},
	}
}
