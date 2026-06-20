package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
)

func TestComponentExtensionsHandlerReturnsUnavailableWhenComponentHomeUnset(t *testing.T) {
	t.Parallel()

	handler := &ComponentExtensionsHandler{Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/component-extensions", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope.Error == nil {
		t.Fatal("envelope.Error = nil, want unavailable error")
	}
	if got, want := envelope.Error.Code, ErrorCodeComponentRegistryUnavailable; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if !strings.Contains(envelope.Error.Message, "ESHU_COMPONENT_HOME") {
		t.Fatalf("error message = %q, want ESHU_COMPONENT_HOME guidance", envelope.Error.Message)
	}
}

func TestComponentExtensionsHandlerListsSanitizedInventoryAndDiagnostics(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "private", "aws-credentials.yaml")
	installComponentForQueryTest(t, home, "dev.eshu.collector.aws", "AWS cloud scanner", configPath)

	handler := &ComponentExtensionsHandler{
		ComponentHome: home,
		Policy: component.Policy{
			Mode:              component.TrustModeAllowlist,
			AllowedIDs:        []string{"dev.eshu.collector.aws"},
			AllowedPublishers: []string{"eshu-hq"},
			RevokedIDs:        []string{"dev.eshu.collector.aws"},
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
		Truth *TruthEnvelope                      `json:"truth"`
		Error *ErrorEnvelope                      `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("envelope.Error = %#v, want nil", envelope.Error)
	}
	if got, want := envelope.Truth.Capability, "component_extensions.inventory"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := envelope.Data.Status, "available"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got, want := len(envelope.Data.Components), 1; got != want {
		t.Fatalf("component count = %d, want %d", got, want)
	}
	componentRow := envelope.Data.Components[0]
	if got, want := componentRow.ID, "dev.eshu.collector.aws"; got != want {
		t.Fatalf("component id = %q, want %q", got, want)
	}
	for _, wantState := range []string{"installed", "enabled", "claim_capable", "revoked", "failed"} {
		if !slices.Contains(componentRow.States, wantState) {
			t.Fatalf("states = %v, want %q", componentRow.States, wantState)
		}
	}
	if componentRow.Diagnostics == nil {
		t.Fatal("diagnostics = nil, want policy diagnostics")
	}
	if got, want := componentRow.Diagnostics.PolicyAllowed, false; got != want {
		t.Fatalf("policy_allowed = %t, want %t", got, want)
	}
	if got, want := componentRow.Diagnostics.PolicyCode, component.ErrorCodeRevokedPackage; got != want {
		t.Fatalf("policy code = %q, want %q", got, want)
	}
	if got := componentRow.Activations[0].ConfigHandle; !strings.HasPrefix(got, "component-config:") {
		t.Fatalf("config_handle = %q, want stable component-config handle", got)
	}
	raw := rec.Body.String()
	for _, forbidden := range []string{home, configPath, "manifest.yaml", "aws-credentials.yaml", "manifest_path", "config_path"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("response leaked %q in body: %s", forbidden, raw)
		}
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v0/component-extensions/dev.eshu.collector.aws/diagnostics", nil)
	detailReq.Header.Set("Accept", EnvelopeMIMEType)
	detailRec := httptest.NewRecorder()
	mux.ServeHTTP(detailRec, detailReq)
	if got, want := detailRec.Code, http.StatusOK; got != want {
		t.Fatalf("detail status = %d, want %d; body=%s", got, want, detailRec.Body.String())
	}
	if strings.Contains(detailRec.Body.String(), configPath) || strings.Contains(detailRec.Body.String(), home) {
		t.Fatalf("detail response leaked local paths: %s", detailRec.Body.String())
	}
}

func TestComponentExtensionsHandlerBoundsInventoryWithLimit(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	installComponentForQueryTest(t, home, "dev.eshu.collector.aws", "AWS cloud scanner", "")
	installComponentForQueryTest(t, home, "dev.eshu.collector.gcp", "GCP cloud scanner", "")

	handler := &ComponentExtensionsHandler{
		ComponentHome: home,
		Profile:       ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/component-extensions?limit=1", nil)
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
	if got, want := envelope.Data.Limit, 1; got != want {
		t.Fatalf("limit = %d, want %d", got, want)
	}
	if got, want := envelope.Data.Count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := envelope.Data.TotalCount, 2; got != want {
		t.Fatalf("total_count = %d, want %d", got, want)
	}
	if !envelope.Data.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := envelope.Data.Components[0].ID, "dev.eshu.collector.aws"; got != want {
		t.Fatalf("first component id = %q, want deterministic first id %q", got, want)
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsComponentExtensionRoutes(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	const componentID = "dev.eshu.collector.aws"
	installComponentForQueryTest(t, home, componentID, "AWS cloud scanner", "")
	manifestPath := filepath.Join(home, "packages", componentID, "0.1.0", "manifest.yaml")
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("os.Remove(%q) error = %v, want nil", manifestPath, err)
	}

	handler := &ComponentExtensionsHandler{
		ComponentHome: home,
		Profile:       ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:        AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	authHandler := AuthMiddlewareWithScopedTokens("", resolver, mux)

	inventoryReq := httptest.NewRequest(http.MethodGet, "/api/v0/component-extensions?limit=1", nil)
	inventoryReq.Header.Set("Authorization", "Bearer scoped-token")
	inventoryReq.Header.Set("Accept", EnvelopeMIMEType)
	inventoryRec := httptest.NewRecorder()
	authHandler.ServeHTTP(inventoryRec, inventoryReq)
	if got, want := inventoryRec.Code, http.StatusOK; got != want {
		t.Fatalf("inventory status = %d, want %d; body=%s", got, want, inventoryRec.Body.String())
	}
	assertComponentExtensionResponseRedacted(t, inventoryRec.Body.String(), home, manifestPath)
	var inventoryEnvelope struct {
		Data  ComponentExtensionInventoryResponse `json:"data"`
		Truth *TruthEnvelope                      `json:"truth"`
		Error *ErrorEnvelope                      `json:"error"`
	}
	if err := json.Unmarshal(inventoryRec.Body.Bytes(), &inventoryEnvelope); err != nil {
		t.Fatalf("json.Unmarshal(inventory) error = %v, want nil", err)
	}
	if inventoryEnvelope.Error != nil {
		t.Fatalf("inventory error = %#v, want nil", inventoryEnvelope.Error)
	}
	if inventoryEnvelope.Truth == nil || inventoryEnvelope.Truth.Capability != componentExtensionsInventoryCapability {
		t.Fatalf("inventory truth = %#v, want component extension inventory truth", inventoryEnvelope.Truth)
	}
	if got, want := inventoryEnvelope.Data.Count, 1; got != want {
		t.Fatalf("inventory count = %d, want %d", got, want)
	}

	diagnosticsReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/component-extensions/"+componentID+"/diagnostics",
		nil,
	)
	diagnosticsReq.Header.Set("Authorization", "Bearer scoped-token")
	diagnosticsReq.Header.Set("Accept", EnvelopeMIMEType)
	diagnosticsRec := httptest.NewRecorder()
	authHandler.ServeHTTP(diagnosticsRec, diagnosticsReq)
	if got, want := diagnosticsRec.Code, http.StatusOK; got != want {
		t.Fatalf("diagnostics status = %d, want %d; body=%s", got, want, diagnosticsRec.Body.String())
	}
	assertComponentExtensionResponseRedacted(t, diagnosticsRec.Body.String(), home, manifestPath)
	var diagnosticsEnvelope struct {
		Data  ComponentExtensionDiagnosticsResponse `json:"data"`
		Truth *TruthEnvelope                        `json:"truth"`
		Error *ErrorEnvelope                        `json:"error"`
	}
	if err := json.Unmarshal(diagnosticsRec.Body.Bytes(), &diagnosticsEnvelope); err != nil {
		t.Fatalf("json.Unmarshal(diagnostics) error = %v, want nil", err)
	}
	if diagnosticsEnvelope.Error != nil {
		t.Fatalf("diagnostics error = %#v, want nil", diagnosticsEnvelope.Error)
	}
	if diagnosticsEnvelope.Truth == nil || diagnosticsEnvelope.Truth.Capability != componentExtensionsDiagnosticsCapability {
		t.Fatalf("diagnostics truth = %#v, want component extension diagnostics truth", diagnosticsEnvelope.Truth)
	}
	if got, want := diagnosticsEnvelope.Data.Component.ID, componentID; got != want {
		t.Fatalf("diagnostics component id = %q, want %q", got, want)
	}
}

func assertComponentExtensionResponseRedacted(
	t *testing.T,
	body string,
	forbiddenValues ...string,
) {
	t.Helper()

	forbidden := append([]string{
		"manifest.yaml",
		"manifest_path",
		"config_path",
	}, forbiddenValues...)
	for _, value := range forbidden {
		if value == "" {
			continue
		}
		if strings.Contains(body, value) {
			t.Fatalf("component extension response leaked %q: %s", value, body)
		}
	}
}

func TestOpenAPISpecIncludesComponentExtensionRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	for _, path := range []string{
		"/api/v0/component-extensions",
		"/api/v0/component-extensions/{component_id}/diagnostics",
	} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("OpenAPI paths missing %s", path)
		}
	}
	inventory := mustMapField(t, paths, "/api/v0/component-extensions")
	get := mustMapField(t, inventory, "get")
	responses := mustMapField(t, get, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, okResponse, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")
	components := mustMapField(t, properties, "components")
	items := mustMapField(t, components, "items")
	componentProperties := mustMapField(t, items, "properties")
	for _, field := range []string{
		"trust_decision",
		"policy_gate",
		"last_conformance_proof",
		"scheduler_state",
		"read_model_availability",
	} {
		if _, ok := componentProperties[field]; !ok {
			t.Fatalf("OpenAPI component extension schema missing %q", field)
		}
	}
}

func installComponentForQueryTest(t *testing.T, home string, componentID string, name string, configPath string) {
	t.Helper()

	registry := component.NewRegistry(home)
	manifestPath := writeComponentExtensionQueryManifest(t, componentID, name)
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
	if configPath == "" {
		return
	}
	if _, err := registry.Enable(componentID, component.Activation{
		InstanceID:    "prod-aws",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    configPath,
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
}

func writeComponentExtensionQueryManifest(t *testing.T, componentID string, name string) string {
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
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}
