// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/environment"
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

// TestEvaluateSupplyChainSuppressionEnvironmentScopeHidesMatchingEnvironment
// proves the headline #5466 capability: a suppression scoped to one
// environment hides a finding whose evidence names that environment.
func TestEvaluateSupplyChainSuppressionEnvironmentScopeHidesMatchingEnvironment(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0200",
		PackageID:     "pkg:npm/example",
		RepositoryID:  "repo://acme/api",
		SubjectDigest: "sha256:same-digest-both-envs",
		Environments:  []string{"stage"},
	}
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-env-stage",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Reason:        "not exploitable in staging",
		Scope: vulnerabilitySuppressionScope{
			CVEID:         "CVE-2026-0200",
			SubjectDigest: "sha256:same-digest-both-envs",
			Environment:   "stage",
		},
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("State = %q, want %q for finding whose environment matches the suppression scope", decision.State, SupplyChainSuppressionStateNotAffected)
	}
}

// TestEvaluateSupplyChainSuppressionEnvironmentScopeDoesNotHideOtherEnvironment
// proves the matcher narrows: the same suppression must NOT hide a finding
// with a different environment.
func TestEvaluateSupplyChainSuppressionEnvironmentScopeDoesNotHideOtherEnvironment(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0200",
		PackageID:     "pkg:npm/example",
		RepositoryID:  "repo://acme/api",
		SubjectDigest: "sha256:same-digest-both-envs",
		Environments:  []string{"prod"},
	}
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-env-stage",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:         "CVE-2026-0200",
			SubjectDigest: "sha256:same-digest-both-envs",
			Environment:   "stage",
		},
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("State = %q, want %q for finding in a different environment than the suppression scope", decision.State, SupplyChainSuppressionStateScopeMismatch)
	}
	if decision.SuppressionID != "suppression-env-stage" {
		t.Fatalf("SuppressionID = %q, want the mismatched suppression preserved for audit", decision.SuppressionID)
	}
}

// TestEvaluateSupplyChainSuppressionSameDigestTwoEnvironmentsHidesOnlyScopedEnvironment
// is the binding acceptance fixture: the same digest deployed to two
// environments, one suppression scoped to one environment, exactly one
// finding hidden and the other visible.
func TestEvaluateSupplyChainSuppressionSameDigestTwoEnvironmentsHidesOnlyScopedEnvironment(t *testing.T) {
	t.Parallel()

	const digest = "sha256:headline-fixture-digest"
	stagingFinding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0210",
		PackageID:     "pkg:npm/example",
		RepositoryID:  "repo://acme/api",
		SubjectDigest: digest,
		Environments:  []string{"stage"},
	}
	prodFinding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0210",
		PackageID:     "pkg:npm/example",
		RepositoryID:  "repo://acme/api",
		SubjectDigest: digest,
		Environments:  []string{"prod"},
	}
	suppressions := []vulnerabilitySuppression{{
		SuppressionID: "suppression-staging-only",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Reason:        "not exploitable in staging, still visible in prod",
		Scope: vulnerabilitySuppressionScope{
			CVEID:         "CVE-2026-0210",
			SubjectDigest: digest,
			Environment:   "stage",
		},
	}}
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	stagingDecision := EvaluateSupplyChainSuppression(stagingFinding, suppressions, now)
	prodDecision := EvaluateSupplyChainSuppression(prodFinding, suppressions, now)

	if stagingDecision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("staging State = %q, want %q (exactly one environment hidden)", stagingDecision.State, SupplyChainSuppressionStateNotAffected)
	}
	if !SupplyChainSuppressionStateIsHidden(stagingDecision.State) {
		t.Fatalf("staging decision state %q must be hidden from the default view", stagingDecision.State)
	}
	if prodDecision.State == SupplyChainSuppressionStateNotAffected || SupplyChainSuppressionStateIsHidden(prodDecision.State) {
		t.Fatalf("prod State = %q, want the same-digest finding in the other environment to stay visible", prodDecision.State)
	}
}

