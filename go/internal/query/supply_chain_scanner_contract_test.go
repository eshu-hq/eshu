package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVulnerabilityScannerReadContractIdentifiesFilters(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/vulnerability-scanner/contract",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal envelope: %v", err)
	}
	if envelope.Truth.Capability != vulnerabilityScannerReadContractCapability {
		t.Fatalf("truth.capability = %q, want %q", envelope.Truth.Capability, vulnerabilityScannerReadContractCapability)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", envelope.Data)
	}
	filters, ok := data["filters"].([]any)
	if !ok {
		t.Fatalf("filters = %T, want []any", data["filters"])
	}
	got := map[string]map[string]any{}
	for _, raw := range filters {
		row, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("filter row = %T, want map[string]any", raw)
		}
		name, _ := row["name"].(string)
		got[name] = row
	}
	wantFilters := []string{
		"repository", "package", "advisory", "image_digest",
		"workload", "service", "environment", "ecosystem", "language",
		"severity", "status", "readiness", "provider_state",
	}
	for _, name := range wantFilters {
		if got[name] == nil {
			t.Fatalf("contract missing filter %q; filters = %#v", name, got)
		}
	}
	if semantics := strings.Join(scannerContractStringSlice(got["provider_state"]["semantics"]), ","); !strings.Contains(semantics, "provider-only") {
		t.Fatalf("provider_state semantics = %q, want provider-only", semantics)
	}
	if support := got["language"]["support"]; support != "unsupported" {
		t.Fatalf("language support = %#v, want unsupported", support)
	}
	if support := got["readiness"]["support"]; support != "missing-evidence driven" {
		t.Fatalf("readiness support = %#v, want missing-evidence driven", support)
	}
	if backing := got["repository"]["backing"]; !strings.Contains(backing.(string), "reducer") {
		t.Fatalf("repository backing = %#v, want reducer read model", backing)
	}
}

func TestVulnerabilityScannerReadContractRejectsUnknownRouteNames(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/vulnerability-scanner/contract?route=whole_graph",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "route must be one of") {
		t.Fatalf("body = %q, want route guidance", w.Body.String())
	}
}

func scannerContractStringSlice(raw any) []string {
	items, _ := raw.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
