package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

func surfaceInventoryRequest(t *testing.T, target string) ResponseEnvelope {
	t.Helper()
	mux := http.NewServeMux()
	router := &APIRouter{SurfaceInventory: &SurfaceInventoryHandler{Profile: ProfileProduction}}
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

func TestSurfaceInventoryHandlerListsWithExactTruth(t *testing.T) {
	t.Parallel()

	envelope := surfaceInventoryRequest(t, "/api/v0/surface-inventory")
	if envelope.Error != nil {
		t.Fatalf("envelope error = %+v, want nil", envelope.Error)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope is nil")
	}
	if got, want := envelope.Truth.Capability, surfaceInventoryCapability; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Level, TruthLevelExact; got != want {
		t.Fatalf("truth level = %q, want %q", got, want)
	}

	data := envelope.Data.(map[string]any)
	inv, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		t.Fatalf("load surface inventory: %v", err)
	}
	if got, want := int(data["total"].(float64)), len(inv.Surfaces); got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
}

func TestSurfaceInventoryHandlerFiltersByCategoryAndReadiness(t *testing.T) {
	t.Parallel()

	envelope := surfaceInventoryRequest(t, "/api/v0/surface-inventory?category=collector&readiness=foundation_only")
	data := envelope.Data.(map[string]any)
	rows := data["surfaces"].([]any)
	if len(rows) == 0 {
		t.Fatal("expected at least one foundation_only collector (kubernetes_live)")
	}
	for _, raw := range rows {
		rec := raw.(map[string]any)
		if rec["category"].(string) != "collector" || rec["readiness"].(string) != "foundation_only" {
			t.Fatalf("filter leaked %v", rec)
		}
	}
}

func TestSurfaceInventoryHandlerRejectsBadLimit(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{SurfaceInventory: &SurfaceInventoryHandler{Profile: ProfileProduction}}
	router.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/surface-inventory?limit=99999", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestSurfaceInventoryReadinessParity is the API side of the #3148 parity proof:
// the embedded inventory the API serves contains at least one implemented, one
// gated, and one foundation-only collector, and the API returns each with the
// readiness lane the embedded artifact declares. The MCP side (that
// get_surface_inventory resolves to this same route) is proven in
// internal/mcp by TestSurfaceInventoryToolResolvesToAPIRoute, and the console
// loader reads the same route, so the three surfaces share one source of truth.
func TestSurfaceInventoryReadinessParity(t *testing.T) {
	t.Parallel()

	inv, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		t.Fatalf("load surface inventory: %v", err)
	}
	want := map[string]capabilitycatalog.ReadinessLane{}
	for _, lane := range []capabilitycatalog.ReadinessLane{
		capabilitycatalog.ReadinessImplemented,
		capabilitycatalog.ReadinessGated,
		capabilitycatalog.ReadinessFoundationOnly,
	} {
		for _, rec := range inv.Surfaces {
			if rec.Category == capabilitycatalog.SurfaceCollector && rec.Readiness == lane {
				want[rec.Name] = lane
				break
			}
		}
	}
	if len(want) != 3 {
		t.Fatalf("expected one implemented, gated, and foundation_only collector in the inventory; found %v", want)
	}

	envelope := surfaceInventoryRequest(t, "/api/v0/surface-inventory?category=collector&limit=1000")
	rows := envelope.Data.(map[string]any)["surfaces"].([]any)
	got := map[string]string{}
	for _, raw := range rows {
		rec := raw.(map[string]any)
		got[rec["name"].(string)] = rec["readiness"].(string)
	}
	for name, lane := range want {
		if got[name] != string(lane) {
			t.Fatalf("collector %q readiness over API = %q, want %q (inventory parity broken)", name, got[name], lane)
		}
	}
}