// TestEvaluateSupplyChainSuppressionEnvironmentScopeCanonicalizesAliases proves
// the matcher consumes the shared environment-alias contract
// (go/internal/environment.Canonical) rather than a local normalization: an
// alias form in the suppression scope ("production") must match a finding
// whose evidence already resolved to the canonical form ("prod").
func TestEvaluateSupplyChainSuppressionEnvironmentScopeCanonicalizesAliases(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0220",
		RepositoryID:  "repo://acme/api",
		SubjectDigest: "sha256:alias-digest",
		Environments:  []string{"prod"},
	}
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-alias",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationAcceptedRisk,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:         "CVE-2026-0220",
			SubjectDigest: "sha256:alias-digest",
			// Decode canonicalizes "production" -> "prod" through
			// environment.Canonical before it ever reaches the matcher, so
			// build the scope the way the decode seam would.
			Environment: environment.Canonical("production"),
		},
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateAcceptedRisk {
		t.Fatalf("State = %q, want %q when the alias canonicalizes to the finding's environment", decision.State, SupplyChainSuppressionStateAcceptedRisk)
	}
}

// TestEvaluateSupplyChainSuppressionEnvironmentScopeFailsClosedWithNoEvidence
// proves the fail-closed rule: a finding with NO environment evidence at all
// must not be hidden by an environment-scoped suppression. Ambiguity resolves
// to "still visible" because a suppression hides a vulnerability.
func TestEvaluateSupplyChainSuppressionEnvironmentScopeFailsClosedWithNoEvidence(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0230",
		RepositoryID:  "repo://acme/api",
		SubjectDigest: "sha256:no-environment-evidence",
		// Environments deliberately empty: no deployment evidence resolved
		// an environment for this finding.
	}
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-env-no-evidence",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:         "CVE-2026-0230",
			SubjectDigest: "sha256:no-environment-evidence",
			Environment:   "stage",
		},
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("State = %q, want %q: a finding with no environment evidence must never be hidden by an environment-scoped suppression", decision.State, SupplyChainSuppressionStateScopeMismatch)
	}
	if SupplyChainSuppressionStateIsHidden(decision.State) {
		t.Fatalf("decision state %q must not be hidden when the finding has no environment evidence", decision.State)
	}
}

// TestEvaluateSupplyChainSuppressionWorkloadAndServiceScopeMatchAndFailClosed
// covers workload_id and service_id scoping, each independently, including
// the fail-closed no-evidence case.
func TestEvaluateSupplyChainSuppressionWorkloadAndServiceScopeMatchAndFailClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		findingFields SupplyChainImpactFinding
		scope         vulnerabilitySuppressionScope
		wantState     SupplyChainSuppressionState
	}{
		{
			name: "workload_id matches",
			findingFields: SupplyChainImpactFinding{
				CVEID:         "CVE-2026-0300",
				SubjectDigest: "sha256:workload-digest",
				WorkloadIDs:   []string{"workload-a", "workload-b"},
			},
			scope: vulnerabilitySuppressionScope{
				CVEID:         "CVE-2026-0300",
				SubjectDigest: "sha256:workload-digest",
				WorkloadID:    "workload-a",
			},
			wantState: SupplyChainSuppressionStateNotAffected,
		},
		{
			name: "workload_id mismatch",
			findingFields: SupplyChainImpactFinding{
				CVEID:         "CVE-2026-0300",
				SubjectDigest: "sha256:workload-digest",
				WorkloadIDs:   []string{"workload-b"},
			},
			scope: vulnerabilitySuppressionScope{
				CVEID:         "CVE-2026-0300",
				SubjectDigest: "sha256:workload-digest",
				WorkloadID:    "workload-a",
			},
			wantState: SupplyChainSuppressionStateScopeMismatch,
		},
		{
			name: "workload_id fails closed with no evidence",
			findingFields: SupplyChainImpactFinding{
				CVEID:         "CVE-2026-0300",
				SubjectDigest: "sha256:workload-digest",
			},
			scope: vulnerabilitySuppressionScope{
				CVEID:         "CVE-2026-0300",
				SubjectDigest: "sha256:workload-digest",
				WorkloadID:    "workload-a",
			},
			wantState: SupplyChainSuppressionStateScopeMismatch,
		},
		{
			name: "service_id matches",
			findingFields: SupplyChainImpactFinding{
				CVEID:         "CVE-2026-0301",
				SubjectDigest: "sha256:service-digest",
				ServiceIDs:    []string{"service-a"},
			},
			scope: vulnerabilitySuppressionScope{
				CVEID:         "CVE-2026-0301",
				SubjectDigest: "sha256:service-digest",
				ServiceID:     "service-a",
			},
			wantState: SupplyChainSuppressionStateNotAffected,
		},
		{
			name: "service_id mismatch",
			findingFields: SupplyChainImpactFinding{
				CVEID:         "CVE-2026-0301",
				SubjectDigest: "sha256:service-digest",
				ServiceIDs:    []string{"service-b"},
			},
			scope: vulnerabilitySuppressionScope{
				CVEID:         "CVE-2026-0301",
				SubjectDigest: "sha256:service-digest",
				ServiceID:     "service-a",
			},
			wantState: SupplyChainSuppressionStateScopeMismatch,
		},
		{
			name: "service_id fails closed with no evidence",
			findingFields: SupplyChainImpactFinding{
				CVEID:         "CVE-2026-0301",
				SubjectDigest: "sha256:service-digest",
			},
			scope: vulnerabilitySuppressionScope{
				CVEID:         "CVE-2026-0301",
				SubjectDigest: "sha256:service-digest",
				ServiceID:     "service-a",
			},
			wantState: SupplyChainSuppressionStateScopeMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			suppression := vulnerabilitySuppression{
				SuppressionID: "suppression-" + tt.name,
				Source:        facts.VulnerabilitySuppressionSourcePolicy,
				Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
				AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
				Scope:         tt.scope,
			}
			decision := EvaluateSupplyChainSuppression(tt.findingFields, []vulnerabilitySuppression{suppression}, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
			if decision.State != tt.wantState {
				t.Fatalf("State = %q, want %q", decision.State, tt.wantState)
			}
		})
	}
}

