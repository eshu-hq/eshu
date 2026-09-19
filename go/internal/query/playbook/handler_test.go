// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestQueryPlaybookHandlerListsCatalogWithWorkflowPlanTruth(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	handler := &Handler{Profile: querycontract.ProfileProduction}
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/query-playbooks", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("envelope error = %+v, want nil", envelope.Error)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope is nil")
	}
	if got, want := envelope.Truth.Capability, Capability; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, querycontract.TruthBasisRuntimeState; got != want {
		t.Fatalf("truth basis = %q, want %q", got, want)
	}

	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map", envelope.Data)
	}
	playbooks, ok := data["playbooks"].([]any)
	if !ok || len(playbooks) == 0 {
		t.Fatalf("playbooks = %#v, want non-empty list", data["playbooks"])
	}
	if got, want := int(data["count"].(float64)), len(Catalog()); got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}

// TestQueryPlaybookHandlerDefaultViewIsCompactAndBounded proves the default
// list response is the compact Summary shape (no steps/required_inputs/
// failure_modes) and fits an MCP-client-friendly response budget (#6795).
func TestQueryPlaybookHandlerDefaultViewIsCompactAndBounded(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	handler := &Handler{Profile: querycontract.ProfileProduction}
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/query-playbooks", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	const budget = 8 * 1024
	if got := rec.Body.Len(); got >= budget {
		t.Fatalf("default /api/v0/query-playbooks body = %d bytes, want < %d", got, budget)
	}

	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data := envelope.Data.(map[string]any)
	playbooks, ok := data["playbooks"].([]any)
	if !ok || len(playbooks) == 0 {
		t.Fatalf("playbooks = %#v, want non-empty list", data["playbooks"])
	}
	for _, raw := range playbooks {
		entry := raw.(map[string]any)
		if _, ok := entry["steps"]; ok {
			t.Fatalf("compact playbook %q carries steps, want omitted", entry["id"])
		}
		if _, ok := entry["required_inputs"]; ok {
			t.Fatalf("compact playbook %q carries required_inputs, want omitted", entry["id"])
		}
		if _, ok := entry["failure_modes"]; ok {
			t.Fatalf("compact playbook %q carries failure_modes, want omitted", entry["id"])
		}
		if entry["id"].(string) == "" {
			t.Fatal("compact playbook missing id")
		}
	}
	if got, want := int(data["total"].(float64)), len(Catalog()); got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
}

// TestQueryPlaybookHandlerViewFullReturnsCompleteDefinitions proves view=full
// restores the pre-#6795 shape (steps, required_inputs, failure_modes).
func TestQueryPlaybookHandlerViewFullReturnsCompleteDefinitions(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	handler := &Handler{Profile: querycontract.ProfileProduction}
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/query-playbooks?view=full&limit=1", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data := envelope.Data.(map[string]any)
	playbooks := data["playbooks"].([]any)
	if len(playbooks) != 1 {
		t.Fatalf("playbooks = %#v, want 1 (limit=1)", playbooks)
	}
	entry := playbooks[0].(map[string]any)
	if _, ok := entry["steps"]; !ok {
		t.Fatal("view=full playbook missing steps")
	}
	if truncated, ok := data["truncated"].(bool); !ok || !truncated {
		t.Fatalf("truncated = %v, want true (limit=1 < catalog size)", data["truncated"])
	}
	if got, ok := data["next_offset"].(float64); !ok || int(got) != 1 {
		t.Fatalf("next_offset = %#v, want 1", data["next_offset"])
	}
}

