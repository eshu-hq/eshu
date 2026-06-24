// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
)

type recordingGenerationLifecycleReader struct {
	page       status.GenerationLifecyclePage
	err        error
	lastFilter status.GenerationLifecycleFilter
}

func (r *recordingGenerationLifecycleReader) ListGenerationLifecycle(
	_ context.Context,
	filter status.GenerationLifecycleFilter,
) (status.GenerationLifecyclePage, error) {
	r.lastFilter = filter
	if r.err != nil {
		return status.GenerationLifecyclePage{}, r.err
	}
	return r.page, nil
}

func newFreshnessMux(reader GenerationLifecycleReader) *http.ServeMux {
	handler := &FreshnessHandler{Generations: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func doFreshnessRequest(t *testing.T, mux *http.ServeMux, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func decodeFreshnessEnvelope(t *testing.T, w *httptest.ResponseRecorder) ResponseEnvelope {
	t.Helper()
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v; body = %s", err, w.Body.String())
	}
	return envelope
}

func TestFreshnessGenerationLifecycleActive(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerationLifecycleReader{page: status.GenerationLifecyclePage{
		Records: []status.GenerationLifecycleRecord{{
			ScopeID:                   "git-repository-scope:acme/app",
			GenerationID:              "gen-active",
			ScopeKind:                 "repository",
			SourceSystem:              "github",
			CollectorKind:             "git",
			CurrentActiveGenerationID: "gen-active",
			IsActive:                  true,
			Status:                    "active",
			QueueStatus:               status.GenerationQueueStatus{Total: 3, Succeeded: 3},
		}},
		Limit: 50,
	}}
	mux := newFreshnessMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/generations?scope_id=git-repository-scope:acme/app")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Truth == nil || envelope.Truth.Freshness.State != FreshnessFresh {
		t.Fatalf("expected fresh truth, got %+v", envelope.Truth)
	}
	data, _ := envelope.Data.(map[string]any)
	if data["count"].(float64) != 1 || data["truncated"].(bool) {
		t.Fatalf("unexpected data: %+v", data)
	}
	if reader.lastFilter.ScopeID != "git-repository-scope:acme/app" {
		t.Fatalf("filter scope_id = %q", reader.lastFilter.ScopeID)
	}
}

func TestFreshnessGenerationLifecyclePendingMarksBuilding(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerationLifecycleReader{page: status.GenerationLifecyclePage{
		Records: []status.GenerationLifecycleRecord{{
			ScopeID:      "git-repository-scope:acme/app",
			GenerationID: "gen-pending",
			Status:       "pending",
			QueueStatus:  status.GenerationQueueStatus{Total: 2, Outstanding: 2},
		}},
		Limit: 50,
	}}
	mux := newFreshnessMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/generations?repository=acme/app")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Truth.Freshness.State != FreshnessBuilding {
		t.Fatalf("freshness = %q, want building", envelope.Truth.Freshness.State)
	}
}

func TestFreshnessGenerationLifecycleFailedCarriesFailure(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerationLifecycleReader{page: status.GenerationLifecyclePage{
		Records: []status.GenerationLifecycleRecord{{
			ScopeID:      "git-repository-scope:acme/app",
			GenerationID: "gen-failed",
			Status:       "failed",
			QueueStatus:  status.GenerationQueueStatus{Total: 1, Failed: 1},
			LatestFailure: &status.GenerationLatestFailure{
				FailureClass:   "parser_panic",
				FailureMessage: "panic decoding tree",
			},
		}},
		Limit: 50,
	}}
	mux := newFreshnessMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/generations?scope_id=git-repository-scope:acme/app&status=failed")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	data := envelope.Data.(map[string]any)
	generations := data["generations"].([]any)
	first := generations[0].(map[string]any)
	failure, ok := first["latest_failure"].(map[string]any)
	if !ok || failure["failure_class"] != "parser_panic" {
		t.Fatalf("expected latest_failure, got %+v", first)
	}
	if reader.lastFilter.Status != "failed" {
		t.Fatalf("status filter = %q, want failed", reader.lastFilter.Status)
	}
}

func TestFreshnessGenerationLifecycleSupersededUnchanged(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerationLifecycleReader{page: status.GenerationLifecyclePage{
		Records: []status.GenerationLifecycleRecord{
			{
				ScopeID:                   "git-repository-scope:acme/app",
				GenerationID:              "gen-old",
				CurrentActiveGenerationID: "gen-new",
				Status:                    "superseded",
			},
			{
				ScopeID:       "git-repository-scope:acme/app",
				GenerationID:  "gen-unchanged",
				Status:        "completed",
				FreshnessHint: "unchanged",
				IsActive:      true,
				QueueStatus:   status.GenerationQueueStatus{Total: 4, Succeeded: 4},
			},
		},
		Limit: 50,
	}}
	mux := newFreshnessMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/generations?scope_id=git-repository-scope:acme/app")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	// No pending/outstanding rows -> fresh, not building.
	if envelope.Truth.Freshness.State != FreshnessFresh {
		t.Fatalf("freshness = %q, want fresh", envelope.Truth.Freshness.State)
	}
}

func TestFreshnessGenerationLifecycleUnknownScopeNotFound(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerationLifecycleReader{page: status.GenerationLifecyclePage{Limit: 50}}
	mux := newFreshnessMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/generations?scope_id=does-not-exist")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeScopeNotFound {
		t.Fatalf("expected scope_not_found, got %+v", envelope.Error)
	}
}

func TestFreshnessGenerationLifecycleUnknownGenerationNotFound(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerationLifecycleReader{page: status.GenerationLifecyclePage{Limit: 50}}
	mux := newFreshnessMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/generations?generation_id=missing")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeNotFound {
		t.Fatalf("expected not_found, got %+v", envelope.Error)
	}
}

func TestFreshnessGenerationLifecycleBroadScanEmptyIsNotNotFound(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerationLifecycleReader{page: status.GenerationLifecyclePage{Limit: 50}}
	mux := newFreshnessMux(reader)

	// No scope selector: an empty page is a confident empty list, not 404.
	w := doFreshnessRequest(t, mux, "/api/v0/freshness/generations?collector_kind=git")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
}

func TestFreshnessGenerationLifecycleRejectsBadLimit(t *testing.T) {
	t.Parallel()

	mux := newFreshnessMux(&recordingGenerationLifecycleReader{})
	for _, target := range []string{
		"/api/v0/freshness/generations?limit=0",
		"/api/v0/freshness/generations?limit=99999",
		"/api/v0/freshness/generations?limit=abc",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			w := doFreshnessRequest(t, mux, target)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestFreshnessGenerationLifecycleRejectsBadStatus(t *testing.T) {
	t.Parallel()

	mux := newFreshnessMux(&recordingGenerationLifecycleReader{})
	w := doFreshnessRequest(t, mux, "/api/v0/freshness/generations?status=bogus")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

func TestFreshnessGenerationLifecycleNilReaderUnavailable(t *testing.T) {
	t.Parallel()

	handler := &FreshnessHandler{Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/generations?scope_id=x")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", w.Code, w.Body.String())
	}
}

func TestFreshnessGenerationLifecycleTruncatedReported(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerationLifecycleReader{page: status.GenerationLifecyclePage{
		Records:   []status.GenerationLifecycleRecord{{ScopeID: "s", GenerationID: "g", Status: "completed"}},
		Limit:     1,
		Truncated: true,
	}}
	mux := newFreshnessMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/generations?collector_kind=git&limit=1")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	data := envelope.Data.(map[string]any)
	if !data["truncated"].(bool) {
		t.Fatalf("truncated = false, want true")
	}
}
