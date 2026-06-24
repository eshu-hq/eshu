// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSecurityAlertReconciliationsClassifiesProviderAlertStates(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/eshu-hq/eshu"
	packageID := "npm://registry.npmjs.org/left-pad"
	alert := securityAlertEnvelope("alert-matched", repoID, map[string]any{
		"provider":                  "github_dependabot",
		"provider_alert_number":     int64(42),
		"provider_state":            "open",
		"package_id":                packageID,
		"ecosystem":                 "npm",
		"package_name":              "left-pad",
		"manifest_path":             "package-lock.json",
		"relationship":              "direct",
		"dependency_scope":          "runtime",
		"ghsa_ids":                  []string{"GHSA-abcd-1234"},
		"cve_ids":                   []string{"CVE-2026-0001"},
		"updated_at":                "2026-05-23T10:15:00Z",
		"source_freshness":          "partial",
		"collection_coverage_state": "incomplete",
		"collection_truncated":      true,
		"collection_pages_fetched":  int64(2),
		"collection_state_filter":   "open",
		"collection_incomplete_reasons": []string{
			"provider_open_alert_page_limit_reached",
		},
	})
	consumption := packageConsumptionCorrelationEnvelope("consume-1", repoID, packageID, "package-lock.json")
	consumption.Payload["installed_version"] = "1.2.0"
	impact := supplyChainImpactFindingEnvelope("impact-1", repoID, packageID, "CVE-2026-0001", "affected_exact")

	decisions := BuildSecurityAlertReconciliations([]facts.Envelope{alert, consumption, impact})
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Status, SecurityAlertReconciliationMatched; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := decision.ProviderState, "open"; got != want {
		t.Fatalf("ProviderState = %q, want %q", got, want)
	}
	if got, want := decision.EshuImpactStatus, "affected_exact"; got != want {
		t.Fatalf("EshuImpactStatus = %q, want %q", got, want)
	}
	if got, want := decision.ObservedVersion, "1.2.0"; got != want {
		t.Fatalf("ObservedVersion = %q, want Eshu-owned observed version %q", got, want)
	}
	if got, want := decision.DependencyEvidenceKind, packageConsumptionCorrelationFactKind; got != want {
		t.Fatalf("DependencyEvidenceKind = %q, want %q", got, want)
	}
	if got, want := decision.CanonicalWrites, 0; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := decision.ProviderAlertFactID, "alert-matched"; got != want {
		t.Fatalf("ProviderAlertFactID = %q, want %q", got, want)
	}
	if got, want := decision.SourceFreshness, "partial"; got != want {
		t.Fatalf("SourceFreshness = %q, want %q", got, want)
	}
	if got, want := decision.CollectionCoverageState, "incomplete"; got != want {
		t.Fatalf("CollectionCoverageState = %q, want %q", got, want)
	}
	if !decision.CollectionTruncated {
		t.Fatal("CollectionTruncated = false, want true")
	}
	if len(decision.EvidenceFactIDs) != 3 {
		t.Fatalf("EvidenceFactIDs = %#v, want alert, consumption, and impact facts", decision.EvidenceFactIDs)
	}
}

