// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
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

func securityAlertSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
