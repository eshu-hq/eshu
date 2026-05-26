package projector

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func buildSecurityAlertReconciliationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if !securityAlertReconciliationTriggerFact(envelope) {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainSecurityAlertReconciliation,
			EntityKey:    "security_alert_reconciliation:" + scopeValue.ScopeID,
			Reason:       securityAlertReconciliationReason(envelope),
			FactID:       envelope.FactID,
			SourceSystem: securityAlertSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}

func securityAlertReconciliationTriggerFact(envelope facts.Envelope) bool {
	switch envelope.FactKind {
	case facts.SecurityAlertRepositoryAlertFactKind,
		facts.PackageRegistryPackageFactKind:
		return true
	default:
		return false
	}
}

func securityAlertReconciliationReason(envelope facts.Envelope) string {
	if envelope.FactKind == facts.PackageRegistryPackageFactKind {
		return "package registry identity observed"
	}
	return "provider security alert evidence observed"
}

func validateSecurityAlertSchemaVersion(envelope facts.Envelope) error {
	want, ok := facts.SecurityAlertSchemaVersion(envelope.FactKind)
	if !ok {
		return nil
	}
	got := strings.TrimSpace(envelope.SchemaVersion)
	if got != want {
		return fmt.Errorf("unsupported security alert schema_version %q for %s, want %s", got, envelope.FactKind, want)
	}
	return nil
}

func securityAlertSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
