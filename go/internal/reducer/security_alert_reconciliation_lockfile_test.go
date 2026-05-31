package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSecurityAlertReconciliationsUsesSupportedNpmLockfileEvidence(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/acme/api"
	matchedPackageID := "npm://registry.npmjs.org/matched-lib"
	lockfilePackageID := "npm://registry.npmjs.org/lockfile-lib"
	providerOnlyPackageID := "npm://registry.npmjs.org/provider-only-lib"
	stalePackageID := "npm://registry.npmjs.org/stale-lib"
	observedAt := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		securityAlertEnvelope("alert-matched", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(10),
			"provider_state":        "open",
			"package_id":            matchedPackageID,
			"package_name":          "matched-lib",
			"ecosystem":             "npm",
			"manifest_path":         "package-lock.json",
			"cve_ids":               []string{"CVE-2026-9471"},
			"ghsa_ids":              []string{"GHSA-synthetic-9471"},
			"vulnerable_range":      "<1.2.4",
			"patched_version":       "1.2.4",
		}),
		securityAlertEnvelope("alert-lockfile", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(11),
			"provider_state":        "open",
			"package_id":            lockfilePackageID,
			"package_name":          "lockfile-lib",
			"ecosystem":             "npm",
			"manifest_path":         "package-lock.json",
			"cve_ids":               []string{"CVE-2026-9472"},
			"ghsa_ids":              []string{"GHSA-synthetic-9472"},
			"vulnerable_range":      "<2.3.5",
			"patched_version":       "2.3.5",
		}),
		securityAlertEnvelope("alert-provider-only", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(12),
			"provider_state":        "open",
			"package_id":            providerOnlyPackageID,
			"package_name":          "provider-only-lib",
			"ecosystem":             "npm",
			"manifest_path":         "package-lock.json",
			"cve_ids":               []string{"CVE-2026-9473"},
			"ghsa_ids":              []string{"GHSA-synthetic-9473"},
			"vulnerable_range":      "<3.4.6",
			"patched_version":       "3.4.6",
		}),
		securityAlertEnvelope("alert-stale", repoID, map[string]any{
			"provider":              "github_dependabot",
			"provider_alert_number": int64(13),
			"provider_state":        "open",
			"package_id":            stalePackageID,
			"package_name":          "stale-lib",
			"ecosystem":             "npm",
			"manifest_path":         "old-package-lock.json",
			"updated_at":            "2026-05-24T00:00:00Z",
			"cve_ids":               []string{"CVE-2026-9474"},
			"ghsa_ids":              []string{"GHSA-synthetic-9474"},
		}),
		packageConsumptionCorrelationEnvelope("consume-matched", repoID, matchedPackageID, "package-lock.json"),
		supplyChainImpactFindingEnvelope("impact-matched", repoID, matchedPackageID, "CVE-2026-9471", "affected_exact"),
		packageManifestDependencyFactWithMetadata(
			repoID,
			"api",
			"package-lock.json",
			"lockfile-lib",
			"npm",
			"2.3.4",
			observedAt,
			map[string]any{
				"section":           "package-lock",
				"lockfile":          true,
				"dependency_path":   []any{"root", "lockfile-lib"},
				"dependency_depth":  2,
				"direct_dependency": false,
			},
		),
		supplyChainImpactFindingEnvelope("impact-lockfile", repoID, lockfilePackageID, "CVE-2026-9472", "affected_exact"),
		packageConsumptionCorrelationEnvelope("consume-stale", repoID, stalePackageID, "package-lock.json"),
	}
	envelopes[len(envelopes)-1].ObservedAt = time.Date(2026, 5, 25, 11, 0, 0, 0, time.UTC)

	decisions := BuildSecurityAlertReconciliations(envelopes)
	got := securityAlertDecisionsByFactID(decisions)

	if got["alert-matched"].Status != SecurityAlertReconciliationMatched {
		t.Fatalf("matched status = %q, want matched", got["alert-matched"].Status)
	}
	lockfile := got["alert-lockfile"]
	if lockfile.Status != SecurityAlertReconciliationMatched {
		t.Fatalf("lockfile status = %q, want matched; reason=%q", lockfile.Status, lockfile.Reason)
	}
	if got, want := lockfile.DependencyEvidenceID, "manifest-dep:"+repoID+":lockfile-lib"; got != want {
		t.Fatalf("lockfile DependencyEvidenceID = %q, want supported lockfile fact %q", got, want)
	}
	if got["alert-provider-only"].Status != SecurityAlertReconciliationProviderOnly {
		t.Fatalf("provider-only status = %q, want provider_only", got["alert-provider-only"].Status)
	}
	if !strings.Contains(got["alert-provider-only"].Reason, "no matching owned dependency evidence") {
		t.Fatalf("provider-only reason = %q, want missing dependency evidence reason", got["alert-provider-only"].Reason)
	}
	if got["alert-stale"].Status != SecurityAlertReconciliationStale {
		t.Fatalf("stale status = %q, want stale", got["alert-stale"].Status)
	}
}

