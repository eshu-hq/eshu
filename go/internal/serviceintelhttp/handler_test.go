// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintelhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// emptyGraph resolves no workloads, so every selector is a not-found.
type emptyGraph struct{}

func (emptyGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (emptyGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func reportResponse(t *testing.T, h *ReportHandler, service string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/"+service+"/intelligence-report", nil)
	req.Header.Set("Accept", query.EnvelopeMIMEType)
	req.SetPathValue("service_name", service)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestReportHandlerNilEntitiesIsUnavailable(t *testing.T) {
	t.Parallel()
	rec := reportResponse(t, &ReportHandler{}, "checkout")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReportHandlerMissingServiceReturnsNotFound(t *testing.T) {
	t.Parallel()
	h := &ReportHandler{Entities: &query.EntityHandler{Neo4j: emptyGraph{}, Profile: query.ProfileProduction}}
	rec := reportResponse(t, h, "missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != query.ErrorCodeNotFound {
		t.Fatalf("error = %#v, want not_found", envelope.Error)
	}
}

func TestReportHandlerUnsupportedCapability(t *testing.T) {
	t.Parallel()
	// Local lightweight does not support platform context truth.
	h := &ReportHandler{Entities: &query.EntityHandler{Neo4j: emptyGraph{}, Profile: query.ProfileLocalLightweight}}
	rec := reportResponse(t, h, "checkout")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body=%s", rec.Code, rec.Body.String())
	}
	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != query.ErrorCodeUnsupportedCapability {
		t.Fatalf("error = %#v, want unsupported_capability", envelope.Error)
	}
}
