// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	cloudpaths "github.com/eshu-hq/eshu/go/internal/query/openapi/paths/cloud"
	evidencepaths "github.com/eshu-hq/eshu/go/internal/query/openapi/paths/evidence"
)

// cloudRuntimeDriftPinnedRoutes is the exact route surface the #9669 W8b
// pin covers: the provider-neutral drift readback plus the drift
// investigation packet, both mounted by CloudRuntimeDriftHandler.Mount.
// The handler tests below and the spec-fragment parity test both range
// over this list, so a route added without pin review fails the seal
// elsewhere and a route dropped breaks every subtest here.
var cloudRuntimeDriftPinnedRoutes = []string{
	"POST /api/v0/cloud/runtime-drift/findings",
	"GET /api/v0/investigations/drift/packet",
}

// cloudRuntimeDriftPinDouble is the pin-satisfying double behind the
// route tests: it implements exactly the two pinned reads, recording
// every call. The compile-time assertions below prove it satisfies the
// pin and the production store slot at once, so serving every route
// through it proves the frozen shape — nothing wider — is enough to
// answer the whole drift surface.
type cloudRuntimeDriftPinDouble struct {
	calls []string
	rows  []MultiCloudRuntimeDriftFindingRow
	total int
}

func (d *cloudRuntimeDriftPinDouble) ListActiveMultiCloudRuntimeDriftFindings(
	_ context.Context,
	_ MultiCloudRuntimeDriftFilter,
) ([]MultiCloudRuntimeDriftFindingRow, error) {
	d.calls = append(d.calls, "ListActiveMultiCloudRuntimeDriftFindings")
	return d.rows, nil
}

func (d *cloudRuntimeDriftPinDouble) CountActiveMultiCloudRuntimeDriftFindings(
	_ context.Context,
	_ MultiCloudRuntimeDriftFilter,
) (int, error) {
	d.calls = append(d.calls, "CountActiveMultiCloudRuntimeDriftFindings")
	if d.total != 0 {
		return d.total, nil
	}
	return len(d.rows), nil
}

var (
	_ CloudRuntimeDriftReadAdmission = (*cloudRuntimeDriftPinDouble)(nil)
	_ MultiCloudRuntimeDriftStore    = (*cloudRuntimeDriftPinDouble)(nil)
)

// cloudRuntimeDriftServe mounts a drift handler over the pin double and
// serves one request through the real mux, so the tests below prove the
// registered routes, not direct method calls.
func cloudRuntimeDriftServe(t *testing.T, store MultiCloudRuntimeDriftStore, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&CloudRuntimeDriftHandler{Store: store, Profile: ProfileLocalAuthoritative}).Mount(mux)
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, bytes.NewBufferString(body))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestCloudRuntimeDriftReadAdmissionRoutesServeDrift proves the two
// pinned routes serve their reads through the pin-satisfying double:
// the findings readback answers HTTP 200 through the real mux and
// records exactly the count-plus-list pair its leg admits through, and
// the packet leg answers HTTP 200 recording exactly the list read — the
// executable form of the route-by-method census, and the route-level OUT
// proof: no served route issues any call beyond the two pinned methods.
func TestCloudRuntimeDriftReadAdmissionRoutesServeDrift(t *testing.T) {
	t.Parallel()

	t.Run("findings readback admits through count plus list", func(t *testing.T) {
		t.Parallel()
		store := &cloudRuntimeDriftPinDouble{rows: multiCloudRuntimeDriftFixtureRows(), total: 5}
		rec := cloudRuntimeDriftServe(t, store, http.MethodPost, "/api/v0/cloud/runtime-drift/findings",
			`{"scope_id":"cloud-scope:gcp:project-synthetic","limit":10}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if want := []string{"CountActiveMultiCloudRuntimeDriftFindings", "ListActiveMultiCloudRuntimeDriftFindings"}; !slices.Equal(store.calls, want) {
			t.Fatalf("calls = %v, want %v: the findings leg must admit through the count-plus-list pair only", store.calls, want)
		}
		if !strings.Contains(rec.Body.String(), "cloud-scope:gcp:project-synthetic") {
			t.Fatalf("body = %s, want the fixture scope echoed in the readback", rec.Body.String())
		}
	})

	t.Run("drift packet admits through the list read only", func(t *testing.T) {
		t.Parallel()
		store := &cloudRuntimeDriftPinDouble{rows: multiCloudRuntimeDriftFixtureRows()}
		rec := cloudRuntimeDriftServe(t, store, http.MethodGet,
			"/api/v0/investigations/drift/packet?scope_id=cloud-scope:gcp:project-synthetic", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if want := []string{"ListActiveMultiCloudRuntimeDriftFindings"}; !slices.Equal(store.calls, want) {
			t.Fatalf("calls = %v, want %v: the packet leg must admit through the list read only, never the count", store.calls, want)
		}
	})
}

// TestCloudRuntimeDriftReadAdmissionSpecFragmentParity proves the pin's
// route list is the spec's route list and the mux's route list: every
// pinned route path appears as a fragment in the published OpenAPI path
// fragments, and the mounted handler serves every pinned route (a
// well-formed request reaches the handler's 200, where an unregistered
// path would fall through to the mux 404).
func TestCloudRuntimeDriftReadAdmissionSpecFragmentParity(t *testing.T) {
	t.Parallel()

	fragments := map[string]string{
		"/api/v0/cloud/runtime-drift/findings": cloudpaths.RuntimeDrift,
		"/api/v0/investigations/drift/packet":  evidencepaths.Investigations,
	}
	for _, route := range cloudRuntimeDriftPinnedRoutes {
		route := route
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			_, path, _ := strings.Cut(route, " ")
			fragment, ok := fragments[path]
			if !ok {
				t.Fatalf("no OpenAPI fragment registered for %q: the pin, the spec, and the mux must name the same routes", path)
			}
			if _, _, found := strings.Cut(fragment, `"`+path+`":`); !found {
				t.Errorf("OpenAPI fragment has no %q entry: the pin, the spec, and the mux must name the same routes", path)
			}
		})
	}

	mux := http.NewServeMux()
	(&CloudRuntimeDriftHandler{Store: &cloudRuntimeDriftPinDouble{rows: multiCloudRuntimeDriftFixtureRows()}, Profile: ProfileLocalAuthoritative}).Mount(mux)
	served := map[string]struct {
		method string
		target string
		body   string
	}{
		"POST /api/v0/cloud/runtime-drift/findings": {http.MethodPost, "/api/v0/cloud/runtime-drift/findings", `{"scope_id":"cloud-scope:gcp:project-synthetic"}`},
		"GET /api/v0/investigations/drift/packet":   {http.MethodGet, "/api/v0/investigations/drift/packet?scope_id=cloud-scope:gcp:project-synthetic", ""},
	}
	for _, route := range cloudRuntimeDriftPinnedRoutes {
		reqSpec := served[route]
		var req *http.Request
		if reqSpec.body == "" {
			req = httptest.NewRequest(reqSpec.method, reqSpec.target, nil)
		} else {
			req = httptest.NewRequest(reqSpec.method, reqSpec.target, bytes.NewBufferString(reqSpec.body))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want %d: the route must be registered on the mux", route, rec.Code, http.StatusOK)
		}
	}
}