func TestBuildSecurityAlertReconciliationsCoversUnmatchedProviderOnlyStaleDismissedAndFixed(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/eshu-hq/eshu"
	packageID := "npm://registry.npmjs.org/left-pad"
	staleObserved := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		securityAlertEnvelope("alert-unmatched", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(1),
			"provider_state":        "open",
			"package_id":            packageID,
			"manifest_path":         "package-lock.json",
			"cve_ids":               []string{"CVE-2026-0002"},
		}),
		securityAlertEnvelope("alert-provider-only", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(2),
			"provider_state":        "open",
			"package_id":            "npm://registry.npmjs.org/provider-only",
			"manifest_path":         "package-lock.json",
			"cve_ids":               []string{"CVE-2026-0003"},
		}),
		securityAlertEnvelope("alert-stale", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(3),
			"provider_state":        "open",
			"package_id":            packageID,
			"manifest_path":         "old-package-lock.json",
			"updated_at":            "2026-05-20T00:00:00Z",
		}),
		securityAlertEnvelope("alert-dismissed", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(4),
			"provider_state":        "dismissed",
			"package_id":            packageID,
			"manifest_path":         "package-lock.json",
		}),
		securityAlertEnvelope("alert-fixed", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(5),
			"provider_state":        "fixed",
			"package_id":            packageID,
			"manifest_path":         "package-lock.json",
		}),
		packageConsumptionCorrelationEnvelope("consume-1", repoID, packageID, "package-lock.json"),
	}
	envelopes[len(envelopes)-1].ObservedAt = staleObserved

	decisions := BuildSecurityAlertReconciliations(envelopes)
	got := map[string]SecurityAlertReconciliationStatus{}
	for _, decision := range decisions {
		got[decision.ProviderAlertFactID] = decision.Status
	}
	want := map[string]SecurityAlertReconciliationStatus{
		"alert-unmatched":     SecurityAlertReconciliationUnmatched,
		"alert-provider-only": SecurityAlertReconciliationProviderOnly,
		"alert-stale":         SecurityAlertReconciliationStale,
		"alert-dismissed":     SecurityAlertReconciliationDismissed,
		"alert-fixed":         SecurityAlertReconciliationFixed,
	}
	for factID, wantStatus := range want {
		if got[factID] != wantStatus {
			t.Fatalf("status for %s = %q, want %q (all: %#v)", factID, got[factID], wantStatus, got)
		}
	}
}

func TestBuildSecurityAlertReconciliationsSelectsNewestStaleConsumption(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/eshu-hq/eshu"
	packageID := "npm://registry.npmjs.org/left-pad"
	alert := securityAlertEnvelope("alert-stale", repoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(6),
		"provider_state":        "open",
		"package_id":            packageID,
		"manifest_path":         "old-package-lock.json",
		"updated_at":            "2026-05-20T00:00:00Z",
	})
	newerConsumption := packageConsumptionCorrelationEnvelope(
		"consume-newer-stale",
		repoID,
		packageID,
		"package-lock.json",
	)
	newerConsumption.ObservedAt = time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	olderConsumption := packageConsumptionCorrelationEnvelope(
		"consume-older-stale",
		repoID,
		packageID,
		"frontend/package-lock.json",
	)
	olderConsumption.ObservedAt = time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	decisions := BuildSecurityAlertReconciliations([]facts.Envelope{
		alert,
		newerConsumption,
		olderConsumption,
	})
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Status, SecurityAlertReconciliationStale; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := decision.DependencyEvidenceID, "consume-newer-stale"; got != want {
		t.Fatalf("DependencyEvidenceID = %q, want newest stale evidence %q", got, want)
	}
}

func TestBuildSecurityAlertReconciliationsResolvesProviderAlertRepositoryScope(t *testing.T) {
	t.Parallel()

	providerRepoID := "security-alert:github:acme/api"
	canonicalRepoID := "repository:r_api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-provider-repo", providerRepoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(7),
		"provider_state":        "open",
		"package_id":            packageID,
		"manifest_path":         "packages/client/package-lock.json",
		"cve_ids":               []string{"CVE-2026-47139"},
		"ghsa_ids":              []string{"GHSA-provider-0002"},
	})
	consumption := packageConsumptionCorrelationEnvelope(
		"consume-provider-repo",
		canonicalRepoID,
		packageID,
		"packages/client/package-lock.json",
	)
	consumption.Payload["repository_name"] = "api"
	impact := supplyChainImpactFindingEnvelope(
		"impact-provider-repo",
		canonicalRepoID,
		packageID,
		"CVE-2026-47139",
		"affected_exact",
	)

	decisions := BuildSecurityAlertReconciliations([]facts.Envelope{alert, consumption, impact})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Status, SecurityAlertReconciliationMatched; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := decision.RepositoryID, canonicalRepoID; got != want {
		t.Fatalf("RepositoryID = %q, want canonical repository id %q", got, want)
	}
	if got, want := decision.ProviderRepositoryID, providerRepoID; got != want {
		t.Fatalf("ProviderRepositoryID = %q, want provider repository id %q", got, want)
	}
}

