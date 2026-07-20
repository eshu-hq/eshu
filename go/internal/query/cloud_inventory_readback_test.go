// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cloudInventoryReadbackPayloadRow returns one canonical
// reducer_cloud_resource_identity fact payload wrapped in the envelope shape the
// readback SQL projects (a top-level payload column holding the fact metadata
// plus the canonical payload object).
func cloudInventoryReadbackPayloadRow(t *testing.T) []byte {
	t.Helper()
	row := map[string]any{
		"fact_id":       "reducer_cloud_resource_identity:abc",
		"fact_kind":     "reducer_cloud_resource_identity",
		"scope_id":      "cloud-scope:gcp:project-synthetic",
		"generation_id": "gcp-gen-1",
		"source_system": "gcp_cloud_inventory",
		"observed_at":   "2026-06-09T00:00:00Z",
		"payload": map[string]any{
			"cloud_resource_uid":    "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1",
			"provider":              "gcp",
			"resource_type":         "compute.googleapis.com/Instance",
			"management_origin":     "declared",
			"has_declared_evidence": true,
			"has_applied_evidence":  true,
			"has_observed_evidence": false,
			"scope_id":              "cloud-scope:gcp:project-synthetic",
			"raw_identity":          "//compute.googleapis.com/projects/project-synthetic/zones/us/instances/vm-1",
		},
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal cloud inventory readback row: %v", err)
	}
	return encoded
}

func TestCloudInventoryHandlerListsCanonicalIdentities(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows:    [][]driver.Value{{cloudInventoryReadbackPayloadRow(t)}},
		queryContains: []string{
			"FROM fact_records",
			"fact_records.fact_kind = 'reducer_cloud_resource_identity'",
			"fact_records.is_tombstone = FALSE",
			"fact_records.payload->>'provider' = $1",
			"fact_records.payload->>'management_origin' = $2",
		},
	}})
	handler := &CloudInventoryHandler{
		Content: NewContentReader(db),
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/cloud/inventory?provider=gcp&management_origin=declared&limit=1",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	resources := data["resources"].([]any)
	if got, want := len(resources), 1; got != want {
		t.Fatalf("len(resources) = %d, want %d", got, want)
	}
	resource := resources[0].(map[string]any)
	if got, want := resource["provider"], "gcp"; got != want {
		t.Fatalf("provider = %#v, want %#v", got, want)
	}
	if got, want := resource["management_origin"], "declared"; got != want {
		t.Fatalf("management_origin = %#v, want %#v", got, want)
	}
	if got, want := resource["source_state"], string(ReplatformingSourceStateExact); got != want {
		t.Fatalf("source_state = %#v, want %#v", got, want)
	}
	evidence := resource["evidence"].(map[string]any)
	if got, want := evidence["declared"], true; got != want {
		t.Fatalf("evidence.declared = %#v, want %#v", got, want)
	}
	if got, want := evidence["observed"], false; got != want {
		t.Fatalf("evidence.observed = %#v, want %#v", got, want)
	}
	// Raw provider locators must never leak through the readback projection.
	if _, present := resource["raw_identity"]; present {
		t.Fatalf("raw_identity must not appear in the readback payload: %#v", resource)
	}
	if got, want := resp.Truth.Capability, cloudInventoryReadbackCapability; got != want {
		t.Fatalf("truth.capability = %#v, want %#v", got, want)
	}
	if got, want := resp.Truth.Basis, TruthBasisSemanticFacts; got != want {
		t.Fatalf("truth.basis = %#v, want %#v", got, want)
	}
}

// TestCloudInventoryHandlerScopedEmptyGrantReturnsEmptyWithoutQuery is the
// #5167 counterpart to the admission-decision/kubernetes/observability-coverage
// empty-grant precedents: a scoped caller with no granted repository or
// ingestion scope must never reach Postgres.
func TestCloudInventoryHandlerScopedEmptyGrantReturnsEmptyWithoutQuery(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: []string{"payload"}, rows: [][]driver.Value{{cloudInventoryReadbackPayloadRow(t)}}},
	})
	handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if len(recorder.queries) != 0 {
		t.Fatalf("Postgres received %d queries, want 0 for an empty-grant scoped caller", len(recorder.queries))
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got, want := len(data["resources"].([]any)), 0; got != want {
		t.Fatalf("len(resources) = %d, want %d", got, want)
	}
}

