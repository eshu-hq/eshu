// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingObservabilityCoverageCorrelationStore struct {
	rows       []ObservabilityCoverageCorrelationRow
	lastFilter ObservabilityCoverageCorrelationFilter
}

func (s *recordingObservabilityCoverageCorrelationStore) ListObservabilityCoverageCorrelations(
	_ context.Context,
	filter ObservabilityCoverageCorrelationFilter,
) ([]ObservabilityCoverageCorrelationRow, error) {
	s.lastFilter = filter
	return append([]ObservabilityCoverageCorrelationRow(nil), s.rows...), nil
}

func TestObservabilityCoverageListCorrelationsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &ObservabilityCoverageHandler{Correlations: &recordingObservabilityCoverageCorrelationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/observability/coverage/correlations?limit=10",
		"/api/v0/observability/coverage/correlations?target_uid=arn:aws:ec2:us-east-1:111122223333:instance/i-abc",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestObservabilityCoverageListCorrelationsUsesBoundedStore(t *testing.T) {
	t.Parallel()

	store := &recordingObservabilityCoverageCorrelationStore{
		rows: []ObservabilityCoverageCorrelationRow{
			{
				CorrelationID:          "observability-coverage-1",
				Provider:               "aws",
				CoverageSignal:         "alarm",
				ObservabilityObjectRef: "arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high",
				ObservabilityUID:       "cloudwatch-alarm-uid",
				TargetUID:              "arn:aws:ec2:us-east-1:111122223333:instance/i-abc",
				TargetServiceRef:       "",
				Outcome:                "exact",
				Reason:                 "alarm InstanceId dimension matched scanned resource id",
				CoverageStatus:         "covered",
				ProvenanceOnly:         false,
				ResolutionMode:         "resource_id",
				SourceClass:            "mixed",
				SourceClasses:          []string{"declared", "observed"},
				SourceKind:             "mixed",
				SourceKinds:            []string{"grafana", "kubernetes"},
				ResourceClass:          "dashboard",
				FreshnessState:         "current",
				EvidenceFactIDs:        []string{"aws_resource:i-abc", "aws_resource:alarm-cpu-high"},
			},
			{CorrelationID: "observability-coverage-2", CoverageSignal: "alarm", TargetUID: "arn:aws:ec2:us-east-1:111122223333:instance/i-def", Outcome: "unresolved", CoverageStatus: "gap", ProvenanceOnly: true},
		},
	}
	handler := &ObservabilityCoverageHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/observability/coverage/correlations?target_uid=arn:aws:ec2:us-east-1:111122223333:instance/i-abc&coverage_signal=alarm&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.TargetUID, "arn:aws:ec2:us-east-1:111122223333:instance/i-abc"; got != want {
		t.Fatalf("TargetUID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.CoverageSignal, "alarm"; got != want {
		t.Fatalf("CoverageSignal = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Correlations []ObservabilityCoverageCorrelationResult `json:"correlations"`
		Count        int                                      `json:"count"`
		Limit        int                                      `json:"limit"`
		Truncated    bool                                     `json:"truncated"`
		NextCursor   map[string]string                        `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Correlations), 1; got != want {
		t.Fatalf("len(correlations) = %d, want %d", got, want)
	}
	if got, want := resp.Correlations[0].TargetUID, "arn:aws:ec2:us-east-1:111122223333:instance/i-abc"; got != want {
		t.Fatalf("TargetUID = %q, want %q", got, want)
	}
	if got, want := resp.Correlations[0].CoverageStatus, "covered"; got != want {
		t.Fatalf("CoverageStatus = %q, want %q", got, want)
	}
	if got, want := resp.Correlations[0].SourceClass, "mixed"; got != want {
		t.Fatalf("SourceClass = %q, want %q", got, want)
	}
	if got, want := resp.Correlations[0].SourceClasses, []string{"declared", "observed"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SourceClasses = %v, want %v", got, want)
	}
	if got, want := resp.Correlations[0].ResourceClass, "dashboard"; got != want {
		t.Fatalf("ResourceClass = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_correlation_id"], "observability-coverage-1"; got != want {
		t.Fatalf("next_cursor.after_correlation_id = %q, want %q", got, want)
	}
}

func TestObservabilityCoverageCorrelationQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'provider' = $3",
		"fact.payload->>'coverage_signal' = $4",
		"fact.payload->>'observability_object_ref' = $5",
		"fact.payload->>'target_uid' = $6",
		"fact.payload->>'target_service_ref' = $7",
		"fact.payload->>'outcome' = $8",
		"fact.payload->>'coverage_status' = $9",
		"fact.payload->>'source_class' = $10",
		"fact.payload->>'resource_class' = $11",
		"fact.fact_id > $12",
	} {
		if !strings.Contains(listObservabilityCoverageCorrelationsQuery, want) {
			t.Fatalf("listObservabilityCoverageCorrelationsQuery missing %q:\n%s", want, listObservabilityCoverageCorrelationsQuery)
		}
	}
}

func TestObservabilityCoverageListCorrelationsFiltersSourceAndResourceClass(t *testing.T) {
	t.Parallel()

	store := &recordingObservabilityCoverageCorrelationStore{}
	handler := &ObservabilityCoverageHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/observability/coverage/correlations?provider=grafana&source_class=declared&resource_class=dashboard&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.SourceClass, "declared"; got != want {
		t.Fatalf("SourceClass = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.ResourceClass, "dashboard"; got != want {
		t.Fatalf("ResourceClass = %q, want %q", got, want)
	}
}
