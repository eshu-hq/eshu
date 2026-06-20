package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
)

func TestComponentExtensionsHandlerClassifiesHostedStatusFields(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	installComponentForQueryStatusTest(t, home, componentStatusInstall{
		ID:             "dev.eshu.collector.scorecard",
		Name:           "Scorecard collector",
		CompatibleCore: ">=0.0.1",
		ConfigPath:     filepath.Join(t.TempDir(), "private", "scorecard.yaml"),
		ClaimsEnabled:  true,
	})
	installComponentForQueryStatusTest(t, home, componentStatusInstall{
		ID:             "dev.eshu.collector.blocked",
		Name:           "Blocked collector",
		CompatibleCore: ">=0.0.1",
	})
	installComponentForQueryStatusTest(t, home, componentStatusInstall{
		ID:             "dev.eshu.collector.future",
		Name:           "Future collector",
		CompatibleCore: ">=99.0.0",
	})
	installComponentForQueryStatusTest(t, home, componentStatusInstall{
		ID:             "dev.eshu.collector.failed",
		Name:           "Failed collector",
		CompatibleCore: ">=0.0.1",
		RemoveManifest: true,
	})
	handler := &ComponentExtensionsHandler{
		ComponentHome: home,
		Policy: component.Policy{
			Mode:              component.TrustModeAllowlist,
			AllowedIDs:        []string{"dev.eshu.collector.scorecard", "dev.eshu.collector.future", "dev.eshu.collector.failed"},
			AllowedPublishers: []string{"eshu-hq"},
			CoreVersion:       "v0.0.5",
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/component-extensions", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var envelope struct {
		Data  ComponentExtensionInventoryResponse `json:"data"`
		Error *ErrorEnvelope                      `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("envelope.Error = %#v, want nil", envelope.Error)
	}

	components := map[string]ComponentExtensionComponent{}
	for _, row := range envelope.Data.Components {
		components[row.ID] = row
	}
	assertComponentHostedStatus(t, components["dev.eshu.collector.scorecard"], componentHostedStatusWant{
		TrustDecision:        "allowed",
		PolicyGate:           "allowed",
		ConformanceProof:     "missing",
		SchedulerState:       "claim_capable",
		ReadModelState:       "unavailable",
		ReadModelUnavailable: "missing_conformance_proof",
	})
	assertComponentHostedStatus(t, components["dev.eshu.collector.blocked"], componentHostedStatusWant{
		TrustDecision:        "blocked",
		PolicyGate:           "disabled_by_policy",
		ConformanceProof:     "missing",
		SchedulerState:       "blocked_by_policy",
		ReadModelState:       "unavailable",
		ReadModelUnavailable: "policy_blocked",
	})
	assertComponentHostedStatus(t, components["dev.eshu.collector.future"], componentHostedStatusWant{
		TrustDecision:        "blocked",
		PolicyGate:           "incompatible",
		ConformanceProof:     "missing",
		SchedulerState:       "blocked_by_policy",
		ReadModelState:       "unavailable",
		ReadModelUnavailable: "policy_blocked",
	})
	assertComponentHostedStatus(t, components["dev.eshu.collector.failed"], componentHostedStatusWant{
		TrustDecision:        "not_evaluated",
		PolicyGate:           "runtime_failure",
		ConformanceProof:     "missing",
		SchedulerState:       "runtime_failure",
		ReadModelState:       "unavailable",
		ReadModelUnavailable: "runtime_failure",
	})
	raw := rec.Body.String()
	for _, forbidden := range []string{home, "scorecard.yaml", "manifest.yaml", "config_path", "manifest_path"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("component status response leaked %q: %s", forbidden, raw)
		}
	}
}

type componentHostedStatusWant struct {
	TrustDecision        string
	PolicyGate           string
	ConformanceProof     string
	SchedulerState       string
	ReadModelState       string
	ReadModelUnavailable string
}

func assertComponentHostedStatus(t *testing.T, row ComponentExtensionComponent, want componentHostedStatusWant) {
	t.Helper()

	if got := row.TrustDecision.Decision; got != want.TrustDecision {
		t.Fatalf("%s trust decision = %q, want %q", row.ID, got, want.TrustDecision)
	}
	if got := row.PolicyGate.State; got != want.PolicyGate {
		t.Fatalf("%s policy gate = %q, want %q", row.ID, got, want.PolicyGate)
	}
	if got := row.LastConformanceProof.Status; got != want.ConformanceProof {
		t.Fatalf("%s conformance proof = %q, want %q", row.ID, got, want.ConformanceProof)
	}
	if got := row.SchedulerState.State; got != want.SchedulerState {
		t.Fatalf("%s scheduler state = %q, want %q", row.ID, got, want.SchedulerState)
	}
	if got := row.ReadModelAvailability.State; got != want.ReadModelState {
		t.Fatalf("%s read model state = %q, want %q", row.ID, got, want.ReadModelState)
	}
	if got := row.ReadModelAvailability.UnavailableReason; got != want.ReadModelUnavailable {
		t.Fatalf("%s read model reason = %q, want %q", row.ID, got, want.ReadModelUnavailable)
	}
}

type componentStatusInstall struct {
	ID             string
	Name           string
	CompatibleCore string
	ConfigPath     string
	ClaimsEnabled  bool
	RemoveManifest bool
}

func installComponentForQueryStatusTest(t *testing.T, home string, input componentStatusInstall) {
	t.Helper()

	manifestPath := writeComponentExtensionQueryStatusManifest(t, input)
	manifest, err := component.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{input.ID},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v99.0.0",
	}
	registry := component.NewRegistry(home)
	if _, err := registry.Install(manifestPath, policy.Verify(manifest)); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if input.ConfigPath != "" {
		if _, err := registry.Enable(input.ID, component.Activation{
			InstanceID:    "primary",
			Mode:          "scheduled",
			ClaimsEnabled: input.ClaimsEnabled,
			ConfigPath:    input.ConfigPath,
		}); err != nil {
			t.Fatalf("Enable() error = %v", err)
		}
	}
	if input.RemoveManifest {
		storedManifest := filepath.Join(home, "packages", input.ID, "0.1.0", "manifest.yaml")
		if err := os.Remove(storedManifest); err != nil {
			t.Fatalf("os.Remove(%q) error = %v, want nil", storedManifest, err)
		}
	}
}

func writeComponentExtensionQueryStatusManifest(t *testing.T, input componentStatusInstall) string {
	t.Helper()

	body := strings.ReplaceAll(`apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: COMPONENT_ID
  name: COMPONENT_NAME
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: "COMPATIBLE_CORE"
  componentType: collector
  collectorKinds:
    - scorecard
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: process
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/scorecard@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: COMPONENT_ID.observation
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - source_evidence_only:no_graph_truth
`, "COMPONENT_ID", input.ID)
	body = strings.ReplaceAll(body, "COMPONENT_NAME", input.Name)
	body = strings.ReplaceAll(body, "COMPATIBLE_CORE", input.CompatibleCore)
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}
	return path
}
