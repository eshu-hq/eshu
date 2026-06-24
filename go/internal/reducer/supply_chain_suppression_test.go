// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestEvaluateSupplyChainSuppressionActiveByDefault(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0001",
		PackageID:    testImpactPackageID,
		RepositoryID: testImpactRepositoryID,
	}

	decision := EvaluateSupplyChainSuppression(finding, nil, time.Now())
	if decision.State != SupplyChainSuppressionStateActive {
		t.Fatalf("State = %q, want %q for finding without suppressions", decision.State, SupplyChainSuppressionStateActive)
	}
	if decision.SuppressionID != "" {
		t.Fatalf("SuppressionID = %q, want empty when no suppression matched", decision.SuppressionID)
	}
}

func TestEvaluateSupplyChainSuppressionAppliesNotAffectedWhenScopeMatches(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0001",
		PackageID:    testImpactPackageID,
		RepositoryID: testImpactRepositoryID,
		EvidencePath: []string{"vulnerability.cve", "vulnerability.affected_package", "package.consumption"},
	}
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-1",
		Source:        facts.VulnerabilitySuppressionSourceVEX,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		Author:        "vex:openvex/operator@example.com",
		AuthoredAt:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Reason:        "vulnerable function never called",
		Scope: vulnerabilitySuppressionScope{
			CVEID:        "CVE-2026-0001",
			PackageID:    testImpactPackageID,
			RepositoryID: testImpactRepositoryID,
		},
		VEXDocumentID:  "https://example.com/vex/openvex.json",
		VEXStatementID: "stmt-1",
		EvidenceRef:    "vex:openvex/stmt-1",
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("State = %q, want %q", decision.State, SupplyChainSuppressionStateNotAffected)
	}
	if decision.SuppressionID != "suppression-1" {
		t.Fatalf("SuppressionID = %q, want suppression-1", decision.SuppressionID)
	}
	if decision.Source != facts.VulnerabilitySuppressionSourceVEX {
		t.Fatalf("Source = %q, want %q", decision.Source, facts.VulnerabilitySuppressionSourceVEX)
	}
	if decision.Reason == "" {
		t.Fatalf("Reason = empty, want explanation")
	}
	if decision.VEXDocumentID == "" {
		t.Fatalf("VEXDocumentID = empty, want VEX provenance preserved")
	}
}

func TestEvaluateSupplyChainSuppressionAppliesAcceptedRisk(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0010",
		PackageID:    "pkg:npm/example",
		RepositoryID: "repo://acme/api",
	}
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-accepted",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationAcceptedRisk,
		Author:        "eshu:policy/operator@acme.com",
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Reason:        "compensating control deployed at gateway",
		Scope: vulnerabilitySuppressionScope{
			CVEID:        "CVE-2026-0010",
			RepositoryID: "repo://acme/api",
		},
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateAcceptedRisk {
		t.Fatalf("State = %q, want %q", decision.State, SupplyChainSuppressionStateAcceptedRisk)
	}
}

func TestEvaluateSupplyChainSuppressionAppliesFalsePositive(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:     "CVE-2026-0020",
		PackageID: "pkg:pypi/example",
	}
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-fp",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationFalsePositive,
		Author:        "eshu:policy/operator@acme.com",
		AuthoredAt:    time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:     "CVE-2026-0020",
			PackageID: "pkg:pypi/example",
		},
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateFalsePositive {
		t.Fatalf("State = %q, want %q", decision.State, SupplyChainSuppressionStateFalsePositive)
	}
}

func TestEvaluateSupplyChainSuppressionExpiredKeepsFindingVisible(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0030",
		PackageID:    "pkg:npm/example",
		RepositoryID: "repo://acme/api",
	}
	suppression := vulnerabilitySuppression{
		SuppressionID:    "suppression-expired",
		Source:           facts.VulnerabilitySuppressionSourcePolicy,
		Justification:    facts.VulnerabilitySuppressionJustificationIgnored,
		AuthoredAt:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:        time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
		ExpiresAtRaw:     "2026-05-14T00:00:00Z",
		ExpiresAtPresent: true,
		Scope: vulnerabilitySuppressionScope{
			CVEID:        "CVE-2026-0030",
			PackageID:    "pkg:npm/example",
			RepositoryID: "repo://acme/api",
		},
	}

	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, now)
	if decision.State != SupplyChainSuppressionStateExpired {
		t.Fatalf("State = %q, want %q for expired suppression", decision.State, SupplyChainSuppressionStateExpired)
	}
	if decision.SuppressionID != "suppression-expired" {
		t.Fatalf("SuppressionID = %q, want suppression-expired (must remain attached for audit)", decision.SuppressionID)
	}
	if decision.ExpiresAt.IsZero() {
		t.Fatalf("ExpiresAt = zero, want expiration timestamp preserved")
	}
}