func TestSecurityAlertReconciliationWriterUsesProviderAlertScopeForPackageTriggeredRepair(t *testing.T) {
	t.Parallel()

	write := SecurityAlertReconciliationWrite{
		ScopeID:      "npm://registry.npmjs.org/serialize-javascript",
		GenerationID: "package-generation-1",
	}
	decision := SecurityAlertReconciliationDecision{
		Provider:                  "github_dependabot",
		ProviderAlertNumber:       12,
		ProviderAlertFactID:       "alert-12",
		RepositoryID:              "repository:r_api",
		ProviderRepositoryID:      "security-alert:github:acme/api",
		ProviderAlertScopeID:      "security-alert:github:acme/api",
		ProviderAlertGenerationID: "security-alert-generation-1",
		SourceFreshness:           "partial",
		CollectionCoverageState:   "incomplete",
		CollectionTruncated:       true,
		CollectionPagesFetched:    2,
		CollectionStateFilter:     "open",
		CollectionIncompleteReasons: []string{
			"provider_open_alert_page_limit_reached",
		},
	}

	identity := securityAlertReconciliationIdentity(write, decision)
	if got, want := identity["scope_id"], "security-alert:github:acme/api"; got != want {
		t.Fatalf("identity scope_id = %q, want provider alert scope %q", got, want)
	}
	if got, want := identity["generation_id"], "security-alert-generation-1"; got != want {
		t.Fatalf("identity generation_id = %q, want provider alert generation %q", got, want)
	}

	payload := securityAlertReconciliationPayload(write, decision)
	if got, want := payload["scope_id"], "security-alert:github:acme/api"; got != want {
		t.Fatalf("payload scope_id = %q, want provider alert scope %q", got, want)
	}
	if got, want := payload["generation_id"], "security-alert-generation-1"; got != want {
		t.Fatalf("payload generation_id = %q, want provider alert generation %q", got, want)
	}
	if got, want := payload["source_freshness"], "partial"; got != want {
		t.Fatalf("payload source_freshness = %q, want %q", got, want)
	}
	if got, want := payload["collection_coverage_state"], "incomplete"; got != want {
		t.Fatalf("payload collection_coverage_state = %q, want %q", got, want)
	}
	if got, want := payload["collection_truncated"], true; got != want {
		t.Fatalf("payload collection_truncated = %v, want %v", got, want)
	}
}

func TestSecurityAlertReconciliationDefersPackageTriggeredUnmatchedEvidence(t *testing.T) {
	t.Parallel()

	intent := Intent{
		SourceSystem: "package_registry",
		Cause:        "package registry identity observed",
		AttemptCount: 1,
	}
	decisions := []SecurityAlertReconciliationDecision{{
		Status:               SecurityAlertReconciliationUnmatched,
		DependencyEvidenceID: "consume-1",
	}}
	if !shouldDeferSecurityAlertReconciliationForPendingImpact(intent, decisions) {
		t.Fatal("shouldDeferSecurityAlertReconciliationForPendingImpact() = false, want true")
	}

	intent.AttemptCount = 3
	if shouldDeferSecurityAlertReconciliationForPendingImpact(intent, decisions) {
		t.Fatal("shouldDeferSecurityAlertReconciliationForPendingImpact() = true after bounded attempts, want false")
	}

	intent.AttemptCount = 1
	intent.SourceSystem = "security_alert"
	intent.Cause = "provider security alert evidence observed"
	if !shouldDeferSecurityAlertReconciliationForPendingImpact(intent, decisions) {
		t.Fatal("shouldDeferSecurityAlertReconciliationForPendingImpact() = false for provider-triggered reconciliation, want true")
	}

	decisions[0].DependencyEvidenceID = ""
	if shouldDeferSecurityAlertReconciliationForPendingImpact(intent, decisions) {
		t.Fatal("shouldDeferSecurityAlertReconciliationForPendingImpact() = true without dependency evidence, want false")
	}
}

