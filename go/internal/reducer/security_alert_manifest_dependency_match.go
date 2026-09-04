// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// This file is the reducer root's manifest-consumption bridge for the
// securityalert family (issue #6061). extractSecurityAlertManifestConsumptions
// and securityAlertManifestConsumptionMatches stayed in root, rather than
// moving with the rest of security_alert_reconciliation.go, because they
// depend on extractPackageManifestDependencies and packageConsumptionKeys
// (package_consumption_correlation.go): root-owned decode and
// package-identity-normalization logic shared with families that have not
// moved out of root yet (supply_chain_impact, package consumption
// correlation). A family subpackage may never import the reducer root, so
// this bridge is wired into securityalert.SecurityAlertReconciliationHandler
// and passed at each securityalert.BuildSecurityAlertReconciliationsWithQuarantine
// call site instead (defaults_additive_domains_supply_chain.go,
// supply_chain_impact_security_alert.go); see
// securityalert.ManifestConsumptionExtractor.

// extractSecurityAlertManifestConsumptions matches decoded provider alerts
// against Eshu-observed manifest/lockfile dependency evidence, admitting one
// securityalert.SecurityAlertConsumption per match. Logic unchanged from
// before the securityalert family move; only the alert/consumption types are
// now the family's exported ones.
func extractSecurityAlertManifestConsumptions(
	alerts []securityalert.ProviderSecurityAlert,
	envelopes []facts.Envelope,
) []securityalert.SecurityAlertConsumption {
	if len(alerts) == 0 {
		return nil
	}
	dependencies := extractPackageManifestDependencies(envelopes)
	if len(dependencies) == 0 {
		return nil
	}
	consumptions := make([]securityalert.SecurityAlertConsumption, 0)
	for _, dependency := range dependencies {
		for _, alert := range alerts {
			if !securityAlertManifestConsumptionMatches(alert, dependency) {
				continue
			}
			consumptions = append(consumptions, securityalert.SecurityAlertConsumption{
				FactID:           dependency.FactID,
				EvidenceKind:     factKindContentEntity,
				RepositoryID:     dependency.RepositoryID,
				RepositoryName:   dependency.RepositoryName,
				PackageID:        alert.PackageID,
				RelativePath:     dependency.RelativePath,
				ObservedAt:       dependency.ObservedAt,
				DependencyRange:  dependency.DependencyRange,
				ObservedVersion:  dependency.ObservedVersion,
				InstalledVersion: dependency.InstalledVersion,
				RequestedRange:   dependency.RequestedRange,
				DependencyPath:   append([]string(nil), dependency.DependencyPath...),
				DependencyDepth:  dependency.DependencyDepth,
				DirectDependency: cloneBoolPointer(dependency.DirectDependency),
				DependencyScope:  dependency.DependencyScope,
				Lockfile:         dependency.Lockfile,
			})
		}
	}
	return consumptions
}

func securityAlertManifestConsumptionMatches(
	alert securityalert.ProviderSecurityAlert,
	dependency packageManifestDependency,
) bool {
	if strings.TrimSpace(alert.PackageID) == "" ||
		!securityalert.SecurityAlertRepositoryScopeMatches(alert, securityalert.SecurityAlertConsumption{
			RepositoryID:   dependency.RepositoryID,
			RepositoryName: dependency.RepositoryName,
		}) {
		return false
	}
	alertKeys := stringSet(packageConsumptionKeys(
		alert.Ecosystem,
		securityalert.SecurityAlertPackageNameCandidates(alert)...,
	))
	if len(alertKeys) == 0 {
		return false
	}
	for _, key := range packageConsumptionKeys(dependency.PackageManager, packageManifestDependencyNameCandidates(dependency)...) {
		if _, ok := alertKeys[key]; ok {
			return true
		}
	}
	return false
}

func packageManifestDependencyNameCandidates(dependency packageManifestDependency) []string {
	names := []string{dependency.DependencyName}
	namespace := strings.TrimSpace(dependency.PackageNamespace)
	name := strings.TrimSpace(dependency.DependencyName)
	if namespace != "" && name != "" {
		names = append(names, namespace+"/"+name)
	}
	return uniqueSortedStrings(names)
}

func securityAlertPackageNameMatchesDependency(
	alert securityalert.ProviderSecurityAlert,
	dependency packageManifestDependency,
) bool {
	for _, dependencyName := range packageManifestDependencyNameCandidates(dependency) {
		if securityAlertPackageNameMatches(alert, dependencyName) {
			return true
		}
	}
	return false
}