func TestEvaluateSupplyChainSuppressionProviderDismissedKeepsFindingVisible(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0040",
		PackageID:    "pkg:npm/example",
		RepositoryID: "repo://acme/api",
	}
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-provider",
		Source:        facts.VulnerabilitySuppressionSourceProviderDismissal,
		Justification: facts.VulnerabilitySuppressionJustificationProviderDismissed,
		Author:        "github_dependabot:operator@acme.com",
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:        "CVE-2026-0040",
			RepositoryID: "repo://acme/api",
		},
		EvidenceRef: "security_alert.repository_alert:github-dependabot:42",
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateProviderDismissed {
		t.Fatalf("State = %q, want %q for provider dismissal evidence", decision.State, SupplyChainSuppressionStateProviderDismissed)
	}
	if decision.SuppressionID == "" || decision.EvidenceRef == "" {
		t.Fatalf("decision must preserve provider evidence link: %#v", decision)
	}
}

func TestEvaluateSupplyChainSuppressionScopeMismatchKeepsFindingVisible(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0050",
		PackageID:    "pkg:npm/example",
		RepositoryID: "repo://acme/api",
	}
	mismatch := vulnerabilitySuppression{
		SuppressionID: "suppression-mismatch",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:        "CVE-2026-0050",
			RepositoryID: "repo://acme/worker", // different repository
		},
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{mismatch}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("State = %q, want %q when suppression scope does not match", decision.State, SupplyChainSuppressionStateScopeMismatch)
	}
	if decision.SuppressionID != "suppression-mismatch" {
		t.Fatalf("SuppressionID = %q, want the mismatched suppression preserved for audit", decision.SuppressionID)
	}
	if decision.Reason == "" {
		t.Fatalf("Reason = empty, want scope-mismatch explanation")
	}
}

func TestEvaluateSupplyChainSuppressionEvidencePathMismatchYieldsScopeMismatch(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0060",
		PackageID:    "pkg:npm/example",
		RepositoryID: "repo://acme/api",
		EvidencePath: []string{"vulnerability.cve", "vulnerability.affected_package"},
	}
	// suppression demands an evidence path step (sbom.component) that
	// is not in the finding's evidence path.
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-evidence",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:        "CVE-2026-0060",
			PackageID:    "pkg:npm/example",
			RepositoryID: "repo://acme/api",
			EvidencePath: []string{"sbom.component"},
		},
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("State = %q, want %q when evidence_path is not satisfied", decision.State, SupplyChainSuppressionStateScopeMismatch)
	}
}

func TestEvaluateSupplyChainSuppressionPrefersActiveOperatorOverExpired(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0070",
		PackageID:    "pkg:npm/example",
		RepositoryID: "repo://acme/api",
	}
	expired := vulnerabilitySuppression{
		SuppressionID:    "suppression-expired",
		Source:           facts.VulnerabilitySuppressionSourcePolicy,
		Justification:    facts.VulnerabilitySuppressionJustificationIgnored,
		AuthoredAt:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:        time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		ExpiresAtRaw:     "2026-05-10T00:00:00Z",
		ExpiresAtPresent: true,
		Scope: vulnerabilitySuppressionScope{
			CVEID:        "CVE-2026-0070",
			PackageID:    "pkg:npm/example",
			RepositoryID: "repo://acme/api",
		},
	}
	active := vulnerabilitySuppression{
		SuppressionID: "suppression-active",
		Source:        facts.VulnerabilitySuppressionSourceVEX,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:        "CVE-2026-0070",
			PackageID:    "pkg:npm/example",
			RepositoryID: "repo://acme/api",
		},
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{expired, active}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("State = %q, want %q (active operator suppression must win over expired)", decision.State, SupplyChainSuppressionStateNotAffected)
	}
	if decision.SuppressionID != "suppression-active" {
		t.Fatalf("SuppressionID = %q, want suppression-active", decision.SuppressionID)
	}
}

func TestEvaluateSupplyChainSuppressionEmptyScopeNeverHidesFindings(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0080",
		PackageID:    "pkg:npm/example",
		RepositoryID: "repo://acme/api",
	}
	emptyScope := vulnerabilitySuppression{
		SuppressionID: "suppression-empty-scope",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		// Scope deliberately empty: a producer omitted scope entirely or
		// shipped a malformed fact. The reducer MUST NOT silently apply
		// this as a wildcard suppression.
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{emptyScope}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("State = %q, want %q so empty-scope suppression cannot accidentally hide every finding", decision.State, SupplyChainSuppressionStateScopeMismatch)
	}
	if decision.SuppressionID != "suppression-empty-scope" {
		t.Fatalf("SuppressionID = %q, want suppression-empty-scope preserved for audit", decision.SuppressionID)
	}
	if decision.Reason == "" {
		t.Fatalf("Reason = empty, want explicit empty-scope explanation")
	}
}

