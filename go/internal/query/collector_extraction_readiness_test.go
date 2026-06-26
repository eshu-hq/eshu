// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mountExtractionReadiness(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	(&CollectorExtractionReadinessHandler{Profile: ProfileProduction}).Mount(mux)
	return mux
}

// envelopeRequest builds a GET request that opts into the Eshu response envelope
// so the truth and error metadata are present in the body.
func envelopeRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	return req
}

type extractionReadinessEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Truth struct {
		Capability string `json:"capability"`
		Basis      string `json:"basis"`
		Level      string `json:"level"`
		Profile    string `json:"profile"`
	} `json:"truth"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func TestCollectorExtractionReadinessListReturnsCatalog(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	mountExtractionReadiness(t).ServeHTTP(rec, envelopeRequest("/api/v0/collector-extraction-readiness"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var env extractionReadinessEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, rec.Body.String())
	}
	if env.Error != nil {
		t.Fatalf("unexpected error envelope: %+v", env.Error)
	}
	if env.Truth.Capability != collectorExtractionReadinessListCapability {
		t.Fatalf("truth capability = %q, want %q", env.Truth.Capability, collectorExtractionReadinessListCapability)
	}
	var data CollectorExtractionReadinessListResponse
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.SchemaVersion != collectorExtractionReadinessSchemaVersion {
		t.Fatalf("schema version = %q, want %q", data.SchemaVersion, collectorExtractionReadinessSchemaVersion)
	}
	if data.Count == 0 || data.Count != data.TotalCount {
		t.Fatalf("count=%d total=%d, want equal and non-zero", data.Count, data.TotalCount)
	}
	if data.Truncated {
		t.Fatalf("truncated=true unexpectedly for default limit")
	}
	var sawGit, sawPagerDuty bool
	for _, family := range data.Families {
		if family.Family == "git" {
			sawGit = true
			if family.Classification != "keep_in_tree" {
				t.Errorf("git classification = %q, want keep_in_tree", family.Classification)
			}
		}
		if family.Family == "pagerduty" {
			sawPagerDuty = true
			if family.Classification != "extraction_candidate" {
				t.Errorf("pagerduty classification = %q, want extraction_candidate", family.Classification)
			}
		}
	}
	if !sawGit || !sawPagerDuty {
		t.Fatalf("catalog missing expected families: git=%t pagerduty=%t", sawGit, sawPagerDuty)
	}
}

func TestCollectorExtractionReadinessListHonorsLimit(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	mountExtractionReadiness(t).ServeHTTP(rec, envelopeRequest("/api/v0/collector-extraction-readiness?limit=2"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var env extractionReadinessEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	var data CollectorExtractionReadinessListResponse
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Count != 2 || !data.Truncated {
		t.Fatalf("count=%d truncated=%t, want count=2 truncated=true", data.Count, data.Truncated)
	}
	if data.TotalCount <= 2 {
		t.Fatalf("total_count=%d, want > 2", data.TotalCount)
	}
}

func TestCollectorExtractionReadinessListRejectsBadLimit(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	mountExtractionReadiness(t).ServeHTTP(rec, envelopeRequest("/api/v0/collector-extraction-readiness?limit=0"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env extractionReadinessEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error == nil || env.Error.Code != string(ErrorCodeInvalidArgument) {
		t.Fatalf("error = %+v, want invalid_argument", env.Error)
	}
}

func TestCollectorExtractionReadinessFamilyDrilldown(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	mountExtractionReadiness(t).ServeHTTP(rec, envelopeRequest("/api/v0/collector-extraction-readiness/pagerduty"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var env extractionReadinessEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	var data CollectorExtractionReadinessFamilyResponse
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Family.Family != "pagerduty" || data.Family.Classification != "extraction_candidate" {
		t.Fatalf("family = %+v, want pagerduty extraction_candidate", data.Family)
	}
	if len(data.Family.Criteria) != 7 {
		t.Fatalf("criteria = %d, want 7", len(data.Family.Criteria))
	}
}

func TestCollectorExtractionReadinessFamily_LocalLightweightReturnsFamilyData(t *testing.T) {
	t.Parallel()
	handler := &CollectorExtractionReadinessHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/collector-extraction-readiness/pagerduty", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	handler.Mount(mux)
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var env extractionReadinessEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal envelope: %v", err)
	}
	if got, want := string(env.Truth.Level), string(TruthLevelExact); got != want {
		t.Fatalf("truth level = %s, want %s", got, want)
	}
	if got, want := string(env.Truth.Profile), string(ProfileLocalLightweight); got != want {
		t.Fatalf("truth profile = %s, want %s", got, want)
	}

	var data CollectorExtractionReadinessFamilyResponse
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Family.Family != "pagerduty" {
		t.Fatalf("family = %s, want pagerduty", data.Family.Family)
	}
}

func TestCollectorExtractionReadinessFamilyNotFound(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	mountExtractionReadiness(t).ServeHTTP(rec, envelopeRequest("/api/v0/collector-extraction-readiness/not_a_collector"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var env extractionReadinessEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error == nil || env.Error.Code != string(ErrorCodeNotFound) {
		t.Fatalf("error = %+v, want not_found", env.Error)
	}
}
