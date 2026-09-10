// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

func supplyChainImpactUsesSecurityAlertScope(intent reducercontract.Intent, envelopes []facts.Envelope) bool {
	if !strings.EqualFold(strings.TrimSpace(intent.SourceSystem), "security_alert") &&
		!strings.HasPrefix(strings.TrimSpace(intent.ScopeID), "security-alert:") {
		return false
	}
	return len(securityalert.ExtractProviderSecurityAlerts(envelopes)) > 0
}

func scopeSupplyChainImpactEvidenceToSecurityAlerts(envelopes []facts.Envelope) []facts.Envelope {
	alerts := securityalert.ExtractProviderSecurityAlerts(envelopes)
	if len(alerts) == 0 {
		return envelopes
	}
	allowedRepositoryIDs := securityAlertScopedRepositoryIDs(alerts, envelopes)
	out := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		if securityAlertScopedEnvelopeAllowed(alerts, allowedRepositoryIDs, envelope) {
			out = append(out, envelope)
		}
	}
	return out
}

func securityAlertScopedRepositoryIDs(
	alerts []securityalert.ProviderSecurityAlert,
	envelopes []facts.Envelope,
) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, alert := range alerts {
		if securityAlertRepositoryIDIsCanonical(alert.RepositoryID) {
			allowed[strings.TrimSpace(alert.RepositoryID)] = struct{}{}
		}
	}
	consumptions := securityalert.ExtractSecurityAlertConsumptions(envelopes)
	for _, alert := range alerts {
		for _, consumption := range consumptions {
			if consumption.PackageID != alert.PackageID {
				continue
			}
			if securityalert.SecurityAlertRepositoryScopeMatches(alert, consumption) {
				allowed[strings.TrimSpace(consumption.RepositoryID)] = struct{}{}
			}
		}
	}
	for _, dependency := range correlation.ExtractPackageManifestDependencies(envelopes) {
		if !securityAlertManifestDependencyMatches(alerts, dependency) {
			continue
		}
		allowed[strings.TrimSpace(dependency.RepositoryID)] = struct{}{}
	}
	return allowed
}

func securityAlertScopedEnvelopeAllowed(
	alerts []securityalert.ProviderSecurityAlert,
	allowedRepositoryIDs map[string]struct{},
	envelope facts.Envelope,
) bool {
	switch envelope.FactKind {
	case correlation.PackageConsumptionFactKind:
		consumption := securityalert.SecurityAlertConsumption{
			RepositoryID:   payloadcore.PayloadStr(envelope.Payload, "repository_id"),
			RepositoryName: payloadcore.PayloadStr(envelope.Payload, "repository_name"),
			PackageID:      payloadcore.PayloadStr(envelope.Payload, "package_id"),
		}
		for _, alert := range alerts {
			if consumption.PackageID == alert.PackageID && securityalert.SecurityAlertRepositoryScopeMatches(alert, consumption) {
				return true
			}
		}
		return false
	case factload.FactKindContentEntity:
		for _, dependency := range correlation.ExtractPackageManifestDependencies([]facts.Envelope{envelope}) {
			if securityAlertScopedManifestDependencyAllowed(alerts, allowedRepositoryIDs, dependency) {
				return true
			}
		}
		return false
	case reducercontract.ContainerImageIdentityFactKind, cicdrun.CICDRunCorrelationFactKind, reducercontract.ServiceCatalogCorrelationFactKind:
		return securityAlertRepositoryIDAllowed(payloadcore.PayloadStr(envelope.Payload, "repository_id"), allowedRepositoryIDs)
	default:
		return true
	}
}

func securityAlertManifestDependencyMatches(
	alerts []securityalert.ProviderSecurityAlert,
	dependency correlation.PackageManifestDependency,
) bool {
	if dependency.RepositoryID == "" || dependency.DependencyName == "" {
		return false
	}
	for _, alert := range alerts {
		if !securityalert.SecurityAlertRepositoryScopeMatches(alert, securityalert.SecurityAlertConsumption{
			RepositoryID:   dependency.RepositoryID,
			RepositoryName: dependency.RepositoryName,
		}) {
			continue
		}
		if correlation.SecurityAlertPackageNameMatchesDependency(alert, dependency) {
			return true
		}
	}
	return false
}

func securityAlertScopedManifestDependencyAllowed(
	alerts []securityalert.ProviderSecurityAlert,
	allowedRepositoryIDs map[string]struct{},
	dependency correlation.PackageManifestDependency,
) bool {
	if securityAlertRepositoryIDAllowed(dependency.RepositoryID, allowedRepositoryIDs) &&
		securityAlertManifestDependencyPackageMatches(alerts, dependency) {
		return true
	}
	return securityAlertManifestDependencyMatches(alerts, dependency)
}

func securityAlertManifestDependencyPackageMatches(
	alerts []securityalert.ProviderSecurityAlert,
	dependency correlation.PackageManifestDependency,
) bool {
	for _, alert := range alerts {
		if correlation.SecurityAlertPackageNameMatchesDependency(alert, dependency) {
			return true
		}
	}
	return false
}

func securityAlertRepositoryIDAllowed(repositoryID string, allowed map[string]struct{}) bool {
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID == "" {
		return false
	}
	_, ok := allowed[repositoryID]
	return ok
}

func securityAlertRepositoryIDIsCanonical(repositoryID string) bool {
	repositoryID = strings.TrimSpace(repositoryID)
	return repositoryID != "" && !strings.HasPrefix(repositoryID, "security-alert:")
}
