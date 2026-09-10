// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// This file is package correlation's manifest-consumption bridge for the
// securityalert family (issue #6061). ExtractSecurityAlertManifestConsumptions
// and securityAlertManifestConsumptionMatches moved here with package
// consumption correlation, rather than with the rest of
// security_alert_reconciliation.go, because they depend on
// ExtractPackageManifestDependencies and PackageConsumptionKeys: decode and
// package-identity-normalization logic shared with families that have not
// moved out of root yet (supply_chain_impact). A family subpackage may never
// import the reducer root, so this bridge is wired into
// securityalert.SecurityAlertReconciliationHandler at its one construction
// site instead (defaults_additive_domains_supply_chain.go), and called
// directly by supply_chain_impact_security_alert.go, which does not go through
// a builder; see securityalert.ManifestConsumptionExtractor.

// ExtractSecurityAlertManifestConsumptions matches decoded provider alerts
// against Eshu-observed manifest/lockfile dependency evidence, admitting one
// securityalert.SecurityAlertConsumption per match. Logic unchanged from
// before the securityalert family move; only the alert/consumption types are
// now the family's exported ones. Exported because the reducer root wires it
// into the security-alert reconciliation handler and supply-chain impact
// calls it directly.
func ExtractSecurityAlertManifestConsumptions(
	alerts []securityalert.ProviderSecurityAlert,
	envelopes []facts.Envelope,
) []securityalert.SecurityAlertConsumption {
	if len(alerts) == 0 {
		return nil
	}
	dependencies := ExtractPackageManifestDependencies(envelopes)
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
				EvidenceKind:     factload.FactKindContentEntity,
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
				DirectDependency: payloadcore.CloneBoolPointer(dependency.DirectDependency),
				DependencyScope:  dependency.DependencyScope,
				Lockfile:         dependency.Lockfile,
			})
		}
	}
	return consumptions
}

func securityAlertManifestConsumptionMatches(
	alert securityalert.ProviderSecurityAlert,
	dependency PackageManifestDependency,
) bool {
	if strings.TrimSpace(alert.PackageID) == "" ||
		!securityalert.SecurityAlertRepositoryScopeMatches(alert, securityalert.SecurityAlertConsumption{
			RepositoryID:   dependency.RepositoryID,
			RepositoryName: dependency.RepositoryName,
		}) {
		return false
	}
	alertKeys := payloadcore.StringSet(PackageConsumptionKeys(
		alert.Ecosystem,
		securityalert.SecurityAlertPackageNameCandidates(alert)...,
	))
	if len(alertKeys) == 0 {
		return false
	}
	for _, key := range PackageConsumptionKeys(dependency.PackageManager, packageManifestDependencyNameCandidates(dependency)...) {
		if _, ok := alertKeys[key]; ok {
			return true
		}
	}
	return false
}

func packageManifestDependencyNameCandidates(dependency PackageManifestDependency) []string {
	names := []string{dependency.DependencyName}
	namespace := strings.TrimSpace(dependency.PackageNamespace)
	name := strings.TrimSpace(dependency.DependencyName)
	if namespace != "" && name != "" {
		names = append(names, namespace+"/"+name)
	}
	return payloadcore.UniqueSortedStrings(names)
}

// SecurityAlertPackageNameMatchesDependency reports whether any name form of
// a manifest dependency matches a provider alert's package identity.
// Exported because supply-chain impact scopes manifest evidence to alerts
// without going through the consumption builder.
func SecurityAlertPackageNameMatchesDependency(
	alert securityalert.ProviderSecurityAlert,
	dependency PackageManifestDependency,
) bool {
	for _, dependencyName := range packageManifestDependencyNameCandidates(dependency) {
		if SecurityAlertPackageNameMatches(alert, dependencyName) {
			return true
		}
	}
	return false
}

// SecurityAlertPackageNameMatches reports whether a manifest dependency name
// matches a provider alert's package identity in any of its forms: the raw
// alert package name, or a name parsed from the alert package ID as either an
// Eshu package-registry URI or a package URL. It travels with the
// manifest-dependency bridge (it takes securityalert.ProviderSecurityAlert,
// so payloadcore's no-family-dependencies boundary excludes it) and is
// exported because supply-chain impact scopes manifest evidence through it.
func SecurityAlertPackageNameMatches(alert securityalert.ProviderSecurityAlert, dependencyName string) bool {
	dependencyName = strings.ToLower(strings.TrimSpace(dependencyName))
	if dependencyName == "" {
		return false
	}
	for _, candidate := range []string{
		alert.PackageName,
		payloadcore.PackageNameFromPackageID(alert.PackageID),
		payloadcore.PackageNameFromPURL(alert.PackageID),
	} {
		if strings.ToLower(strings.TrimSpace(candidate)) == dependencyName {
			return true
		}
	}
	return false
}