// TestQueryPlaybookHandlerFullViewIsByteIdenticalToDefinition proves
// view=full's per-playbook JSON is exactly Definition's own serialization --
// not a hand-copied projection that could silently drop a field Definition
// gains later (#6795 review finding).
func TestQueryPlaybookHandlerFullViewIsByteIdenticalToDefinition(t *testing.T) {
	t.Parallel()

	catalog := Catalog()
	mux := http.NewServeMux()
	handler := &Handler{Profile: querycontract.ProfileProduction}
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v0/query-playbooks?view=full&limit=%d", len(catalog)), nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var envelope struct {
		Data struct {
			Playbooks json.RawMessage `json:"playbooks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	var gotPlaybooks, wantPlaybooks []map[string]any
	if err := json.Unmarshal(envelope.Data.Playbooks, &gotPlaybooks); err != nil {
		t.Fatalf("decode response playbooks: %v", err)
	}
	if err := json.Unmarshal(want, &wantPlaybooks); err != nil {
		t.Fatalf("decode expected playbooks: %v", err)
	}
	if !reflect.DeepEqual(gotPlaybooks, wantPlaybooks) {
		t.Fatal("view=full playbooks diverged from Definition's own serialization")
	}
}

// TestQueryPlaybookHandlerBeforeAfterPayloadSize measures the pre-#6795
// default payload (view=full, unbounded) against the post-#6795 default and
// logs both, proving the reduction is real and measured (#6795).
func TestQueryPlaybookHandlerBeforeAfterPayloadSize(t *testing.T) {
	t.Parallel()

	catalog := Catalog()
	mux := http.NewServeMux()
	handler := &Handler{Profile: querycontract.ProfileProduction}
	handler.Mount(mux)

	before := playbookRawBody(t, mux, fmt.Sprintf("/api/v0/query-playbooks?view=full&limit=%d", len(catalog)))
	after := playbookRawBody(t, mux, "/api/v0/query-playbooks")
	t.Logf("list_query_playbooks default payload: before(#6795 shape, view=full, all %d playbooks)=%d bytes, after(compact default, limit=%d)=%d bytes",
		len(catalog), len(before), defaultListLimit, len(after))
	if len(after) >= len(before) {
		t.Fatalf("after size %d bytes not smaller than before size %d bytes", len(after), len(before))
	}
	const budget = 8 * 1024
	if len(after) >= budget {
		t.Fatalf("after size %d bytes, want < %d", len(after), budget)
	}
}

func playbookRawBody(t *testing.T, mux http.Handler, target string) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

// TestQueryPlaybookHandlerPagesDeterministically proves offset paging returns
// the catalog entries in stable order.
func TestQueryPlaybookHandlerPagesDeterministically(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	handler := &Handler{Profile: querycontract.ProfileProduction}
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/query-playbooks?limit=1&offset=1&view=full", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data := envelope.Data.(map[string]any)
	playbooks := data["playbooks"].([]any)
	entry := playbooks[0].(map[string]any)
	if got, want := entry["id"].(string), Catalog()[1].ID; got != want {
		t.Fatalf("offset paging mismatch: got %q, want %q", got, want)
	}
}

// TestQueryPlaybookHandlerRejectsBadView proves an unrecognized view value is
// a bounded 400.
func TestQueryPlaybookHandlerRejectsBadView(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	handler := &Handler{Profile: querycontract.ProfileProduction}
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/query-playbooks?view=verbose", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestQueryPlaybookHandlerResolvesBoundedCallSequence(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	handler := &Handler{Profile: querycontract.ProfileProduction}
	handler.Mount(mux)

	body := bytes.NewBufferString(`{"playbook_id":"service_story_citation","inputs":{"service_name":"payments-api","environment":"prod"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/query-playbooks/resolve", body)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Resolved ResolvedPlaybook `json:"resolved"`
		} `json:"data"`
		Truth *querycontract.TruthEnvelope `json:"truth"`
		Error *querycontract.ErrorEnvelope `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("envelope error = %+v, want nil", envelope.Error)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != Capability {
		t.Fatalf("truth = %+v, want query playbook capability", envelope.Truth)
	}
	if got, want := envelope.Data.Resolved.PlaybookID, "service_story_citation"; got != want {
		t.Fatalf("resolved.playbook_id = %q, want %q", got, want)
	}
	if len(envelope.Data.Resolved.Calls) == 0 {
		t.Fatal("resolved calls is empty")
	}
	if got, want := envelope.Data.Resolved.Calls[0].Arguments["workload_id"], "payments-api"; got != want {
		t.Fatalf("first call workload_id = %#v, want %#v", got, want)
	}
}

func TestQueryPlaybookHandlerRejectsUnknownPlaybookWithBoundedError(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	handler := &Handler{Profile: querycontract.ProfileProduction}
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/query-playbooks/resolve", bytes.NewBufferString(`{"playbook_id":"missing","inputs":{}}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error == nil {
		t.Fatal("error = nil, want bounded not_found error")
	}
	if got, want := envelope.Error.Code, querycontract.ErrorCodeNotFound; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := envelope.Error.Capability, Capability; got != want {
		t.Fatalf("error capability = %q, want %q", got, want)
	}
}
