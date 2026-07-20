// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
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

// TestObservabilityCoverageListCorrelationsScopedEmptyGrantReturnsEmptyWithoutStoreRead
// is the #5167 counterpart to the admission-decision and kubernetes-correlation
// empty-grant precedents: a scoped caller with no granted repository or
// ingestion scope must never reach the store.
func TestObservabilityCoverageListCorrelationsScopedEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &recordingObservabilityCoverageCorrelationStore{rows: []ObservabilityCoverageCorrelationRow{
		{CorrelationID: "observability-coverage-1", TargetUID: "arn:aws:ec2:us-east-1:111122223333:instance/i-abc"},
	}}
	handler := &ObservabilityCoverageHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/observability/coverage/correlations?target_uid=arn:aws:ec2:us-east-1:111122223333:instance/i-abc&limit=10",
		nil,
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastFilter.TargetUID != "" {
		t.Fatal("store was called for empty scoped grants")
	}
	if strings.Contains(w.Body.String(), "observability-coverage-1") {
		t.Fatalf("empty scoped response leaked a correlation id: %s", w.Body.String())
	}
}

// observabilityCoverageScopedFixtureRow returns the raw driver row for one
// reducer_observability_coverage_correlation fact, in
// listObservabilityCoverageCorrelationsQuery's projected column order
// (fact_id, payload).
func observabilityCoverageScopedFixtureRow(t *testing.T) []driver.Value {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"provider":        "aws",
		"coverage_signal": "alarm",
		"target_uid":      "arn:aws:ec2:us-east-1:111122223333:instance/i-tenant-a",
		"outcome":         "exact",
		"coverage_status": "covered",
	})
	if err != nil {
		t.Fatalf("marshal fixture payload: %v", err)
	}
	return []driver.Value{"observability-coverage-tenant-a", payload}
}

// TestObservabilityCoverageListCorrelationsScopedGrantHitsRealStoreAndReturnsRowData
// proves the #5167 fix against the ACTUAL production backend
// (PostgresObservabilityCoverageCorrelationStore over a real *sql.DB, the same
// type cmd/api/wiring_handlers.go and cmd/mcp-server/wiring.go construct): a
// scoped caller with a matching grant reaches the store, the dispatched SQL
// carries the access-scoping predicate with the caller's granted ids bound as
// args, and the response surfaces the real row data the fake driver returned.
func TestObservabilityCoverageListCorrelationsScopedGrantHitsRealStoreAndReturnsRowData(t *testing.T) {
	t.Parallel()

	db, recorder := openScopeQueryerTestDB(t, []string{"fact_id", "payload"}, [][]driver.Value{
		observabilityCoverageScopedFixtureRow(t),
	})
	handler := &ObservabilityCoverageHandler{Correlations: NewPostgresObservabilityCoverageCorrelationStore(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/observability/coverage/correlations?target_uid=arn:aws:ec2:us-east-1:111122223333:instance/i-tenant-a&limit=10",
		nil,
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedScopeIDs:      []string{"aws-scope:tenant-a"},
		AllowedRepositoryIDs: []string{"repo-tenant-a"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := recorder.calls(), 1; got != want {
		t.Fatalf("queryer received %d queries, want exactly %d", got, want)
	}
	dispatched := recorder.queries[0]
	if !strings.Contains(dispatched, "fact.scope_id = ANY($14) OR fact.scope_id = ANY($15)") {
		t.Fatalf("dispatched query missing #5167 access-scoping predicate:\n%s", dispatched)
	}
	args := recorder.args[0]
	if len(args) < 15 {
		t.Fatalf("len(args) = %d, want at least 15", len(args))
	}
	if got := fmt.Sprintf("%v", args[13]); !strings.Contains(got, "repo-tenant-a") {
		t.Fatalf("allowed_repository_ids arg = %v, want it to contain %q", got, "repo-tenant-a")
	}
	if got := fmt.Sprintf("%v", args[14]); !strings.Contains(got, "aws-scope:tenant-a") {
		t.Fatalf("allowed_scope_ids arg = %v, want it to contain %q", got, "aws-scope:tenant-a")
	}

	var resp struct {
		Correlations []ObservabilityCoverageCorrelationResult `json:"correlations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(resp.Correlations) != 1 {
		t.Fatalf("len(correlations) = %d, want 1; body = %s", len(resp.Correlations), w.Body.String())
	}
	if got, want := resp.Correlations[0].TargetUID, "arn:aws:ec2:us-east-1:111122223333:instance/i-tenant-a"; got != want {
		t.Fatalf("TargetUID = %q, want %q (real row data from the fake driver)", got, want)
	}
	if got, want := resp.Correlations[0].CoverageStatus, "covered"; got != want {
		t.Fatalf("CoverageStatus = %q, want %q (real row data from the fake driver)", got, want)
	}
}

// TestObservabilityCoverageListCorrelationsUnscopedQueryStaysUnfiltered is the
// no-regression counterpart: a shared/admin caller (no AuthContext) must
// still issue the byte-identical unscoped query with no access-scoping
// predicate.
func TestObservabilityCoverageListCorrelationsUnscopedQueryStaysUnfiltered(t *testing.T) {
	t.Parallel()

	db, recorder := openScopeQueryerTestDB(t, []string{"fact_id", "payload"}, [][]driver.Value{
		observabilityCoverageScopedFixtureRow(t),
	})
	handler := &ObservabilityCoverageHandler{Correlations: NewPostgresObservabilityCoverageCorrelationStore(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/observability/coverage/correlations?target_uid=arn:aws:ec2:us-east-1:111122223333:instance/i-tenant-a&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := recorder.calls(), 1; got != want {
		t.Fatalf("queryer received %d queries, want exactly %d", got, want)
	}
	if strings.Contains(recorder.queries[0], "allowed_repository_ids") || strings.Contains(recorder.queries[0], "= ANY($14)") {
		t.Fatalf("unscoped/admin query must stay unfiltered, got:\n%s", recorder.queries[0])
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