func TestBuildSecurityAlertReconciliationsFailsClosedForAmbiguousProviderRepositoryScope(t *testing.T) {
	t.Parallel()

	providerRepoID := "security-alert:github:acme/api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-provider-ambiguous", providerRepoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(8),
		"provider_state":        "open",
		"package_id":            packageID,
		"manifest_path":         "packages/client/package-lock.json",
		"cve_ids":               []string{"CVE-2026-47139"},
	})
	firstConsumption := packageConsumptionCorrelationEnvelope(
		"consume-provider-ambiguous-1",
		"repository:r_api_1",
		packageID,
		"packages/client/package-lock.json",
	)
	firstConsumption.Payload["repository_name"] = "api"
	secondConsumption := packageConsumptionCorrelationEnvelope(
		"consume-provider-ambiguous-2",
		"repository:r_api_2",
		packageID,
		"packages/client/package-lock.json",
	)
	secondConsumption.Payload["repository_name"] = "api"

	decisions := BuildSecurityAlertReconciliations([]facts.Envelope{alert, firstConsumption, secondConsumption})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Status, SecurityAlertReconciliationAmbiguous; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := decision.ReasonCode, "owned_dependency_ambiguous"; got != want {
		t.Fatalf("ReasonCode = %q, want %q", got, want)
	}
	if got, want := len(decision.MissingEvidence), 1; got != want {
		t.Fatalf("len(MissingEvidence) = %d, want %d", got, want)
	}
	if got, want := decision.MissingEvidence[0].Reason, "multiple_repository_candidates"; got != want {
		t.Fatalf("MissingEvidence[0].Reason = %q, want %q", got, want)
	}
	if !strings.Contains(decision.Reason, "ambiguous") {
		t.Fatalf("Reason = %q, want ambiguous evidence reason", decision.Reason)
	}
	if got, want := decision.RepositoryID, providerRepoID; got != want {
		t.Fatalf("RepositoryID = %q, want unresolved provider repository id %q", got, want)
	}
}

func securityAlertEnvelope(factID string, repoID string, payload map[string]any) facts.Envelope {
	payload["repository_id"] = repoID
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          repoID,
		GenerationID:     "generation-1",
		FactKind:         facts.SecurityAlertRepositoryAlertFactKind,
		SchemaVersion:    facts.SecurityAlertSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		Payload:          payload,
	}
}

func packageConsumptionCorrelationEnvelope(factID string, repoID string, packageID string, relativePath string) facts.Envelope {
	return facts.Envelope{
		FactID:       factID,
		ScopeID:      repoID,
		GenerationID: "generation-1",
		FactKind:     packageConsumptionCorrelationFactKind,
		ObservedAt:   time.Date(2026, 5, 23, 11, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"repository_id": repoID,
			"package_id":    packageID,
			"relative_path": relativePath,
			"outcome":       "exact",
		},
	}
}

func supplyChainImpactFindingEnvelope(
	factID string,
	repoID string,
	packageID string,
	cveID string,
	impactStatus string,
) facts.Envelope {
	return facts.Envelope{
		FactID:       factID,
		ScopeID:      repoID,
		GenerationID: "generation-1",
		FactKind:     supplyChainImpactFactKind,
		ObservedAt:   time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"repository_id": repoID,
			"package_id":    packageID,
			"cve_id":        cveID,
			"advisory_id":   "GHSA-abcd-1234",
			"impact_status": impactStatus,
		},
	}
}
