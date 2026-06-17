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

	envelope := capabilitiesRequest(t, "/api/v0/capabilities")
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
	first := capabilities[0].(map[string]any)
	if first["capability"].(string) != catalog.Entries[0].Capability {
		t.Fatalf("first capability = %q, want %q", first["capability"], catalog.Entries[0].Capability)
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
