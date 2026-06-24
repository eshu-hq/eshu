// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryPlaybookHandlerListsCatalogWithWorkflowPlanTruth(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{
		Playbooks: &QueryPlaybookHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/query-playbooks", nil)
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
	if envelope.Error != nil {
		t.Fatalf("envelope error = %+v, want nil", envelope.Error)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope is nil")
	}
	if got, want := envelope.Truth.Capability, CapabilityQueryPlaybooks; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, TruthBasisRuntimeState; got != want {
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
	if got, want := int(data["count"].(float64)), len(PlaybookCatalog()); got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}

func TestQueryPlaybookHandlerResolvesBoundedCallSequence(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{
		Playbooks: &QueryPlaybookHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	body := bytes.NewBufferString(`{"playbook_id":"service_story_citation","inputs":{"service_name":"payments-api","environment":"prod"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/query-playbooks/resolve", body)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Resolved ResolvedPlaybook `json:"resolved"`
		} `json:"data"`
		Truth *TruthEnvelope `json:"truth"`
		Error *ErrorEnvelope `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("envelope error = %+v, want nil", envelope.Error)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != CapabilityQueryPlaybooks {
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
	router := &APIRouter{
		Playbooks: &QueryPlaybookHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/query-playbooks/resolve", bytes.NewBufferString(`{"playbook_id":"missing","inputs":{}}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error == nil {
		t.Fatal("error = nil, want bounded not_found error")
	}
	if got, want := envelope.Error.Code, ErrorCodeNotFound; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := envelope.Error.Capability, CapabilityQueryPlaybooks; got != want {
		t.Fatalf("error capability = %q, want %q", got, want)
	}
}
