// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package security

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// securityAlertReconciliationCandidateFactKinds are the fact kinds
// securityAlertReconciliationTriggerFact accepts.
var securityAlertReconciliationCandidateFactKinds = []string{
	facts.SecurityAlertRepositoryAlertFactKind, facts.PackageRegistryPackageFactKind,
}

// BuildSecurityAlertReconciliationReducerIntent builds the scope-generation
// work item that reconciles provider alerts with package registry identities.
func BuildSecurityAlertReconciliationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstAcrossKinds(securityAlertReconciliationTriggerFact, securityAlertReconciliationCandidateFactKinds...)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainSecurityAlertReconciliation,
		EntityKey:    "security_alert_reconciliation:" + scopeID,
		Reason:       securityAlertReconciliationReason(envelope),
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
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
