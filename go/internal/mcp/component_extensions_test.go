package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestComponentExtensionToolsResolveToQueryRoutes(t *testing.T) {
	t.Parallel()

	inventory, err := resolveRoute("list_component_extensions", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute(list_component_extensions) error = %v, want nil", err)
	}
	if got, want := inventory.method, "GET"; got != want {
		t.Fatalf("inventory method = %q, want %q", got, want)
	}
	if got, want := inventory.path, "/api/v0/component-extensions"; got != want {
		t.Fatalf("inventory path = %q, want %q", got, want)
	}

	boundedInventory, err := resolveRoute("list_component_extensions", map[string]any{
		"limit": float64(1),
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_component_extensions limit) error = %v, want nil", err)
	}
	if got, want := boundedInventory.path, "/api/v0/component-extensions"; got != want {
		t.Fatalf("bounded inventory path = %q, want %q", got, want)
	}
	if got, want := boundedInventory.query["limit"], "1"; got != want {
		t.Fatalf("bounded inventory limit query = %q, want %q", got, want)
	}

	diagnostics, err := resolveRoute("get_component_extension_diagnostics", map[string]any{
		"component_id": "dev.eshu.collector.aws",
	})
	if err != nil {
		t.Fatalf("resolveRoute(get_component_extension_diagnostics) error = %v, want nil", err)
	}
	if got, want := diagnostics.method, "GET"; got != want {
		t.Fatalf("diagnostics method = %q, want %q", got, want)
	}
	if got, want := diagnostics.path, "/api/v0/component-extensions/dev.eshu.collector.aws/diagnostics"; got != want {
		t.Fatalf("diagnostics path = %q, want %q", got, want)
	}
}

func TestReadOnlyToolsIncludesComponentExtensionDiagnostics(t *testing.T) {
	t.Parallel()

	names := map[string]bool{}
	for _, tool := range ReadOnlyTools() {
		names[tool.Name] = true
	}
	for _, want := range []string{"list_component_extensions", "get_component_extension_diagnostics"} {
		if !names[want] {
			t.Fatalf("ReadOnlyTools() missing %q", want)
		}
	}
}

func TestDispatchToolComponentExtensionsAllowsScopedRoutes(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	const componentID = "dev.eshu.collector.aws"
	installComponentForMCPTest(t, home, componentID, "AWS cloud scanner")
	manifestPath := filepath.Join(home, "packages", componentID, "0.1.0", "manifest.yaml")
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("os.Remove(%q) error = %v, want nil", manifestPath, err)
	}

	componentHandler := &query.ComponentExtensionsHandler{
		ComponentHome: home,
		Profile:       query.ProfileProduction,
	}
	mux := http.NewServeMux()
	componentHandler.Mount(mux)
	resolver := &mcpScopedTokenResolver{
		auth: query.AuthContext{
			Mode:        query.AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	handler := query.AuthMiddlewareWithScopedTokens("", resolver, mux)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	inventory, err := dispatchTool(
		context.Background(),
		handler,
		"list_component_extensions",
		map[string]any{"limit": float64(1)},
		"Bearer scoped-token",
		logger,
	)
	if err != nil {
		t.Fatalf("dispatchTool(list_component_extensions) error = %v, want nil", err)
	}
	assertComponentExtensionDispatchResult(t, inventory, "component_extensions.inventory", home, manifestPath)

	diagnostics, err := dispatchTool(
		context.Background(),
		handler,
		"get_component_extension_diagnostics",
		map[string]any{"component_id": componentID},
		"Bearer scoped-token",
		logger,
	)
	if err != nil {
		t.Fatalf("dispatchTool(get_component_extension_diagnostics) error = %v, want nil", err)
	}
	assertComponentExtensionDispatchResult(t, diagnostics, "component_extensions.diagnostics", home, manifestPath)
}

func assertComponentExtensionDispatchResult(
	t *testing.T,
	result *dispatchResult,
	capability string,
	forbiddenValues ...string,
) {
	t.Helper()

	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want component extension envelope")
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != capability {
		t.Fatalf("truth = %#v, want capability %q", result.Envelope.Truth, capability)
	}
	rawBytes, err := json.Marshal(result.Envelope)
	if err != nil {
		t.Fatalf("json.Marshal(envelope) error = %v, want nil", err)
	}
	raw := string(rawBytes)
	for _, want := range []string{
		`"trust_decision"`,
		`"policy_gate"`,
		`"last_conformance_proof"`,
		`"scheduler_state"`,
		`"read_model_availability"`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("component extension dispatch missing %s: %s", want, raw)
		}
	}
	forbidden := append([]string{
		"manifest.yaml",
		"manifest_path",
		"config_path",
	}, forbiddenValues...)
	for _, value := range forbidden {
		if value == "" {
			continue
		}
		if strings.Contains(raw, value) {
			t.Fatalf("component extension dispatch leaked %q: %s", value, raw)
		}
	}
}

func installComponentForMCPTest(t *testing.T, home string, componentID string, name string) {
	t.Helper()

	registry := component.NewRegistry(home)
	manifestPath := writeComponentExtensionMCPManifest(t, componentID, name)
	manifest, err := component.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{componentID},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.0.5",
	}
	if _, err := registry.Install(manifestPath, policy.Verify(manifest)); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
}

func writeComponentExtensionMCPManifest(t *testing.T, componentID string, name string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "manifest.yaml")
	body := strings.ReplaceAll(`apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: COMPONENT_ID
  name: COMPONENT_NAME
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.1"
  componentType: collector
  collectorKinds:
    - aws
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: process
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/aws@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: FACT_KIND
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - observed
  consumerContracts:
    reducer:
      phases:
        - source_evidence_only:no_graph_truth
`, "COMPONENT_ID", componentID)
	body = strings.ReplaceAll(body, "COMPONENT_NAME", name)
	body = strings.ReplaceAll(body, "FACT_KIND", componentID+".observation")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}
