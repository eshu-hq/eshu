// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/extraction"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// cannedExtractionReadiness is a hermetic ExtractionReadinessReadAdmission:
// two families served verbatim so the route tests below prove the legs
// admit through the pin, not the extraction static catalog.
type cannedExtractionReadiness struct {
	rows map[scope.CollectorKind]extraction.Readiness
}

func (c *cannedExtractionReadiness) Catalog() []extraction.Readiness {
	out := make([]extraction.Readiness, 0, len(c.rows))
	for _, row := range c.rows {
		out = append(out, row)
	}
	return out
}

func (c *cannedExtractionReadiness) Lookup(family scope.CollectorKind) (extraction.Readiness, bool) {
	row, ok := c.rows[family]
	return row, ok
}

// cannedFactSchema is a hermetic FactSchemaReadAdmission serving one
// fact kind so the route tests prove the legs admit through the pin,
// not the facts static registry.
type cannedFactSchema struct{}

func (cannedFactSchema) SupportedSchemaVersions() map[string]string {
	return map[string]string{"canned-fact": "v9"}
}

func (cannedFactSchema) SchemaVersion(factKind string) (string, bool) {
	if factKind == "canned-fact" {
		return "v9", true
	}
	return "", false
}

func (cannedFactSchema) ClassifySchemaVersion(factKind, candidate string) facts.Compatibility {
	if factKind == "canned-fact" && candidate == "v9" {
		return facts.CompatibilitySupported
	}
	return facts.CompatibilityUnsupportedMajor
}

// errSurfaceInventory is a hermetic SurfaceInventoryReadAdmission that
// answers every load with a failure, proving the list leg fails closed
// through the pin instead of serving a silent empty inventory.
type errSurfaceInventory struct{ err error }

func (e errSurfaceInventory) LoadSurfaceInventory() (capabilitycatalog.SurfaceInventory, error) {
	return capabilitycatalog.SurfaceInventory{}, e.err
}

// TestCollectorExtractionReadinessServesThroughPin proves the two
// extraction-readiness legs admit through the G2
// ExtractionReadinessReadAdmission pin: an injected admission drives
// both routes, untracked families still 404, and a handler built
// without an admission keeps serving the real static catalog (the cmd
// wiring sets only Profile).
func TestCollectorExtractionReadinessServesThroughPin(t *testing.T) {
	t.Parallel()

	canned := &cannedExtractionReadiness{rows: map[scope.CollectorKind]extraction.Readiness{
		scope.CollectorGit: {Family: scope.CollectorGit, DisplayName: "Canned Git"},
	}}
	mux := http.NewServeMux()
	(&CollectorExtractionReadinessHandler{Profile: ProfileProduction, Reads: canned}).Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, envelopeRequest("/api/v0/collector-extraction-readiness"))
	if rec.Code != http.StatusOK {
		t.Fatalf("canned list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var env extractionReadinessEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var data CollectorExtractionReadinessListResponse
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.TotalCount != 1 || data.Families[0].DisplayName != "Canned Git" {
		t.Fatalf("canned list = %+v, want the one canned family", data)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, envelopeRequest("/api/v0/collector-extraction-readiness/git"))
	if rec.Code != http.StatusOK {
		t.Fatalf("canned family status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, envelopeRequest("/api/v0/collector-extraction-readiness/not-a-family"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("untracked family status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	mountExtractionReadiness(t).ServeHTTP(rec, envelopeRequest("/api/v0/collector-extraction-readiness"))
	if rec.Code != http.StatusOK {
		t.Fatalf("default list status = %d, want 200; the nil-Reads default must keep serving", rec.Code)
	}
}

// TestFactSchemaVersionServesThroughPin proves the three
// fact-schema-version legs admit through the G2 FactSchemaReadAdmission
// pin: an injected admission drives list, detail, and classification,
// unknown kinds still 404, and a handler built without an admission
// keeps serving the real static registry.
func TestFactSchemaVersionServesThroughPin(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	(&FactSchemaVersionHandler{Profile: ProfileProduction, Reads: cannedFactSchema{}}).Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, envelopeRequest("/api/v0/fact-schema-versions"))
	if rec.Code != http.StatusOK {
		t.Fatalf("canned list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data FactSchemaVersionListResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.TotalCount != 1 || env.Data.FactSchemaVersions[0].FactKind != "canned-fact" {
		t.Fatalf("canned list = %+v, want the one canned kind", env.Data)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, envelopeRequest("/api/v0/fact-schema-versions/canned-fact?candidate=v9"))
	if rec.Code != http.StatusOK {
		t.Fatalf("canned detail status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Data FactSchemaVersionDetailResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.Data.SupportedVersion != "v9" || detail.Data.Compatibility != string(facts.CompatibilitySupported) {
		t.Fatalf("canned detail = %+v, want v9/compatible", detail.Data)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, envelopeRequest("/api/v0/fact-schema-versions/unknown-kind"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown kind status = %d, want 404", rec.Code)
	}
}

// TestSurfaceInventoryFailsClosedThroughPin proves the surface-inventory
// leg admits through the G2 SurfaceInventoryReadAdmission pin: an
// injected failing admission surfaces the fixed 500 instead of a silent
// empty inventory. The nil-Reads default keeps serving the real
// embedded artifact (covered by the standing list tests).
func TestSurfaceInventoryFailsClosedThroughPin(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	(&SurfaceInventoryHandler{Profile: ProfileProduction, Reads: errSurfaceInventory{err: errors.New("surface inventory probe: store unavailable")}}).Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/surface-inventory", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failing inventory status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}