func TestEvaluateSupplyChainSuppressionInvalidExpiresAtNeverExtendsSuppression(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0090",
		PackageID:    "pkg:npm/example",
		RepositoryID: "repo://acme/api",
	}
	// Suppression that would otherwise apply, but ships an unparseable
	// expires_at value. The reducer MUST NOT treat the bad timestamp as
	// "no expiration" and let the suppression apply indefinitely.
	suppression := vulnerabilitySuppression{
		SuppressionID:        "suppression-invalid-expiry",
		Source:               facts.VulnerabilitySuppressionSourcePolicy,
		Justification:        facts.VulnerabilitySuppressionJustificationIgnored,
		AuthoredAt:           time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		ExpiresAtRaw:         "2026-13-40T99:99:99Z",
		ExpiresAtPresent:     true,
		ExpiresAtParseFailed: true,
		Scope: vulnerabilitySuppressionScope{
			CVEID:        "CVE-2026-0090",
			PackageID:    "pkg:npm/example",
			RepositoryID: "repo://acme/api",
		},
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateExpired {
		t.Fatalf("State = %q, want %q for invalid expires_at", decision.State, SupplyChainSuppressionStateExpired)
	}
	if decision.SuppressionID != "suppression-invalid-expiry" {
		t.Fatalf("SuppressionID = %q, want suppression-invalid-expiry preserved", decision.SuppressionID)
	}
	if !strings.Contains(decision.Reason, "invalid") {
		t.Fatalf("Reason = %q, want mention of invalid expires_at", decision.Reason)
	}
}

func TestEvaluateSupplyChainSuppressionScopeMismatchReasonIncludesAllAnchors(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0100",
		AdvisoryID:   "GHSA-aaaa-bbbb-cccc",
		PackageID:    "pkg:npm/example",
		PURL:         "pkg:npm/example@1.2.3",
		RepositoryID: "repo://acme/api",
	}
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-mismatch-all",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:      "CVE-2026-0100",
			AdvisoryID: "GHSA-zzzz-yyyy-xxxx",   // mismatch
			PURL:       "pkg:npm/example@9.9.9", // mismatch
		},
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("State = %q, want %q", decision.State, SupplyChainSuppressionStateScopeMismatch)
	}
	if !strings.Contains(decision.Reason, "advisory_id") {
		t.Fatalf("Reason = %q, want advisory_id diff for auditability", decision.Reason)
	}
	if !strings.Contains(decision.Reason, "purl") {
		t.Fatalf("Reason = %q, want purl diff for auditability", decision.Reason)
	}
}

func TestBuildVulnerabilitySuppressionsFromEnvelopesNormalizesPayload(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		vulnerabilitySuppressionFactEnvelope(
			"vuln-suppression:1",
			facts.VulnerabilitySuppressionSourceVEX,
			facts.VulnerabilitySuppressionJustificationNotAffected,
			"vex:openvex/operator@example.com",
			"2026-05-10T00:00:00Z",
			"",
			map[string]any{
				"cve_id":        "CVE-2026-0001",
				"package_id":    "pkg:npm/example",
				"repository_id": "repo://acme/api",
				"evidence_path": []any{"vulnerability.cve", "vulnerability.affected_package"},
			},
		),
	}

	suppressions := BuildVulnerabilitySuppressions(envelopes)
	if got, want := len(suppressions), 1; got != want {
		t.Fatalf("BuildVulnerabilitySuppressions() len = %d, want %d", got, want)
	}
	s := suppressions[0]
	if s.SuppressionID != "vuln-suppression:1" {
		t.Fatalf("SuppressionID = %q, want vuln-suppression:1", s.SuppressionID)
	}
	if s.Source != facts.VulnerabilitySuppressionSourceVEX {
		t.Fatalf("Source = %q, want %q", s.Source, facts.VulnerabilitySuppressionSourceVEX)
	}
	if s.Justification != facts.VulnerabilitySuppressionJustificationNotAffected {
		t.Fatalf("Justification = %q, want %q", s.Justification, facts.VulnerabilitySuppressionJustificationNotAffected)
	}
	if s.Scope.CVEID != "CVE-2026-0001" || s.Scope.PackageID != "pkg:npm/example" || s.Scope.RepositoryID != "repo://acme/api" {
		t.Fatalf("Scope = %#v, want all anchors preserved", s.Scope)
	}
	if len(s.Scope.EvidencePath) != 2 {
		t.Fatalf("Scope.EvidencePath = %#v, want two steps", s.Scope.EvidencePath)
	}
	if s.AuthoredAt.IsZero() {
		t.Fatalf("AuthoredAt = zero, want parsed RFC3339 timestamp")
	}
}

func vulnerabilitySuppressionFactEnvelope(
	id string,
	source string,
	justification string,
	author string,
	authoredAt string,
	expiresAt string,
	scope map[string]any,
) facts.Envelope {
	payload := map[string]any{
		"suppression_id": id,
		"source":         source,
		"justification":  justification,
		"author":         author,
		"authored_at":    authoredAt,
		"scope":          scope,
	}
	if expiresAt != "" {
		payload["expires_at"] = expiresAt
	}
	return facts.Envelope{
		FactID:   id,
		FactKind: facts.VulnerabilitySuppressionFactKind,
		Payload:  payload,
	}
}
