// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/environment"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

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
			name: "workload_id aggregate stays visible",
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
			wantState: SupplyChainSuppressionStateScopeMismatch,
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
	scope := suppressions[0].Scope
	if scope.Environment != "" || scope.WorkloadID != "" || scope.ServiceID != "" {
		t.Fatalf("Scope = %#v, want environment/workload_id/service_id empty when omitted from the payload", scope)
	}
}
