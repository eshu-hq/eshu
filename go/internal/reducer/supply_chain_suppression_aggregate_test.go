// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestEvaluateSupplyChainSuppressionEnvironmentScopeKeepsMultiEnvironmentAggregateVisible(t *testing.T) {
	t.Parallel()

	const digest = "sha256:multi-environment-aggregate"
	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-5466",
		PackageID:     "pkg:npm/example",
		RepositoryID:  "repo://example/api",
		SubjectDigest: digest,
	}
	finalizeSupplyChainImpactFinding(&finding, supplyChainImpactIndex{
		deployments: []supplyChainDeploymentContext{
			{
				factID:              "deployment:stage",
				artifactDigest:      digest,
				repositoryID:        finding.RepositoryID,
				environment:         "stage",
				environmentEvidence: supplyChainEnvironmentEvidenceDeployEvent,
				outcome:             string(CICDRunCorrelationExact),
			},
			{
				factID:              "deployment:prod",
				artifactDigest:      digest,
				repositoryID:        finding.RepositoryID,
				environment:         "prod",
				environmentEvidence: supplyChainEnvironmentEvidenceDeployEvent,
				outcome:             string(CICDRunCorrelationExact),
			},
		},
	})
	if got, want := len(finding.Environments), 2; got != want {
		t.Fatalf("len(Environments) = %d, want %d after production finalization", got, want)
	}

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{{
		SuppressionID: "suppression-stage-only",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:         finding.CVEID,
			SubjectDigest: digest,
			Environment:   "stage",
		},
	}}, time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))

	if SupplyChainSuppressionStateIsHidden(decision.State) {
		t.Fatalf("State = %q, want visible: one stage predicate cannot hide a prod+stage aggregate", decision.State)
	}
	if decision.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("State = %q, want %q for ambiguous aggregate", decision.State, SupplyChainSuppressionStateScopeMismatch)
	}
}

func TestEvaluateSupplyChainSuppressionAmbiguousEnvironmentReasonPreservesVerifiedPair(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-5466",
		SubjectDigest: "sha256:verified-pair-multi-environment",
		Environments:  []string{"prod", "stage"},
		WorkloadIDs:   []string{"workload-a"},
		ServiceIDs:    []string{"service-a"},
		ServiceWorkloadPairs: []SupplyChainServiceWorkloadPair{{
			ServiceID:  "service-a",
			WorkloadID: "workload-a",
		}},
	}
	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{{
		SuppressionID: "suppression-verified-pair",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:         finding.CVEID,
			SubjectDigest: finding.SubjectDigest,
			Environment:   "stage",
			WorkloadID:    "workload-a",
			ServiceID:     "service-a",
		},
	}}, time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))

	if !strings.Contains(decision.Reason, "environment") {
		t.Fatalf("Reason = %q, want environment ambiguity", decision.Reason)
	}
	if strings.Contains(decision.Reason, "no verified co-occurrence") {
		t.Fatalf("Reason = %q, must not reject the verified workload/service pair", decision.Reason)
	}
}

func TestEvaluateSupplyChainSuppressionDeploymentContextDoesNotCrossVulnerabilityIdentity(t *testing.T) {
	t.Parallel()

	findings := []SupplyChainImpactFinding{
		{CVEID: "CVE-2026-54661", Environments: []string{"prod"}},
		{CVEID: "CVE-2026-54662", Environments: []string{"prod"}},
	}
	suppressions := []vulnerabilitySuppression{{
		SuppressionID: "suppression-other-cve",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:       "CVE-2026-54661",
			Environment: "prod",
		},
	}}
	now := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	for i := range findings {
		findings[i].Suppression = EvaluateSupplyChainSuppression(findings[i], suppressions, now)
	}

	if findings[0].Suppression.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("CVE-A State = %q, want %q", findings[0].Suppression.State, SupplyChainSuppressionStateNotAffected)
	}
	if findings[1].Suppression.State != SupplyChainSuppressionStateActive {
		t.Fatalf("CVE-B State = %q, want %q for a different vulnerability identity", findings[1].Suppression.State, SupplyChainSuppressionStateActive)
	}
	if findings[1].Suppression.SuppressionID != "" {
		t.Fatalf("CVE-B SuppressionID = %q, want empty for a different vulnerability identity", findings[1].Suppression.SuppressionID)
	}
}

func TestEvaluateSupplyChainSuppressionSingleDeploymentDimensionRequiresSingletonEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		finding SupplyChainImpactFinding
		scope   vulnerabilitySuppressionScope
	}{
		{
			name:    "workload",
			finding: SupplyChainImpactFinding{WorkloadIDs: []string{"workload-a", "workload-b"}},
			scope:   vulnerabilitySuppressionScope{WorkloadID: "workload-a"},
		},
		{
			name:    "service",
			finding: SupplyChainImpactFinding{ServiceIDs: []string{"service-a", "service-b"}},
			scope:   vulnerabilitySuppressionScope{ServiceID: "service-a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finding := tt.finding
			finding.CVEID = "CVE-2026-5466"
			finding.SubjectDigest = "sha256:multi-context-aggregate"
			tt.scope.CVEID = finding.CVEID
			tt.scope.SubjectDigest = finding.SubjectDigest

			decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{{
				SuppressionID: "suppression-" + tt.name,
				Source:        facts.VulnerabilitySuppressionSourcePolicy,
				Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
				AuthoredAt:    time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
				Scope:         tt.scope,
			}}, time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))

			if SupplyChainSuppressionStateIsHidden(decision.State) {
				t.Fatalf("State = %q, want visible for multi-%s aggregate", decision.State, tt.name)
			}
		})
	}
}
