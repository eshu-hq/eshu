// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
)

type recordingChangedSinceReader struct {
	summary    status.ChangedSinceSummary
	err        error
	lastFilter status.ChangedSinceFilter
}

func (r *recordingChangedSinceReader) ComputeChangedSinceDelta(
	_ context.Context,
	filter status.ChangedSinceFilter,
) (status.ChangedSinceSummary, error) {
	r.lastFilter = filter
	if r.err != nil {
		return status.ChangedSinceSummary{}, r.err
	}
	return r.summary, nil
}

func newChangedSinceMux(reader ChangedSinceReader) *http.ServeMux {
	handler := &FreshnessHandler{ChangedSince: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func TestChangedSinceUnchangedProducesNoFalseDeltas(t *testing.T) {
	t.Parallel()

	reader := &recordingChangedSinceReader{summary: status.ChangedSinceSummary{
		ScopeID:                   "git-repository-scope:acme/app",
		ScopeKind:                 "repository",
		SinceGenerationID:         "gen-prior",
		CurrentActiveGenerationID: "gen-current",
		SampleLimit:               25,
		Categories: []status.ChangedSinceCategoryDelta{
			{Category: status.ChangedSinceCategoryFiles, Counts: status.ChangedSinceCounts{Unchanged: 10}},
			{Category: status.ChangedSinceCategoryContentEntities, Counts: status.ChangedSinceCounts{Unchanged: 4}},
			{Category: status.ChangedSinceCategoryFacts, Counts: status.ChangedSinceCounts{Unchanged: 2}},
		},
	}}
	mux := newChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=git-repository-scope:acme/app&since_generation_id=gen-prior")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Truth == nil || envelope.Truth.Freshness.State != FreshnessFresh {
		t.Fatalf("expected fresh truth, got %+v", envelope.Truth)
	}
	data := envelope.Data.(map[string]any)
	categories := data["categories"].([]any)
	for _, raw := range categories {
		category := raw.(map[string]any)
		counts := category["counts"].(map[string]any)
		for _, key := range []string{"added", "updated", "retired", "superseded"} {
			if counts[key].(float64) != 0 {
				t.Fatalf("category %v has false %s delta: %+v", category["category"], key, counts)
			}
		}
	}
}

func TestChangedSinceAllVerdictsSurfaceSeparately(t *testing.T) {
	t.Parallel()

	reader := &recordingChangedSinceReader{summary: status.ChangedSinceSummary{
		ScopeID:                   "git-repository-scope:acme/app",
		ScopeKind:                 "repository",
		SinceGenerationID:         "gen-prior",
		CurrentActiveGenerationID: "gen-current",
		SampleLimit:               25,
		Categories: []status.ChangedSinceCategoryDelta{
			{
				Category: status.ChangedSinceCategoryFiles,
				Counts:   status.ChangedSinceCounts{Added: 2, Updated: 1, Unchanged: 5, Retired: 1, Superseded: 1},
				Samples: map[status.ChangedSinceClassification][]status.ChangedSinceSample{
					status.ChangedSinceRetired:    {{StableFactKey: "file:gone", FactKind: "file"}},
					status.ChangedSinceSuperseded: {{StableFactKey: "file:dropped", FactKind: "file"}},
				},
			},
		},
	}}
	mux := newChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?repository=acme/app&since_generation_id=gen-prior")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	data := envelope.Data.(map[string]any)
	category := data["categories"].([]any)[0].(map[string]any)
	counts := category["counts"].(map[string]any)
	if counts["retired"].(float64) != 1 || counts["superseded"].(float64) != 1 {
		t.Fatalf("retired/superseded collapsed: %+v", counts)
	}
	samples := category["samples"].(map[string]any)
	if _, ok := samples["retired"]; !ok {
		t.Fatalf("missing retired samples: %+v", samples)
	}
	if reader.lastFilter.Repository != "acme/app" {
		t.Fatalf("filter repository = %q", reader.lastFilter.Repository)
	}
}

func TestChangedSinceUnavailableMapsToUnavailableFreshness(t *testing.T) {
	t.Parallel()

	reader := &recordingChangedSinceReader{summary: status.ChangedSinceSummary{
		ScopeID:     "git-repository-scope:acme/app",
		ScopeKind:   "repository",
		Unavailable: true,
		SampleLimit: 25,
		Categories: []status.ChangedSinceCategoryDelta{
			{Category: status.ChangedSinceCategoryFiles, Unavailable: true},
		},
	}}
	mux := newChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=git-repository-scope:acme/app&since_generation_id=gen-prior")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Truth.Freshness.State != FreshnessUnavailable {
		t.Fatalf("freshness = %q, want unavailable", envelope.Truth.Freshness.State)
	}
	data := envelope.Data.(map[string]any)
	if !data["unavailable"].(bool) {
		t.Fatalf("unavailable = false, want true")
	}
}

