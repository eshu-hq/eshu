// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingKubernetesCorrelationStore struct {
	rows       []KubernetesCorrelationRow
	lastFilter KubernetesCorrelationFilter
}

func (s *recordingKubernetesCorrelationStore) ListKubernetesCorrelations(
	_ context.Context,
	filter KubernetesCorrelationFilter,
) ([]KubernetesCorrelationRow, error) {
	s.lastFilter = filter
	return append([]KubernetesCorrelationRow(nil), s.rows...), nil
}

func TestKubernetesListCorrelationsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &KubernetesHandler{Correlations: &recordingKubernetesCorrelationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/kubernetes/correlations?limit=10",
		"/api/v0/kubernetes/correlations?cluster_id=cluster-prod",
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

func TestKubernetesListCorrelationsUsesBoundedStore(t *testing.T) {
	t.Parallel()

	store := &recordingKubernetesCorrelationStore{
		rows: []KubernetesCorrelationRow{
			{
				CorrelationID:    "kubernetes-correlation-1",
				ClusterID:        "cluster-prod",
				WorkloadObjectID: "deployment/payments/checkout",
				Namespace:        "payments",
				WorkloadName:     "checkout",
				ImageRef:         "registry.example.com/checkout@sha256:abc",
				SourceDigest:     "sha256:abc",
				JoinMode:         "digest",
				Outcome:          "exact",
				DriftKind:        "in_sync",
				Reason:           "live image digest matched an active deployment-source digest",
				ProvenanceOnly:   false,
			},
			{CorrelationID: "kubernetes-correlation-2", WorkloadObjectID: "deployment/payments/orphan", Outcome: "unresolved"},
		},
	}
	handler := &KubernetesHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/kubernetes/correlations?cluster_id=cluster-prod&namespace=payments&outcome=exact&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.ClusterID, "cluster-prod"; got != want {
		t.Fatalf("ClusterID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Namespace, "payments"; got != want {
		t.Fatalf("Namespace = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Outcome, "exact"; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Correlations []KubernetesCorrelationResult `json:"correlations"`
		Count        int                           `json:"count"`
		Limit        int                           `json:"limit"`
		Truncated    bool                          `json:"truncated"`
		NextCursor   map[string]string             `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Correlations), 1; got != want {
		t.Fatalf("len(correlations) = %d, want %d", got, want)
	}
	if got, want := resp.Correlations[0].WorkloadObjectID, "deployment/payments/checkout"; got != want {
		t.Fatalf("WorkloadObjectID = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_correlation_id"], "kubernetes-correlation-1"; got != want {
		t.Fatalf("next_cursor.after_correlation_id = %q, want %q", got, want)
	}
}

func TestKubernetesCorrelationQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'cluster_id' = $3",
		"fact.payload->>'workload_object_id' = $4",
		"fact.payload->>'namespace' = $5",
		"fact.payload->>'image_ref' = $6",
		"fact.payload->>'source_digest' = $7",
		"fact.payload->>'outcome' = $8",
		"fact.payload->>'drift_kind' = $9",
		"fact.fact_id > $10",
		"ORDER BY fact.fact_id ASC",
	} {
		if !strings.Contains(listKubernetesCorrelationsQuery, want) {
			t.Fatalf("listKubernetesCorrelationsQuery missing %q:\n%s", want, listKubernetesCorrelationsQuery)
		}
	}
}

// failingKubernetesCorrelationQueryer fails the test if any query reaches the
// database. It proves that scope/anchor validation rejects an unbounded read
// before a SQL statement is ever issued.
type failingKubernetesCorrelationQueryer struct {
	t *testing.T
}

func (q failingKubernetesCorrelationQueryer) QueryContext(
	context.Context,
	string,
	...any,
) (*sql.Rows, error) {
	q.t.Helper()
	q.t.Fatal("QueryContext called: unbounded scope reached the database instead of being rejected")
	return nil, nil
}

func TestKubernetesCorrelationFilterRejectsUnboundedScope(t *testing.T) {
	t.Parallel()

	// A non-nil DB ensures the nil-DB guard passes so the scope/anchor
	// validation is the path actually exercised. The queryer fails the test if
	// it is ever reached, proving the unbounded read is rejected up front.
	store := PostgresKubernetesCorrelationStore{DB: failingKubernetesCorrelationQueryer{t: t}}
	_, err := store.ListKubernetesCorrelations(context.Background(), KubernetesCorrelationFilter{Limit: 10})
	if err == nil {
		t.Fatal("ListKubernetesCorrelations() error = nil, want non-nil for unbounded scope")
	}
	if want := "is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ListKubernetesCorrelations() error = %q, want it to contain %q", err.Error(), want)
	}
}

func TestKubernetesCorrelationFilterRejectsNilDB(t *testing.T) {
	t.Parallel()

	store := PostgresKubernetesCorrelationStore{DB: nil}
	_, err := store.ListKubernetesCorrelations(context.Background(), KubernetesCorrelationFilter{
		ClusterID: "cluster-prod",
		Limit:     10,
	})
	if err == nil {
		t.Fatal("ListKubernetesCorrelations() error = nil, want non-nil for nil DB")
	}
	if want := "database is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ListKubernetesCorrelations() error = %q, want it to contain %q", err.Error(), want)
	}
}
