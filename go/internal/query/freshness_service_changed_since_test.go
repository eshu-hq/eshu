// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
)

type recordingServiceChangedSinceReader struct {
	summary    status.ServiceChangedSinceSummary
	err        error
	lastFilter status.ServiceChangedSinceFilter
}

func (r *recordingServiceChangedSinceReader) ComputeServiceChangedSinceDelta(
	_ context.Context,
	filter status.ServiceChangedSinceFilter,
) (status.ServiceChangedSinceSummary, error) {
	r.lastFilter = filter
	if r.err != nil {
		return status.ServiceChangedSinceSummary{}, r.err
	}
	return r.summary, nil
}

func newServiceChangedSinceMux(reader ServiceChangedSinceReader) *http.ServeMux {
	handler := &FreshnessHandler{ServiceChangedSince: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func TestServiceChangedSinceUnchangedProducesNoFalseDeltas(t *testing.T) {
	t.Parallel()

	reader := &recordingServiceChangedSinceReader{summary: status.ServiceChangedSinceSummary{
		ServiceID:                 "svc-checkout",
		SinceGenerationID:         "gen-prior",
		CurrentActiveGenerationID: "gen-current",
		SampleLimit:               25,
		Categories: []status.ChangedSinceCategoryDelta{
			{Category: status.ChangedSinceCategoryOwnership, Counts: status.ChangedSinceCounts{Unchanged: 3}},
		},
	}}
	mux := newServiceChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/services/changed-since?service_id=svc-checkout&since_generation_id=gen-prior")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Truth == nil || envelope.Truth.Freshness.State != FreshnessFresh {
		t.Fatalf("expected fresh truth, got %+v", envelope.Truth)
	}
	data := envelope.Data.(map[string]any)
	category := data["categories"].([]any)[0].(map[string]any)
	counts := category["counts"].(map[string]any)
	for _, key := range []string{"added", "updated", "retired", "superseded"} {
		if counts[key].(float64) != 0 {
			t.Fatalf("ownership has false %s delta: %+v", key, counts)
		}
	}
}

func TestServiceChangedSinceUnknownServiceNotFound(t *testing.T) {
	t.Parallel()

	reader := &recordingServiceChangedSinceReader{summary: status.ServiceChangedSinceSummary{}}
	mux := newServiceChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/services/changed-since?service_id=missing&since_generation_id=gen-prior")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeServiceNotFound {
		t.Fatalf("expected service_not_found error, got %+v", envelope.Error)
	}
}

func TestServiceChangedSinceUnavailableWhenNoActiveGeneration(t *testing.T) {
	t.Parallel()

	reader := &recordingServiceChangedSinceReader{summary: status.ServiceChangedSinceSummary{
		ServiceID:   "svc-checkout",
		SampleLimit: 25,
		Unavailable: true,
		Categories: []status.ChangedSinceCategoryDelta{
			{Category: status.ChangedSinceCategoryOwnership, Unavailable: true},
		},
	}}
	mux := newServiceChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/services/changed-since?service_id=svc-checkout&since_generation_id=gen-prior")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Truth == nil || envelope.Truth.Freshness.State != FreshnessUnavailable {
		t.Fatalf("expected unavailable freshness, got %+v", envelope.Truth)
	}
}

func TestServiceChangedSinceUnknownPriorGenerationNotFound(t *testing.T) {
	t.Parallel()

	reader := &recordingServiceChangedSinceReader{summary: status.ServiceChangedSinceSummary{
		ServiceID:                 "svc-checkout",
		CurrentActiveGenerationID: "gen-current",
		SampleLimit:               25,
	}}
	mux := newServiceChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/services/changed-since?service_id=svc-checkout&since_generation_id=missing")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
}

func TestServiceChangedSinceRequiresServiceID(t *testing.T) {
	t.Parallel()

	mux := newServiceChangedSinceMux(&recordingServiceChangedSinceReader{})
	w := doFreshnessRequest(t, mux, "/api/v0/freshness/services/changed-since?since_generation_id=gen-prior")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

func TestServiceChangedSinceRequiresSinceReference(t *testing.T) {
	t.Parallel()

	mux := newServiceChangedSinceMux(&recordingServiceChangedSinceReader{})
	w := doFreshnessRequest(t, mux, "/api/v0/freshness/services/changed-since?service_id=svc-checkout")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

func TestServiceChangedSinceReaderNotConfigured(t *testing.T) {
	t.Parallel()

	mux := newServiceChangedSinceMux(nil)
	w := doFreshnessRequest(t, mux, "/api/v0/freshness/services/changed-since?service_id=svc-checkout&since_generation_id=gen-prior")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", w.Code, w.Body.String())
	}
}
