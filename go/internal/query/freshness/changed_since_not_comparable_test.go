// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// notComparableSummary is the reader answer for a window with a delta
// generation at one or both ends (#7282).
func notComparableSummary(sinceDelta, currentDelta bool) status.ChangedSinceSummary {
	return status.ChangedSinceSummary{
		ScopeID:                   "git-repository-scope:acme/app",
		ScopeKind:                 "repository",
		SinceGenerationID:         "gen-prior",
		CurrentActiveGenerationID: "gen-current",
		Unavailable:               true,
		UnavailableReason:         status.ChangedSinceUnavailableBaselineNotComparable,
		SinceIsDelta:              sinceDelta,
		CurrentIsDelta:            currentDelta,
		SampleLimit:               25,
		Categories: []status.ChangedSinceCategoryDelta{
			{Category: status.ChangedSinceCategoryFiles, Unavailable: true},
		},
	}
}

func TestChangedSinceBaselineNotComparableSurfaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                     string
		sinceDelta, currentDelta bool
		detail                   string
	}{
		{name: "delta current", currentDelta: true, detail: "current active generation is a delta generation"},
		{name: "delta since", sinceDelta: true, detail: "choose a full generation as the since reference"},
		{name: "both delta", sinceDelta: true, currentDelta: true, detail: "both the since generation and the current active generation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux := newChangedSinceMux(&recordingChangedSinceReader{summary: notComparableSummary(tt.sinceDelta, tt.currentDelta)})
			w := doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=git-repository-scope:acme/app&since_generation_id=gen-prior")
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
			}
			envelope := decodeFreshnessEnvelope(t, w)
			if envelope.Truth.Freshness.State != querycontract.FreshnessUnavailable {
				t.Fatalf("freshness = %q, want unavailable", envelope.Truth.Freshness.State)
			}
			if !strings.Contains(envelope.Truth.Freshness.Detail, tt.detail) {
				t.Fatalf("freshness detail = %q, want it to contain %q", envelope.Truth.Freshness.Detail, tt.detail)
			}
			if envelope.Truth.Freshness.Cause != "" || envelope.Truth.Freshness.NextCheck != nil {
				t.Fatalf("cause = %q next_check = %+v; nothing lags, so no freshness cause applies",
					envelope.Truth.Freshness.Cause, envelope.Truth.Freshness.NextCheck)
			}
			data := envelope.Data.(map[string]any)
			if got, want := data["unavailable_reason"], "baseline_not_comparable"; got != want {
				t.Fatalf("unavailable_reason = %v, want %v", got, want)
			}
			if !data["unavailable"].(bool) {
				t.Fatalf("unavailable = false, want true")
			}
			for key, want := range map[string]bool{"since_is_delta": tt.sinceDelta, "current_is_delta": tt.currentDelta} {
				got, present := data[key]
				if want && got != true {
					t.Fatalf("%s = %v, want true", key, got)
				}
				if !want && present {
					t.Fatalf("%s = %v, want the field omitted for a full generation", key, got)
				}
			}
		})
	}
}

// TestChangedSinceUnavailableReasonIsRecordedOnTheSpan pins the operator
// signal for #7282: a refused window carries the closed reason on the handler
// span, and a served diff carries none. It swaps this package's tracer, so it
// does not run in parallel.
func TestChangedSinceUnavailableReasonIsRecordedOnTheSpan(t *testing.T) {
	record := func(t *testing.T, summary status.ChangedSinceSummary) map[string]any {
		t.Helper()
		recorder := tracetest.NewSpanRecorder()
		provider := tracesdk.NewTracerProvider(tracesdk.WithSpanProcessor(recorder))
		t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
		previousTracer := freshnessHandlerTracer
		freshnessHandlerTracer = provider.Tracer("changed-since-unavailable-reason-test")
		t.Cleanup(func() { freshnessHandlerTracer = previousTracer })

		mux := newChangedSinceMux(&recordingChangedSinceReader{summary: summary})
		doFreshnessRequest(t, mux, "/api/v0/freshness/changed-since?scope_id=git-repository-scope:acme/app&since_generation_id=gen-prior")
		spans := recorder.Ended()
		if len(spans) != 1 || spans[0].Name() != telemetry.SpanQueryFreshnessChangedSince {
			t.Fatalf("spans = %d, want one %s span", len(spans), telemetry.SpanQueryFreshnessChangedSince)
		}
		attributes := map[string]any{}
		for _, item := range spans[0].Attributes() {
			attributes[string(item.Key)] = item.Value.AsInterface()
		}
		return attributes
	}

	refused := record(t, notComparableSummary(false, true))
	if got, want := refused[telemetry.SpanAttrChangedSinceUnavailableReason], "baseline_not_comparable"; got != want {
		t.Fatalf("%s = %v, want %q", telemetry.SpanAttrChangedSinceUnavailableReason, got, want)
	}
	served := notComparableSummary(false, false)
	served.Unavailable, served.UnavailableReason = false, ""
	if got, present := record(t, served)[telemetry.SpanAttrChangedSinceUnavailableReason]; present {
		t.Fatalf("%s = %v on a served diff, want it absent", telemetry.SpanAttrChangedSinceUnavailableReason, got)
	}
}

// TestChangedSinceUnknownUnavailableReasonIsNotMistakenForNoCurrentGeneration
// pins the envelope switch: only an empty reason means the scope has no
// current active generation. A reason the switch does not know must name
// itself in the detail and carry no pending-generation cause, so a future
// reason added without its own case shows up instead of inheriting the wrong
// explanation (#7286 review).
func TestChangedSinceUnknownUnavailableReasonIsNotMistakenForNoCurrentGeneration(t *testing.T) {
	t.Parallel()

	handler := &Handler{}
	noCurrent := handler.changedSinceTruthEnvelope(status.ChangedSinceSummary{Unavailable: true})
	if !strings.Contains(noCurrent.Freshness.Detail, "no current active generation") {
		t.Fatalf("empty reason detail = %q, want the no-current-generation explanation", noCurrent.Freshness.Detail)
	}
	if noCurrent.Freshness.Cause != CausePendingRepoGeneration {
		t.Fatalf("empty reason cause = %q, want %q", noCurrent.Freshness.Cause, CausePendingRepoGeneration)
	}

	unknown := handler.changedSinceTruthEnvelope(status.ChangedSinceSummary{
		Unavailable:       true,
		UnavailableReason: "some_future_reason",
	})
	if unknown.Freshness.State != querycontract.FreshnessUnavailable {
		t.Fatalf("unknown reason state = %q, want %q", unknown.Freshness.State, querycontract.FreshnessUnavailable)
	}
	if strings.Contains(unknown.Freshness.Detail, "no current active generation") {
		t.Fatalf("unknown reason detail = %q, must not claim the scope has no current generation", unknown.Freshness.Detail)
	}
	if !strings.Contains(unknown.Freshness.Detail, "some_future_reason") {
		t.Fatalf("unknown reason detail = %q, want it to name the reason", unknown.Freshness.Detail)
	}
	if unknown.Freshness.Cause != "" {
		t.Fatalf("unknown reason cause = %q, want none", unknown.Freshness.Cause)
	}
}