func TestSecurityAlertReconciliationHandlerDefersPackageTriggeredLockfileEvidence(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/acme/api"
	packageID := "npm://registry.npmjs.org/pending-lib"
	loader := &recordingSecurityAlertReconciliationFactLoader{
		scopeFacts: []facts.Envelope{
			packageRegistryPackageFact(
				packageID,
				"npm",
				"pending-lib",
				"",
				time.Date(2026, 5, 25, 9, 0, 0, 0, time.UTC),
			),
		},
		activeFacts: []facts.Envelope{
			securityAlertEnvelope("alert-pending", repoID, map[string]any{
				"provider":              "github_dependabot",
				"provider_alert_number": int64(14),
				"provider_state":        "open",
				"package_id":            packageID,
				"package_name":          "pending-lib",
				"ecosystem":             "npm",
				"manifest_path":         "package-lock.json",
				"cve_ids":               []string{"CVE-2026-9475"},
				"ghsa_ids":              []string{"GHSA-synthetic-9475"},
				"vulnerable_range":      "<4.5.7",
				"patched_version":       "4.5.7",
			}),
		},
		manifestFacts: []facts.Envelope{
			packageManifestDependencyFactWithMetadata(
				repoID,
				"api",
				"package-lock.json",
				"pending-lib",
				"npm",
				"4.5.6",
				time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC),
				map[string]any{
					"section":           "package-lock",
					"lockfile":          true,
					"dependency_path":   []any{"root", "pending-lib"},
					"dependency_depth":  2,
					"direct_dependency": false,
				},
			),
		},
	}
	writer := &recordingSecurityAlertReconciliationWriter{}
	handler := SecurityAlertReconciliationHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-package-triggered-reconciliation",
		ScopeID:      packageID,
		GenerationID: "package-generation-1",
		SourceSystem: "package_registry",
		Domain:       DomainSecurityAlertReconciliation,
		Cause:        "package registry identity observed",
		AttemptCount: 1,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want retryable pending-impact error")
	}
	if retryable, ok := err.(interface{ Retryable() bool }); !ok || !retryable.Retryable() {
		t.Fatalf("Handle() error = %T %v, want retryable pending-impact error", err, err)
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want 0 while impact evidence is pending", writer.calls)
	}
	if got, want := strings.Join(loader.manifestEcosystems, ","), "npm"; got != want {
		t.Fatalf("manifest ecosystems = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.manifestPackageNames, ","), "pending-lib"; got != want {
		t.Fatalf("manifest package names = %q, want %q", got, want)
	}
}

func securityAlertDecisionsByFactID(
	decisions []SecurityAlertReconciliationDecision,
) map[string]SecurityAlertReconciliationDecision {
	out := make(map[string]SecurityAlertReconciliationDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.ProviderAlertFactID] = decision
	}
	return out
}

type recordingSecurityAlertReconciliationFactLoader struct {
	scopeFacts           []facts.Envelope
	activeFacts          []facts.Envelope
	manifestFacts        []facts.Envelope
	manifestEcosystems   []string
	manifestPackageNames []string
}

func (l *recordingSecurityAlertReconciliationFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.scopeFacts...), nil
}

func (l *recordingSecurityAlertReconciliationFactLoader) ListFactsByKind(
	context.Context,
	string,
	string,
	[]string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.scopeFacts...), nil
}

func (l *recordingSecurityAlertReconciliationFactLoader) ListActiveSecurityAlertReconciliationFacts(
	context.Context,
	SecurityAlertReconciliationFactFilter,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.activeFacts...), nil
}

func (l *recordingSecurityAlertReconciliationFactLoader) ListActivePackageManifestDependencyFacts(
	_ context.Context,
	ecosystems []string,
	packageNames []string,
) ([]facts.Envelope, error) {
	l.manifestEcosystems = append([]string(nil), ecosystems...)
	l.manifestPackageNames = append([]string(nil), packageNames...)
	return append([]facts.Envelope(nil), l.manifestFacts...), nil
}

type recordingSecurityAlertReconciliationWriter struct {
	calls int
	write SecurityAlertReconciliationWrite
}

func (w *recordingSecurityAlertReconciliationWriter) WriteSecurityAlertReconciliations(
	_ context.Context,
	write SecurityAlertReconciliationWrite,
) (SecurityAlertReconciliationWriteResult, error) {
	w.calls++
	w.write = write
	return SecurityAlertReconciliationWriteResult{CanonicalWrites: len(write.Decisions)}, nil
}
