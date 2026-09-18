// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

func capabilitiesRequest(t *testing.T, target string) ResponseEnvelope {
	t.Helper()
	mux := http.NewServeMux()
	router := &APIRouter{Capabilities: &CapabilitiesHandler{Profile: ProfileProduction}}
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return envelope
}

func TestCapabilitiesHandlerListsCatalogWithExactTruth(t *testing.T) {
	t.Parallel()

	envelope := capabilitiesRequest(t, "/api/v0/capabilities?view=full&include_authorization=true&limit=500")
	if envelope.Error != nil {
		t.Fatalf("envelope error = %+v, want nil", envelope.Error)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope is nil")
	}
	if got, want := envelope.Truth.Capability, capabilityCatalogCapability; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Level, TruthLevelExact; got != want {
		t.Fatalf("truth level = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Freshness.State, FreshnessFresh; got != want {
		t.Fatalf("freshness = %q, want %q", got, want)
	}

	data := envelope.Data.(map[string]any)
	catalog, err := capabilitycatalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	// Parity: the API total equals the embedded catalog size.
	if got, want := int(data["total"].(float64)), len(catalog.Entries); got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
	capabilities := data["capabilities"].([]any)
	if len(capabilities) != len(catalog.Entries) {
		t.Fatalf("returned %d capabilities, want %d", len(capabilities), len(catalog.Entries))
	}
	authorization := data["authorization"].(map[string]any)
	if got, want := authorization["version"].(string), catalog.Authorization.Version; got != want {
		t.Fatalf("authorization version = %q, want %q", got, want)
	}
	if got, want := len(authorization["roles"].([]any)), len(catalog.Authorization.Roles); got != want {
		t.Fatalf("authorization roles = %d, want %d", got, want)
	}
	first := capabilities[0].(map[string]any)
	if first["capability"].(string) != catalog.Entries[0].Capability {
		t.Fatalf("first capability = %q, want %q", first["capability"], catalog.Entries[0].Capability)
	}
	entryAuthorization := first["authorization"].(map[string]any)
	if entryAuthorization["family"].(string) == "" || entryAuthorization["action"].(string) == "" {
		t.Fatalf("first capability authorization missing family/action: %+v", entryAuthorization)
	}

	foundProfileBudget := false
	for i, raw := range capabilities {
		entry := raw.(map[string]any)
		profiles, ok := entry["profiles"].(map[string]any)
		if !ok {
			t.Fatalf("capability %q missing profiles", entry["capability"])
		}
		production, ok := profiles["production"].(map[string]any)
		if !ok {
			t.Fatalf("capability %q missing production profile", entry["capability"])
		}
		expected := catalog.Entries[i].Profiles["production"]
		if expected.P95LatencyMS != nil {
			if got, want := int(production["p95_latency_ms"].(float64)), *expected.P95LatencyMS; got != want {
				t.Fatalf("%s production p95_latency_ms = %d, want %d", entry["capability"], got, want)
			}
			if got, want := production["max_scope_size"].(string), expected.MaxScopeSize; got != want {
				t.Fatalf("%s production max_scope_size = %q, want %q", entry["capability"], got, want)
			}
			foundProfileBudget = true
			break
		}
	}
	if !foundProfileBudget {
		t.Fatal("catalog response has no production profile with a p95 latency budget")
	}
}

func TestOpenAPISpecDocumentsCapabilityAuthorizationCatalog(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := spec["paths"].(map[string]any)
	path := paths["/api/v0/capabilities"].(map[string]any)
	get := path["get"].(map[string]any)
	response := get["responses"].(map[string]any)["200"].(map[string]any)
	content := response["content"].(map[string]any)["application/json"].(map[string]any)
	schema := content["schema"].(map[string]any)
	properties := schema["properties"].(map[string]any)

	if _, ok := properties["authorization"]; !ok {
		t.Fatal("capabilities OpenAPI response missing top-level authorization catalog")
	}
	authorization := properties["authorization"].(map[string]any)
	authzProperties := authorization["properties"].(map[string]any)
	dataClasses := authzProperties["data_classes"].(map[string]any)
	dataClassItems := dataClasses["items"].(map[string]any)
	dataClassProperties := dataClassItems["properties"].(map[string]any)
	sensitivity := dataClassProperties["sensitivity"].(map[string]any)
	if !openAPIStringListIncludes(sensitivity["enum"].([]any), "restricted") {
		t.Fatal("capabilities OpenAPI data-class sensitivity enum missing restricted")
	}
	capabilities := properties["capabilities"].(map[string]any)
	items := capabilities["items"].(map[string]any)
	entryProperties := items["properties"].(map[string]any)
	if _, ok := entryProperties["authorization"]; !ok {
		t.Fatal("capabilities OpenAPI entry missing authorization metadata")
	}
	profiles, ok := entryProperties["profiles"].(map[string]any)
	if !ok {
		t.Fatal("capabilities OpenAPI entry missing profile metadata")
	}
	profileProperties := profiles["additionalProperties"].(map[string]any)["properties"].(map[string]any)
	if _, ok := profileProperties["p95_latency_ms"]; !ok {
		t.Fatal("capabilities OpenAPI profile metadata missing p95_latency_ms")
	}
	if _, ok := profileProperties["max_scope_size"]; !ok {
		t.Fatal("capabilities OpenAPI profile metadata missing max_scope_size")
	}
}

func TestCapabilitiesHandlerFiltersByMaturity(t *testing.T) {
	t.Parallel()

	envelope := capabilitiesRequest(t, "/api/v0/capabilities?maturity=general_availability")
	data := envelope.Data.(map[string]any)
	for _, raw := range data["capabilities"].([]any) {
		entry := raw.(map[string]any)
		if entry["maturity"].(string) != "general_availability" {
			t.Fatalf("maturity filter leaked %q", entry["maturity"])
		}
	}

	none := capabilitiesRequest(t, "/api/v0/capabilities?maturity=does_not_exist")
	noneData := none.Data.(map[string]any)
	if got := int(noneData["total"].(float64)); got != 0 {
		t.Fatalf("unknown maturity total = %d, want 0", got)
	}
}

func TestCapabilitiesHandlerPagesDeterministically(t *testing.T) {
	t.Parallel()

	page := capabilitiesRequest(t, "/api/v0/capabilities?limit=2&offset=0")
	data := page.Data.(map[string]any)
	if got := len(data["capabilities"].([]any)); got != 2 {
		t.Fatalf("page size = %d, want 2", got)
	}
	if truncated, ok := data["truncated"].(bool); !ok || !truncated {
		t.Fatalf("truncated = %v, want true", data["truncated"])
	}

	catalog, _ := capabilitycatalog.Load()
	second := capabilitiesRequest(t, "/api/v0/capabilities?limit=2&offset=2")
	secondData := second.Data.(map[string]any)
	secondFirst := secondData["capabilities"].([]any)[0].(map[string]any)
	if secondFirst["capability"].(string) != catalog.Entries[2].Capability {
		t.Fatalf("offset paging mismatch: got %q, want %q", secondFirst["capability"], catalog.Entries[2].Capability)
	}
}

func TestCapabilitiesHandlerRejectsBadLimit(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{Capabilities: &CapabilitiesHandler{Profile: ProfileProduction}}
	router.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/capabilities?limit=9999", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestCapabilitiesHandlerRejectsBadView proves an unrecognized view value is a
// bounded 400, not a silent fallback (#6795).
func TestCapabilitiesHandlerRejectsBadView(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{Capabilities: &CapabilitiesHandler{Profile: ProfileProduction}}
	router.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/capabilities?view=verbose", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestCapabilitiesHandlerDefaultViewOmitsProfilesAndProofSignals proves the
// default (compact) response drops the two largest per-entry fields while
// keeping every other field, and that the top-level authorization catalog is
// present but empty (#6795).
func TestCapabilitiesHandlerDefaultViewOmitsProfilesAndProofSignals(t *testing.T) {
	t.Parallel()

	envelope := capabilitiesRequest(t, "/api/v0/capabilities")
	data := envelope.Data.(map[string]any)

	authorization, ok := data["authorization"].(map[string]any)
	if !ok {
		t.Fatalf("authorization field missing or wrong type: %#v", data["authorization"])
	}
	if version, _ := authorization["version"].(string); version != "" {
		t.Fatalf("default authorization.version = %q, want empty (opt in with include_authorization=true)", version)
	}
	if roles, ok := authorization["roles"].([]any); ok && len(roles) != 0 {
		t.Fatalf("default authorization.roles = %#v, want empty", roles)
	}

	capabilities := data["capabilities"].([]any)
	if len(capabilities) == 0 {
		t.Fatal("capabilities page is empty")
	}
	for _, raw := range capabilities {
		entry := raw.(map[string]any)
		if _, ok := entry["profiles"]; ok {
			t.Fatalf("compact entry %q carries profiles, want omitted", entry["capability"])
		}
		if _, ok := entry["proof_signals"]; ok {
			t.Fatalf("compact entry %q carries proof_signals, want omitted", entry["capability"])
		}
		if entry["capability"].(string) == "" {
			t.Fatal("compact entry missing capability id")
		}
		if _, ok := entry["surfaces"]; !ok {
			t.Fatal("compact entry missing surfaces")
		}
	}
}

// TestCapabilitiesHandlerDefaultPageFitsResponseBudget proves the default
// call's serialized body stays under an MCP-client-friendly budget. This is
// the acceptance test #6795 itself specifies (default page < ~8KB).
func TestCapabilitiesHandlerDefaultPageFitsResponseBudget(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{Capabilities: &CapabilitiesHandler{Profile: ProfileProduction}}
	router.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/capabilities", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	const budget = 8 * 1024
	if got := rec.Body.Len(); got >= budget {
		t.Fatalf("default /api/v0/capabilities body = %d bytes, want < %d", got, budget)
	}
}

// TestCapabilitiesHandlerFullViewIncludesProfilesAndAuthorization proves
// view=full plus include_authorization=true restores the pre-#6795 shape, so
// console and any other caller of the full detail keeps working via opt-in.
func TestCapabilitiesHandlerFullViewIncludesProfilesAndAuthorization(t *testing.T) {
	t.Parallel()

	envelope := capabilitiesRequest(t, "/api/v0/capabilities?view=full&include_authorization=true&limit=2")
	data := envelope.Data.(map[string]any)

	authorization := data["authorization"].(map[string]any)
	if version, _ := authorization["version"].(string); version == "" {
		t.Fatal("include_authorization=true returned empty authorization catalog")
	}

	capabilities := data["capabilities"].([]any)
	if len(capabilities) == 0 {
		t.Fatal("capabilities page is empty")
	}
	entry := capabilities[0].(map[string]any)
	if _, ok := entry["profiles"]; !ok {
		t.Fatal("view=full entry missing profiles")
	}
}
