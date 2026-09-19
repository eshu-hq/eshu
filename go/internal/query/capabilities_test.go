// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// capabilitiesRawBody performs the request and returns the raw response body,
// for byte-size and byte-identity comparisons that json.Unmarshal into
// map[string]any would blur (map key order, number formatting).
func capabilitiesRawBody(t *testing.T, target string) []byte {
	t.Helper()
	mux := http.NewServeMux()
	router := &APIRouter{Capabilities: &CapabilitiesHandler{Profile: ProfileProduction}}
	router.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

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
	if got, ok := data["next_offset"].(float64); !ok || int(got) != 2 {
		t.Fatalf("next_offset = %#v, want 2", data["next_offset"])
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
	got := rec.Body.Len()
	t.Logf("default GET /api/v0/capabilities (limit=%d, compact, no authorization) body = %d bytes", capabilitiesDefaultLimit, got)
	if got >= budget {
		t.Fatalf("default /api/v0/capabilities body = %d bytes, want < %d", got, budget)
	}
}

// TestCapabilitiesHandlerBeforeAfterPayloadSize measures the pre-#6795
// default payload (view=full, include_authorization=true, the old
// limit=200 default) against the post-#6795 default and logs both, proving
// the reduction is real and measured, not asserted (#6795).
func TestCapabilitiesHandlerBeforeAfterPayloadSize(t *testing.T) {
	t.Parallel()

	before := capabilitiesRawBody(t, "/api/v0/capabilities?view=full&include_authorization=true&limit=200")
	after := capabilitiesRawBody(t, "/api/v0/capabilities")
	t.Logf("get_capability_catalog default payload: before(#6795 shape, limit=200,view=full,include_authorization=true)=%d bytes, after(compact default, limit=%d)=%d bytes",
		len(before), capabilitiesDefaultLimit, len(after))
	if len(after) >= len(before) {
		t.Fatalf("after size %d bytes not smaller than before size %d bytes", len(after), len(before))
	}
}

// TestCapabilitiesHandlerFullViewIsByteIdenticalToEntry proves view=full's
// per-entry JSON is exactly capabilitycatalog.Entry's own serialization --
// not a hand-copied projection that could silently drop a field Entry gains
// later (#6795 review finding).
func TestCapabilitiesHandlerFullViewIsByteIdenticalToEntry(t *testing.T) {
	t.Parallel()

	catalog, err := capabilitycatalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	limit := len(catalog.Entries)
	got := capabilitiesRawBody(t, "/api/v0/capabilities?view=full&include_authorization=true&limit="+strconv.Itoa(limit))

	var envelope struct {
		Data struct {
			Capabilities json.RawMessage `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got, &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	want, err := json.Marshal(catalog.Entries)
	if err != nil {
		t.Fatalf("marshal catalog.Entries: %v", err)
	}

	// Compare decoded values, not raw bytes: the handler and this test both
	// call encoding/json.Marshal on the same []capabilitycatalog.Entry value,
	// so byte order is already guaranteed identical, but decoding first makes
	// a future encoder change (e.g. indentation) fail on content, not on
	// incidental whitespace.
	var gotEntries, wantEntries []map[string]any
	if err := json.Unmarshal(envelope.Data.Capabilities, &gotEntries); err != nil {
		t.Fatalf("decode response capabilities: %v", err)
	}
	if err := json.Unmarshal(want, &wantEntries); err != nil {
		t.Fatalf("decode expected capabilities: %v", err)
	}
	if !reflect.DeepEqual(gotEntries, wantEntries) {
		t.Fatalf("view=full capabilities diverged from capabilitycatalog.Entry's own serialization")
	}
	if !bytes.Equal(envelope.Data.Capabilities, want) {
		t.Fatal("view=full capabilities bytes diverged from json.Marshal(catalog.Entries) bytes")
	}
}

// TestCapabilitiesHandlerCompactFieldsMatchFullEntry proves every field the
// compact view keeps has the same value as the corresponding full-view
// (capabilitycatalog.Entry) field, for every catalog entry -- the compact
// projection must be a value-preserving subset, not a lossy rename (#6795
// review finding).
func TestCapabilitiesHandlerCompactFieldsMatchFullEntry(t *testing.T) {
	t.Parallel()

	catalog, err := capabilitycatalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	limit := len(catalog.Entries)

	compactEnvelope := capabilitiesRequest(t, "/api/v0/capabilities?limit="+strconv.Itoa(limit))
	compactData := compactEnvelope.Data.(map[string]any)
	compactEntries := compactData["capabilities"].([]any)
	if len(compactEntries) != len(catalog.Entries) {
		t.Fatalf("compact page = %d entries, want %d", len(compactEntries), len(catalog.Entries))
	}

	for i, raw := range compactEntries {
		entry := raw.(map[string]any)
		full := catalog.Entries[i]
		if got := entry["capability"].(string); got != full.Capability {
			t.Fatalf("entry %d capability = %q, want %q", i, got, full.Capability)
		}
		if got := entry["display_name"].(string); got != full.DisplayName {
			t.Fatalf("entry %d display_name = %q, want %q", i, got, full.DisplayName)
		}
		if got, _ := entry["owner_package"].(string); got != full.OwnerPackage {
			t.Fatalf("entry %d owner_package = %q, want %q", i, got, full.OwnerPackage)
		}
		if got := entry["maturity"].(string); got != string(full.Maturity) {
			t.Fatalf("entry %d maturity = %q, want %q", i, got, full.Maturity)
		}
		if got := len(entry["surfaces"].([]any)); got != len(full.Surfaces) {
			t.Fatalf("entry %d surfaces = %d, want %d", i, got, len(full.Surfaces))
		}
		if got := entry["console"].(bool); got != full.Console {
			t.Fatalf("entry %d console = %v, want %v", i, got, full.Console)
		}
		auth := entry["authorization"].(map[string]any)
		if got, _ := auth["family"].(string); got != full.Authorization.Family {
			t.Fatalf("entry %d authorization.family = %q, want %q", i, got, full.Authorization.Family)
		}
		if _, ok := entry["profiles"]; ok {
			t.Fatalf("entry %d compact view still carries profiles", i)
		}
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

// TestCapabilitiesHandlerNextOffsetIsNilWhenNotTruncated proves next_offset
// is present-but-null on the last page, not merely absent, so a caller can
// always rely on the key existing.
func TestCapabilitiesHandlerNextOffsetIsNilWhenNotTruncated(t *testing.T) {
	t.Parallel()

	catalog, err := capabilitycatalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	envelope := capabilitiesRequest(t, "/api/v0/capabilities?limit="+strconv.Itoa(len(catalog.Entries)))
	data := envelope.Data.(map[string]any)
	if truncated, _ := data["truncated"].(bool); truncated {
		t.Fatal("page truncated with limit == catalog size, want complete page")
	}
	if _, exists := data["next_offset"]; !exists {
		t.Fatal("next_offset key missing, want present (null)")
	}
	if data["next_offset"] != nil {
		t.Fatalf("next_offset = %#v, want nil", data["next_offset"])
	}
}