// TestEvaluateSupplyChainSuppressionEnvironmentAndWorkloadCombinationRequiresBoth
// proves scope keys combine with AND semantics: setting both environment and
// workload_id requires the finding to match both, not either.
func TestEvaluateSupplyChainSuppressionEnvironmentAndWorkloadCombinationRequiresBoth(t *testing.T) {
	t.Parallel()

	scope := vulnerabilitySuppressionScope{
		CVEID:         "CVE-2026-0400",
		SubjectDigest: "sha256:combo-digest",
		Environment:   "stage",
		WorkloadID:    "workload-a",
	}
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-combo",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope:         scope,
	}
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	both := SupplyChainImpactFinding{
		CVEID: "CVE-2026-0400", SubjectDigest: "sha256:combo-digest",
		Environments: []string{"stage"}, WorkloadIDs: []string{"workload-a"},
	}
	if got := EvaluateSupplyChainSuppression(both, []vulnerabilitySuppression{suppression}, now); got.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("both match: State = %q, want %q", got.State, SupplyChainSuppressionStateNotAffected)
	}

	environmentOnly := SupplyChainImpactFinding{
		CVEID: "CVE-2026-0400", SubjectDigest: "sha256:combo-digest",
		Environments: []string{"stage"}, WorkloadIDs: []string{"workload-b"},
	}
	if got := EvaluateSupplyChainSuppression(environmentOnly, []vulnerabilitySuppression{suppression}, now); got.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("environment matches but workload does not: State = %q, want %q", got.State, SupplyChainSuppressionStateScopeMismatch)
	}

	workloadOnly := SupplyChainImpactFinding{
		CVEID: "CVE-2026-0400", SubjectDigest: "sha256:combo-digest",
		Environments: []string{"prod"}, WorkloadIDs: []string{"workload-a"},
	}
	if got := EvaluateSupplyChainSuppression(workloadOnly, []vulnerabilitySuppression{suppression}, now); got.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("workload matches but environment does not: State = %q, want %q", got.State, SupplyChainSuppressionStateScopeMismatch)
	}
}

