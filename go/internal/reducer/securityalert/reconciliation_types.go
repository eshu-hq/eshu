// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import "time"

// ProviderSecurityAlert is one decoded security_alert.repository_alert fact.
// ExtractProviderSecurityAlerts and ExtractProviderSecurityAlertsWithQuarantine
// are the only constructors; they seed the embedded
// SecurityAlertReconciliationDecision with the alert's provider-reported and
// identity fields (ProviderAlertFactID, RepositoryID, PackageID, CVEIDs,
// GHSAIDs, and the rest) and leave every reconciliation-only field
// (Status, Reason, ReasonCode, MissingEvidence, EshuImpactStatus, ...) at its
// zero value until BuildSecurityAlertReconciliations or
// BuildSecurityAlertReconciliationsWithQuarantine classifies it. The
// unexported updatedAtTime carries the alert's provider UpdatedAt parsed into
// a comparable time.Time; MatchSecurityAlertConsumption uses it to decide
// whether a stale-consumption candidate postdates the alert, so a caller that
// builds a ProviderSecurityAlert outside the decode path (there is none in
// this package) must not rely on this field being set.
type ProviderSecurityAlert struct {
	SecurityAlertReconciliationDecision
	updatedAtTime time.Time
}

// SecurityAlertConsumption is one piece of Eshu-owned dependency-consumption
// evidence a provider alert is reconciled against — either a
// reducer_package_consumption_correlation fact (via
// ExtractSecurityAlertConsumptions) or a manifest/lockfile dependency fact
// supplied by the caller-injected ManifestConsumptionExtractor.
// RepositoryID/RepositoryName/PackageID identify the owned package;
// RelativePath is the manifest or lockfile path the evidence was observed at.
// ObservedVersion is authoritative when non-empty; otherwise
// InstalledVersion/DependencyRange/RequestedRange carry the manifest-declared
// range a caller can resolve a version from. FactID is the evidence fact this
// consumption came from and becomes a decision's DependencyEvidenceID when
// matched; a zero value (empty FactID) signals "no consumption," the sentinel
// SecurityAlertRepositoryScopeMatches, MatchSecurityAlertConsumption, and
// selectSecurityAlertConsumption use to report "no match."
type SecurityAlertConsumption struct {
	FactID           string
	EvidenceKind     string
	RepositoryID     string
	RepositoryName   string
	PackageID        string
	RelativePath     string
	ObservedAt       time.Time
	DependencyRange  string
	ObservedVersion  string
	InstalledVersion string
	RequestedRange   string
	DependencyPath   []string
	DependencyDepth  int
	DirectDependency *bool
	DependencyScope  string
	Lockfile         bool
}

// SecurityAlertImpact is one reducer_supply_chain_impact_finding fact,
// projected down to the fields matchSecurityAlertImpact needs to decide
// whether a reducer impact finding already covers a provider alert's
// repository/package/advisory identity. RepositoryID and PackageID scope the
// match; CVEID and AdvisoryID are compared against a ProviderSecurityAlert's
// CVEIDs/GHSAIDs via SecurityAlertIDMatches (case-insensitive). Status is
// copied onto a matched decision's EshuImpactStatus. A zero value (empty
// FactID) is the "no impact finding" sentinel matchSecurityAlertImpact
// returns when nothing matches.
type SecurityAlertImpact struct {
	FactID       string
	RepositoryID string
	PackageID    string
	CVEID        string
	AdvisoryID   string
	Status       string
}