func TestChangedSinceRetentionExpiredReasonSurfaces(t *testing.T) {
	t.Parallel()

	reader := &recordingChangedSinceReader{summary: status.ChangedSinceSummary{
		ScopeID:                   "git-repository-scope:acme/app",
		ScopeKind:                 "repository",
		SinceGenerationID:         "gen-pruned",
		CurrentActiveGenerationID: "gen-current",
		Unavailable:               true,
		UnavailableReason:         status.ChangedSinceUnavailableRetentionExpired,
		SampleLimit:               25,
		Categories: []status.ChangedSinceCategoryDelta{
			{Category: status.ChangedSinceCategoryFiles, Unavailable: true},
		},
	}}
	mux := newChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=git-repository-scope:acme/app&since_generation_id=gen-pruned")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Truth.Freshness.State != FreshnessUnavailable {
		t.Fatalf("freshness = %q, want unavailable", envelope.Truth.Freshness.State)
	}
	if !strings.Contains(envelope.Truth.Freshness.Detail, "retention") {
		t.Fatalf("freshness detail = %q, want retention context", envelope.Truth.Freshness.Detail)
	}
	if envelope.Truth.Freshness.Cause != FreshnessCauseRetentionExpired {
		t.Fatalf("freshness cause = %q, want %q", envelope.Truth.Freshness.Cause, FreshnessCauseRetentionExpired)
	}
	if envelope.Truth.Freshness.NextCheck == nil {
		t.Fatalf("freshness next_check = nil, want generation lifecycle drilldown")
	}
	if got, want := envelope.Truth.Freshness.NextCheck.Tool, "get_generation_lifecycle"; got != want {
		t.Fatalf("freshness next_check.tool = %q, want %q", got, want)
	}
	data := envelope.Data.(map[string]any)
	if got, want := data["unavailable_reason"], string(status.ChangedSinceUnavailableRetentionExpired); got != want {
		t.Fatalf("unavailable_reason = %v, want %v", got, want)
	}
}

func TestChangedSinceBuildingMapsToBuildingFreshness(t *testing.T) {
	t.Parallel()

	reader := &recordingChangedSinceReader{summary: status.ChangedSinceSummary{
		ScopeID:                   "git-repository-scope:acme/app",
		ScopeKind:                 "repository",
		SinceGenerationID:         "gen-prior",
		CurrentActiveGenerationID: "gen-current",
		Building:                  true,
		SampleLimit:               25,
		Categories: []status.ChangedSinceCategoryDelta{
			{Category: status.ChangedSinceCategoryFiles, Counts: status.ChangedSinceCounts{Added: 1}},
		},
	}}
	mux := newChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=x&since_generation_id=gen-prior")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Truth.Freshness.State != FreshnessBuilding {
		t.Fatalf("freshness = %q, want building", envelope.Truth.Freshness.State)
	}
}

func TestChangedSinceUnknownScopeNotFound(t *testing.T) {
	t.Parallel()

	reader := &recordingChangedSinceReader{summary: status.ChangedSinceSummary{}}
	mux := newChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=missing&since_generation_id=gen-prior")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeScopeNotFound {
		t.Fatalf("expected scope_not_found, got %+v", envelope.Error)
	}
}

func TestChangedSinceUnknownSinceGenerationNotFound(t *testing.T) {
	t.Parallel()

	// Scope resolved, but no prior generation and not unavailable.
	reader := &recordingChangedSinceReader{summary: status.ChangedSinceSummary{
		ScopeID:                   "git-repository-scope:acme/app",
		ScopeKind:                 "repository",
		CurrentActiveGenerationID: "gen-current",
		SampleLimit:               25,
	}}
	mux := newChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=git-repository-scope:acme/app&since_generation_id=missing")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
	envelope := decodeFreshnessEnvelope(t, w)
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeNotFound {
		t.Fatalf("expected not_found, got %+v", envelope.Error)
	}
}

func TestChangedSinceRequiresScopeSelector(t *testing.T) {
	t.Parallel()

	mux := newChangedSinceMux(&recordingChangedSinceReader{})
	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?since_generation_id=gen-prior")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

func TestChangedSinceRequiresSinceReference(t *testing.T) {
	t.Parallel()

	mux := newChangedSinceMux(&recordingChangedSinceReader{})
	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=x")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

func TestChangedSinceRejectsBadObservedAt(t *testing.T) {
	t.Parallel()

	mux := newChangedSinceMux(&recordingChangedSinceReader{})
	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=x&since_observed_at=not-a-time")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

func TestChangedSinceRejectsBadSampleLimit(t *testing.T) {
	t.Parallel()

	mux := newChangedSinceMux(&recordingChangedSinceReader{})
	for _, target := range []string{
		"/api/v0/freshness/changed-since?scope_id=x&since_generation_id=g&sample_limit=0",
		"/api/v0/freshness/changed-since?scope_id=x&since_generation_id=g&sample_limit=9999",
		"/api/v0/freshness/changed-since?scope_id=x&since_generation_id=g&sample_limit=abc",
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

func TestChangedSinceNilReaderUnavailable(t *testing.T) {
	t.Parallel()

	handler := &FreshnessHandler{Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=x&since_generation_id=g")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", w.Code, w.Body.String())
	}
}

func TestChangedSinceObservedAtParsed(t *testing.T) {
	t.Parallel()

	reader := &recordingChangedSinceReader{summary: status.ChangedSinceSummary{
		ScopeID:                   "git-repository-scope:acme/app",
		ScopeKind:                 "repository",
		SinceGenerationID:         "gen-prior",
		CurrentActiveGenerationID: "gen-current",
		SampleLimit:               25,
		Categories:                []status.ChangedSinceCategoryDelta{{Category: status.ChangedSinceCategoryFiles}},
	}}
	mux := newChangedSinceMux(reader)

	w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=git-repository-scope:acme/app&since_observed_at=2026-06-09T10:00:00Z")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if reader.lastFilter.SinceObservedAt.IsZero() {
		t.Fatalf("SinceObservedAt not parsed into filter")
	}
}
