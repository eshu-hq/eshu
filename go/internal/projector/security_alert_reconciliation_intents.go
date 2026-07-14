// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// securityAlertReconciliationCandidateFactKinds are the fact kinds
// securityAlertReconciliationTriggerFact accepts.
var securityAlertReconciliationCandidateFactKinds = []string{
	facts.SecurityAlertRepositoryAlertFactKind, facts.PackageRegistryPackageFactKind,
}

func buildSecurityAlertReconciliationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstAcrossKinds(securityAlertReconciliationTriggerFact, securityAlertReconciliationCandidateFactKinds...)
	if !ok {
		return ReducerIntent{}, false
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