// TestEvaluateSupplyChainSuppressionNoNewScopeFieldsBehavesAsDigestEverywhere
// is the highest-risk regression: an existing suppression that never sets
// environment/workload_id/service_id MUST behave exactly as it did before
// #5466, regardless of what environment/workload/service evidence the
// finding carries.
func TestEvaluateSupplyChainSuppressionNoNewScopeFieldsBehavesAsDigestEverywhere(t *testing.T) {
	t.Parallel()

	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-digest-everywhere",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:         "CVE-2026-0500",
			SubjectDigest: "sha256:digest-everywhere",
			// Environment, WorkloadID, ServiceID deliberately unset.
		},
	}
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	for _, finding := range []SupplyChainImpactFinding{
		{CVEID: "CVE-2026-0500", SubjectDigest: "sha256:digest-everywhere"},
		{CVEID: "CVE-2026-0500", SubjectDigest: "sha256:digest-everywhere", Environments: []string{"stage"}},
		{CVEID: "CVE-2026-0500", SubjectDigest: "sha256:digest-everywhere", Environments: []string{"prod"}},
		{CVEID: "CVE-2026-0500", SubjectDigest: "sha256:digest-everywhere", WorkloadIDs: []string{"workload-x"}},
		{CVEID: "CVE-2026-0500", SubjectDigest: "sha256:digest-everywhere", ServiceIDs: []string{"service-x"}},
	} {
		decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, now)
		if decision.State != SupplyChainSuppressionStateNotAffected {
			t.Fatalf("finding %#v: State = %q, want %q (digest-everywhere suppression must hide the finding regardless of environment/workload/service evidence)", finding, decision.State, SupplyChainSuppressionStateNotAffected)
		}
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

	suppressions, quarantined, err := BuildVulnerabilitySuppressions(envelopes)
	if err != nil {
		t.Fatalf("BuildVulnerabilitySuppressions() error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("BuildVulnerabilitySuppressions() quarantined = %#v, want none", quarantined)
	}
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

// TestBuildVulnerabilitySuppressionsDecodesEnvironmentWorkloadServiceScope
// proves the decode seam reads the three new optional scope keys and
// canonicalizes environment through the shared environment-alias contract
// (go/internal/environment.Canonical) rather than a local normalization.
func TestBuildVulnerabilitySuppressionsDecodesEnvironmentWorkloadServiceScope(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		vulnerabilitySuppressionFactEnvelope(
			"vuln-suppression:2",
			facts.VulnerabilitySuppressionSourcePolicy,
			facts.VulnerabilitySuppressionJustificationNotAffected,
			"eshu:policy/operator@example.com",
			"2026-05-10T00:00:00Z",
			"",
			map[string]any{
				"cve_id":         "CVE-2026-0600",
				"subject_digest": "sha256:decode-scope",
				// "production" is an alias form; the decode seam must
				// canonicalize it through environment.Canonical, matching
				// the shared alias contract used elsewhere in the reducer.
				"environment": "production",
				"workload_id": "workload-decode-1",
				"service_id":  "service-decode-1",
			},
		),
	}

	suppressions := BuildVulnerabilitySuppressions(envelopes)
	if got, want := len(suppressions), 1; got != want {
		t.Fatalf("BuildVulnerabilitySuppressions() len = %d, want %d", got, want)
	}
	scope := suppressions[0].Scope
	if want := environment.Canonical("production"); scope.Environment != want {
		t.Fatalf("Scope.Environment = %q, want %q (canonicalized via environment.Canonical)", scope.Environment, want)
	}
	if scope.Environment != "prod" {
		t.Fatalf("Scope.Environment = %q, want the canonical form \"prod\"", scope.Environment)
	}
	if scope.WorkloadID != "workload-decode-1" {
		t.Fatalf("Scope.WorkloadID = %q, want workload-decode-1", scope.WorkloadID)
	}
	if scope.ServiceID != "service-decode-1" {
		t.Fatalf("Scope.ServiceID = %q, want service-decode-1", scope.ServiceID)
	}
}

// TestBuildVulnerabilitySuppressionsOmittedEnvironmentWorkloadServiceScope
// proves the additive-optional contract: a payload that never sets the new
// scope keys decodes to empty fields, not zero-value surprises.
func TestBuildVulnerabilitySuppressionsOmittedEnvironmentWorkloadServiceScope(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		vulnerabilitySuppressionFactEnvelope(
			"vuln-suppression:3",
			facts.VulnerabilitySuppressionSourceVEX,
			facts.VulnerabilitySuppressionJustificationNotAffected,
			"vex:openvex/operator@example.com",
			"2026-05-10T00:00:00Z",
			"",
			map[string]any{
				"cve_id":        "CVE-2026-0700",
				"repository_id": "repo://acme/api",
			},
		),
	}

	suppressions := BuildVulnerabilitySuppressions(envelopes)
	if got, want := len(suppressions), 1; got != want {
		t.Fatalf("BuildVulnerabilitySuppressions() len = %d, want %d", got, want)
	}
	scope := suppressions[0].Scope
	if scope.Environment != "" || scope.WorkloadID != "" || scope.ServiceID != "" {
		t.Fatalf("Scope = %#v, want environment/workload_id/service_id empty when omitted from the payload", scope)
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
		FactID:        id,
		FactKind:      facts.VulnerabilitySuppressionFactKind,
		SchemaVersion: facts.VulnerabilitySuppressionSchemaVersionV1,
		Payload:       payload,
	}
}
