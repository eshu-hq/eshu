// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"
)

func TestSecurityAlertReconciliationFactIdentitySurvivesProviderOnlyToMatched(t *testing.T) {
	t.Parallel()

	write := SecurityAlertReconciliationWrite{
		ScopeID:      "package:npm/left-pad",
		GenerationID: "package-generation-1",
		IntentID:     "intent-1",
		SourceSystem: "security_alert",
		Cause:        "provider security alert evidence observed",
	}
	providerOnly := SecurityAlertReconciliationDecision{
		Provider:                  "github_dependabot",
		ProviderAlertID:           "github_dependabot:security-alert:github:acme/api:42",
		ProviderAlertNumber:       42,
		ProviderAlertFactID:       "security-alert-fact-42",
		ProviderAlertScopeID:      "security-alert:github:acme/api",
		ProviderAlertGenerationID: "security-alert-generation-1",
		ProviderState:             "open",
		RepositoryID:              "security-alert:github:acme/api",
		ProviderRepositoryID:      "security-alert:github:acme/api",
		PackageID:                 "npm://registry.npmjs.org/left-pad",
		GHSAIDs:                   []string{"GHSA-abcd-1234"},
		CVEIDs:                    []string{"CVE-2026-0001"},
		Status:                    SecurityAlertReconciliationProviderOnly,
		Reason:                    "provider alert has no matching owned dependency evidence",
		EvidenceFactIDs:           []string{"security-alert-fact-42"},
	}
	matched := providerOnly
	matched.RepositoryID = "repository:r_api"
	matched.Status = SecurityAlertReconciliationMatched
	matched.EshuImpactStatus = "affected_exact"
	matched.EshuImpactFindingID = "impact-1"
	matched.DependencyEvidenceID = "consume-1"
	matched.ImpactEvidenceID = "impact-1"
	matched.Reason = "provider alert matches owned dependency and reducer impact evidence"
	matched.EvidenceFactIDs = []string{"security-alert-fact-42", "consume-1", "impact-1"}

	if got, want := securityAlertReconciliationFactID(write, matched), securityAlertReconciliationFactID(write, providerOnly); got != want {
		t.Fatalf("matched fact id = %q, want provider-only replacement id %q", got, want)
	}
	if got, want := securityAlertReconciliationStableFactKey(write, matched), securityAlertReconciliationStableFactKey(write, providerOnly); got != want {
		t.Fatalf("matched stable_fact_key = %q, want provider-only replacement key %q", got, want)
	}

	payload := securityAlertReconciliationPayload(write, matched)
	if got, want := payload["reconciliation_status"], string(SecurityAlertReconciliationMatched); got != want {
		t.Fatalf("payload reconciliation_status = %q, want %q", got, want)
	}
	if got, want := payload["reason"], "provider alert matches owned dependency and reducer impact evidence"; got != want {
		t.Fatalf("payload reason = %q, want %q", got, want)
	}
	wantEvidence := []string{"consume-1", "impact-1", "security-alert-fact-42"}
	if got := payload["evidence_fact_ids"]; !reflect.DeepEqual(got, wantEvidence) {
		t.Fatalf("payload evidence_fact_ids = %#v, want %#v", got, wantEvidence)
	}
}

func TestSecurityAlertReconciliationFactIdentitySurvivesMatchedToStale(t *testing.T) {
	t.Parallel()

	write := SecurityAlertReconciliationWrite{
		ScopeID:      "security-alert:github:acme/api",
		GenerationID: "security-alert-generation-1",
		IntentID:     "intent-2",
		SourceSystem: "security_alert",
		Cause:        "provider security alert evidence observed",
	}
	matched := SecurityAlertReconciliationDecision{
		Provider:                  "github_dependabot",
		ProviderAlertID:           "github_dependabot:security-alert:github:acme/api:43",
		ProviderAlertNumber:       43,
		ProviderAlertFactID:       "security-alert-fact-43",
		ProviderAlertScopeID:      "security-alert:github:acme/api",
		ProviderAlertGenerationID: "security-alert-generation-1",
		ProviderState:             "open",
		RepositoryID:              "repository:r_api",
		ProviderRepositoryID:      "security-alert:github:acme/api",
		PackageID:                 "npm://registry.npmjs.org/left-pad",
		GHSAIDs:                   []string{"GHSA-abcd-1234"},
		CVEIDs:                    []string{"CVE-2026-0001"},
		Status:                    SecurityAlertReconciliationMatched,
		EshuImpactStatus:          "affected_exact",
		EshuImpactFindingID:       "impact-43",
		DependencyEvidenceID:      "consume-43",
		ImpactEvidenceID:          "impact-43",
		Reason:                    "provider alert matches owned dependency and reducer impact evidence",
		EvidenceFactIDs:           []string{"security-alert-fact-43", "consume-43", "impact-43"},
	}
	stale := matched
	stale.ProviderAlertFactID = "security-alert-fact-43-refresh"
	stale.Status = SecurityAlertReconciliationStale
	stale.EshuImpactStatus = ""
	stale.EshuImpactFindingID = ""
	stale.DependencyEvidenceID = "consume-43-newer"
	stale.ImpactEvidenceID = ""
	stale.Reason = "newer owned dependency evidence no longer matches the provider alert manifest path"
	stale.EvidenceFactIDs = []string{"security-alert-fact-43-refresh", "consume-43-newer"}

	if got, want := securityAlertReconciliationFactID(write, stale), securityAlertReconciliationFactID(write, matched); got != want {
		t.Fatalf("stale fact id = %q, want matched replacement id %q", got, want)
	}

	payload := securityAlertReconciliationPayload(write, stale)
	if got, want := payload["reconciliation_status"], string(SecurityAlertReconciliationStale); got != want {
		t.Fatalf("payload reconciliation_status = %q, want %q", got, want)
	}
	if got, want := payload["reason"], "newer owned dependency evidence no longer matches the provider alert manifest path"; got != want {
		t.Fatalf("payload reason = %q, want %q", got, want)
	}
	wantEvidence := []string{"consume-43-newer", "security-alert-fact-43-refresh"}
	if got := payload["evidence_fact_ids"]; !reflect.DeepEqual(got, wantEvidence) {
		t.Fatalf("payload evidence_fact_ids = %#v, want %#v", got, wantEvidence)
	}
}