// TestCloudInventoryHandlerScopedGrantHitsRealStoreAndReturnsRowData proves
// the #5167 fix against the ACTUAL production backend (ContentReader over a
// real *sql.DB, the same type cmd/api/wiring_handlers.go constructs): a
// scoped caller with a matching grant reaches Postgres, the dispatched SQL
// carries the access-scoping predicate with the caller's granted ids bound as
// args, and the response surfaces the real row data the fake driver
// returned -- not just a 200/shape check.
func TestCloudInventoryHandlerScopedGrantHitsRealStoreAndReturnsRowData(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: []string{"payload"}, rows: [][]driver.Value{{cloudInventoryReadbackPayloadRow(t)}}},
	})
	handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?provider=gcp", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		AllowedScopeIDs: []string{"cloud-scope:gcp:project-synthetic"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("Postgres received %d queries, want exactly %d", got, want)
	}
	dispatched := recorder.queries[0]
	if !strings.Contains(dispatched, "fact_records.scope_id = ANY(") {
		t.Fatalf("dispatched query missing #5167 access-scoping predicate:\n%s", dispatched)
	}
	found := false
	for _, arg := range recorder.args[0] {
		if s := fmt.Sprintf("%v", arg); strings.Contains(s, "cloud-scope:gcp:project-synthetic") {
			found = true
		}
	}
	if !found {
		t.Fatalf("bound args %#v did not include the granted scope id", recorder.args[0])
	}

	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	resources := data["resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("len(resources) = %d, want 1; body = %s", len(resources), w.Body.String())
	}
	resource := resources[0].(map[string]any)
	if got, want := resource["scope_id"], "cloud-scope:gcp:project-synthetic"; got != want {
		t.Fatalf("scope_id = %#v, want %#v (real row data from the fake driver)", got, want)
	}
	if got, want := resource["provider"], "gcp"; got != want {
		t.Fatalf("provider = %#v, want %#v (real row data from the fake driver)", got, want)
	}
}

// TestCloudInventoryHandlerUnscopedQueryStaysUnfiltered is the no-regression
// counterpart: a shared/admin caller (no AuthContext) must still issue the
// byte-identical unscoped query with no access-scoping predicate.
func TestCloudInventoryHandlerUnscopedQueryStaysUnfiltered(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: []string{"payload"}, rows: [][]driver.Value{{cloudInventoryReadbackPayloadRow(t)}}},
	})
	handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?provider=gcp", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("Postgres received %d queries, want exactly %d", got, want)
	}
	if strings.Contains(recorder.queries[0], "fact_records.scope_id = ANY(") {
		t.Fatalf("unscoped/admin query must stay unfiltered, got:\n%s", recorder.queries[0])
	}
}

func TestCloudInventoryReadbackSelectsOnlyActiveScopeGenerations(t *testing.T) {
	t.Parallel()

	query, _ := buildCloudInventoryIdentitiesSQL(cloudInventoryFilter{Limit: 50})
	for _, required := range []string{
		"JOIN ingestion_scopes AS scope",
		"scope.active_generation_id = fact_records.generation_id",
		"JOIN scope_generations AS generation",
		"generation.status = 'active'",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("cloud inventory query missing active-generation guard %q:\n%s", required, query)
		}
	}
}

func TestCloudInventoryHandlerRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	handler := &CloudInventoryHandler{
		Content: NewContentReader(openContentReaderTestDB(t, nil)),
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?provider=oracle", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestCloudInventoryHandlerRejectsUnknownManagementOrigin(t *testing.T) {
	t.Parallel()

	handler := &CloudInventoryHandler{
		Content: NewContentReader(openContentReaderTestDB(t, nil)),
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?management_origin=guessed", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestCloudInventoryHandlerUnsupportedProfile(t *testing.T) {
	t.Parallel()

	handler := &CloudInventoryHandler{
		Content: NewContentReader(openContentReaderTestDB(t, nil)),
		Profile: ProfileLocalLightweight,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected error envelope, got %s", w.Body.String())
	}
	if got, want := resp.Error.Code, ErrorCodeUnsupportedCapability; got != want {
		t.Fatalf("error.code = %#v, want %#v", got, want)
	}
}
