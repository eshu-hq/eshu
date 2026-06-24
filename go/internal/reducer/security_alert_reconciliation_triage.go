// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

const (
	securityAlertReasonMatchedExactImpact    = "matched_exact_impact"
	securityAlertReasonImpactFindingMissing  = "impact_finding_missing"
	securityAlertReasonOwnedDependencyMissed = "owned_dependency_missing"
	securityAlertReasonProviderAlertStale    = "provider_alert_stale"
	securityAlertReasonOwnedDependencyAmbig  = "owned_dependency_ambiguous"
	securityAlertReasonUnsupportedEcosystem  = "unsupported_ecosystem"
	securityAlertReasonProviderDismissed     = "provider_alert_dismissed"
	securityAlertReasonProviderFixed         = "provider_alert_fixed"
)

// SecurityAlertReconciliationMissingEvidence names the evidence family that
// prevents a provider alert reconciliation from becoming a matched row.
type SecurityAlertReconciliationMissingEvidence struct {
	Kind       string `json:"kind"`
	Reason     string `json:"reason"`
	EvidenceID string `json:"evidence_id,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

func securityAlertMissingEvidence(
	kind string,
	reason string,
	evidenceID string,
) []SecurityAlertReconciliationMissingEvidence {
	return []SecurityAlertReconciliationMissingEvidence{{
		Kind:       strings.TrimSpace(kind),
		Reason:     strings.TrimSpace(reason),
		EvidenceID: strings.TrimSpace(evidenceID),
	}}
}

func securityAlertUnsupportedTriage(
	alert providerSecurityAlert,
) (SecurityAlertReconciliationStatus, string, []SecurityAlertReconciliationMissingEvidence, bool) {
	ecosystem := normalizedSecurityAlertEcosystem(alert.Ecosystem)
	if ecosystem == "" {
		return "", "", nil, false
	}
	if securityAlertEcosystemHasMatcher(ecosystem) {
		return "", "", nil, false
	}
	return SecurityAlertReconciliationUnsupported,
		securityAlertReasonUnsupportedEcosystem,
		securityAlertMissingEvidence("ecosystem_matcher", "unsupported_ecosystem", ""),
		true
}

func normalizedSecurityAlertEcosystem(ecosystem string) string {
	normalized := string(packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem)))
	if normalized != "" {
		return normalized
	}
	return strings.ToLower(strings.TrimSpace(ecosystem))
}

func securityAlertEcosystemHasMatcher(ecosystem string) bool {
	switch ecosystem {
	case string(packageidentity.EcosystemNPM),
		string(packageidentity.EcosystemNuGet),
		string(packageidentity.EcosystemCargo),
		string(packageidentity.EcosystemHex),
		string(packageidentity.EcosystemGoModule),
		string(packageidentity.EcosystemPyPI),
		string(packageidentity.EcosystemSwift),
		string(packageidentity.EcosystemPub),
		string(packageidentity.EcosystemComposer),
		string(packageidentity.EcosystemMaven),
		string(packageidentity.EcosystemRubyGems),
		string(packageidentity.EcosystemOS),
		"redhat",
		"fedora",
		"centos",
		"rocky",
		"alma",
		"amazonlinux",
		"rpm",
		"debian",
		"ubuntu",
		"deb",
		"dpkg",
		"alpine",
		"apk":
		return true
	default:
		return false
	}
}
