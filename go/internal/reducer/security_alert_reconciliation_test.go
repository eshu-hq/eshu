package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSecurityAlertReconciliationsClassifiesProviderAlertStates(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/eshu-hq/eshu"
	packageID := "npm://registry.npmjs.org/left-pad"
	alert := securityAlertEnvelope("alert-matched", repoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(42),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "npm",
		"package_name":          "left-pad",
		"manifest_path":         "package-lock.json",
		"relationship":          "direct",
		"dependency_scope":      "runtime",
		"ghsa_ids":              []string{"GHSA-abcd-1234"},
		"cve_ids":               []string{"CVE-2026-0001"},
		"updated_at":            "2026-05-23T10:15:00Z",
	})
	consumption := packageConsumptionCorrelationEnvelope("consume-1", repoID, packageID, "package-lock.json")
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
	if got, want := decision.CanonicalWrites, 0; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := decision.ProviderAlertFactID, "alert-matched"; got != want {
		t.Fatalf("ProviderAlertFactID = %q, want %q", got, want)
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
