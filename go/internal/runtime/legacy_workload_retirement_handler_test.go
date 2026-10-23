// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
)

type fakeRetirer struct {
	req  legacyworkload.RetireRequest
	resp legacyworkload.RetireResponse
	err  error
	n    int
}

func (f *fakeRetirer) Run(_ context.Context, req legacyworkload.RetireRequest) (legacyworkload.RetireResponse, error) {
	f.n++
	f.req = req
	return f.resp, f.err
}

func postRetire(t *testing.T, f *fakeRetirer, body string) *httptest.ResponseRecorder {
	t.Helper()
	h, err := NewLegacyWorkloadRetirementHandler(f)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/retire-legacy-workloads", strings.NewReader(body)))
	return rec
}

func TestLegacyWorkloadRetirementRequiresARunner(t *testing.T) {
	if _, err := NewLegacyWorkloadRetirementHandler(nil); err == nil {
		t.Fatal("want an error for a nil runner")
	}
}

func TestLegacyWorkloadRetirementDryRunReturnsTheContractBody(t *testing.T) {
	f := &fakeRetirer{resp: legacyworkload.RetireResponse{
		DryRun: true, Eligible: 1, Blocked: 1,
		Candidates: []legacyworkload.RetireCandidate{
			{ID: "workload:checkout-service", TwinID: "workload:repository:r:checkout-service", RepoID: "repository:r", Name: "checkout-service", Relationships: map[string]int{}, Eligible: true},
			{ID: "workload:x", Name: "x", RepoID: "repository:r", Relationships: map[string]int{"DOCUMENTS": 1}, BlockedReason: "relationships"},
		},
	}}
	rec := postRetire(t, f, `{"dry_run": true, "limit": 5, "ids": ["workload:checkout-service"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if !f.req.DryRun || f.req.Limit != 5 || len(f.req.IDs) != 1 {
		t.Fatalf("request = %+v", f.req)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"dry_run", "candidates", "eligible", "blocked", "deleted"} {
		if _, ok := got[key]; !ok {
			t.Errorf("response is missing %q: %s", key, rec.Body)
		}
	}
	first := got["candidates"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "twin_id", "repo_id", "name", "relationships", "eligible", "blocked_reason"} {
		if _, ok := first[key]; !ok {
			t.Errorf("candidate is missing %q: %v", key, first)
		}
	}
}

func TestLegacyWorkloadRetirementRequiresExplicitDryRun(t *testing.T) {
	f := &fakeRetirer{}
	for _, body := range []string{`{}`, `{"limit": 5}`, `{"reason": "x"}`} {
		rec := postRetire(t, f, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400 so an omitted dry_run can never execute", body, rec.Code)
		}
	}
	if f.n != 0 {
		t.Fatalf("runner called %d times for bodies without dry_run", f.n)
	}
}

func TestLegacyWorkloadRetirementRejectsBadBodies(t *testing.T) {
	f := &fakeRetirer{}
	cases := map[string]string{
		"not json":      `nope`,
		"unknown field": `{"dry_run": true, "everything": true}`,
		"trailing data": `{"dry_run": true} {"dry_run": false}`,
		"wrong type":    `{"dry_run": "yes"}`,
	}
	for name, body := range cases {
		if rec := postRetire(t, f, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
	if f.n != 0 {
		t.Fatal("runner must not be called for a malformed body")
	}
}

func TestLegacyWorkloadRetirementMapsRunnerErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		body string
	}{
		{"invalid request keeps its text", fmt.Errorf("%w: reason is required when dry_run is false", legacyworkload.ErrInvalidRequest), http.StatusUnprocessableEntity, "reason is required"},
		{"unsupported backend", legacyworkload.ErrBackendUnvalidated, http.StatusNotImplemented, "Neo4j only"},
		{"no graph writer wired", legacyworkload.ErrWriterUnavailable, http.StatusServiceUnavailable, "no graph writer"},
	}
	for _, c := range cases {
		rec := postRetire(t, &fakeRetirer{err: c.err}, `{"dry_run": false, "reason": "r"}`)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: status = %d body = %s", c.name, rec.Code, rec.Body)
		}
	}
}

func TestLegacyWorkloadRetirementServerFaultNeverLeaksTheCause(t *testing.T) {
	secret := "bolt://user:hunter2@graph.internal:7687 constraint workload_id"
	rec := postRetire(t, &fakeRetirer{err: errors.New(secret)}, `{"dry_run": true}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") || strings.Contains(rec.Body.String(), "graph.internal") {
		t.Fatalf("5xx body leaked the cause: %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), legacyWorkloadRetirementFailedMessage) {
		t.Fatalf("body = %s, want the fixed message", rec.Body)
	}
}

func TestLegacyWorkloadRetirementOnlyAcceptsPost(t *testing.T) {
	f := &fakeRetirer{}
	h, _ := NewLegacyWorkloadRetirementHandler(f)
	mux := http.NewServeMux()
	h.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/retire-legacy-workloads", nil))
	if rec.Code != http.StatusMethodNotAllowed || f.n != 0 {
		t.Fatalf("status = %d calls = %d", rec.Code, f.n)
	}
}

func TestLegacyWorkloadRetirementMountsOnTheAdminMux(t *testing.T) {
	f := &fakeRetirer{resp: legacyworkload.RetireResponse{DryRun: true, Candidates: []legacyworkload.RetireCandidate{}}}
	h, _ := NewLegacyWorkloadRetirementHandler(f)
	mux, err := NewAdminMux(AdminMuxConfig{ServiceName: "reducer", LegacyWorkloadRetirement: h})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/retire-legacy-workloads", strings.NewReader(`{"dry_run": true}`)))
	if rec.Code != http.StatusOK || f.n != 1 {
		t.Fatalf("status = %d calls = %d body = %s", rec.Code, f.n, rec.Body)
	}
	bare, _ := NewAdminMux(AdminMuxConfig{ServiceName: "ingester"})
	rec = httptest.NewRecorder()
	bare.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/retire-legacy-workloads", strings.NewReader(`{"dry_run": true}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a runtime without the handler must 404, got %d", rec.Code)
	}
}
