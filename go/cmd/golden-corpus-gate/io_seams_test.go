// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeDrainQuerier returns a scripted sequence of counts so the poll loop can be
// tested without Postgres.
type fakeDrainQuerier struct {
	seq   []DrainCounts
	i     int
	errOn int // 1-based index to return an error on; 0 disables
}

func (f *fakeDrainQuerier) Counts(_ context.Context) (DrainCounts, error) {
	f.i++
	if f.errOn == f.i {
		return DrainCounts{}, errors.New("boom")
	}
	if f.i-1 < len(f.seq) {
		return f.seq[f.i-1], nil
	}
	return f.seq[len(f.seq)-1], nil
}

func TestPollUntilDrainedConvergesAfterRetries(t *testing.T) {
	q := &fakeDrainQuerier{seq: []DrainCounts{
		{FactWorkItemsResidual: 5, SharedIntentsNonterminal: 3},
		{FactWorkItemsResidual: 1, SharedIntentsNonterminal: 1},
		{}, // drained
	}}
	counts, ok, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 0, time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatalf("expected drained, got counts %+v", counts)
	}
}

func TestPollUntilDrainedWaitsForPopulation(t *testing.T) {
	// Both queues read empty from the start, but the reducer has not emitted the
	// required domain until the third poll. The poll must NOT converge on the
	// early empty reads (the premature-convergence bug).
	q := &fakeDrainQuerier{seq: []DrainCounts{
		{PopulatedDomainsPresent: 0}, // empty + unpopulated — must not converge
		{PopulatedDomainsPresent: 0},
		{PopulatedDomainsPresent: 1}, // reducer emitted; empty + populated — converge
	}}
	counts, ok, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 1, time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatalf("expected convergence after population, got %+v", counts)
	}
	if q.i < 3 {
		t.Errorf("converged after %d polls; must wait for population (>=3)", q.i)
	}
}

func TestPollUntilDrainedTimesOutWhenNeverPopulated(t *testing.T) {
	// Queues are empty but the reducer never emits the required domain → the gate
	// must NOT report drained (it would otherwise pass on an unreduced pipeline).
	q := &fakeDrainQuerier{seq: []DrainCounts{{PopulatedDomainsPresent: 0}}}
	_, ok, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 1, 5*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Fatal("must not converge when the required domain is never populated")
	}
}

func TestPollUntilDrainedTimeoutReturnsLastCounts(t *testing.T) {
	q := &fakeDrainQuerier{seq: []DrainCounts{{FactWorkItemsResidual: 9}}}
	counts, drained, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 0, 5*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if drained {
		t.Fatal("expected timeout, got drained")
	}
	if counts.FactWorkItemsResidual != 9 {
		t.Errorf("want last residual 9, got %d", counts.FactWorkItemsResidual)
	}
}

func TestPollUntilDrainedPropagatesQueryError(t *testing.T) {
	q := &fakeDrainQuerier{seq: []DrainCounts{{}}, errOn: 1}
	if _, _, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 0, time.Second, time.Millisecond); err == nil {
		t.Fatal("expected query error to propagate")
	}
}

// fakeCounter satisfies graphCounter from maps keyed by identifier.
type fakeCounter struct {
	nodes map[string]int64
	edges map[string]int64
	corr  map[string]int64 // key "from|rel|to"
	// corrEv keys "from|rel|to|kind1,kind2" for evidence-filtered correlation
	// counts. A miss returns 0, modelling a shared edge that exists (corr > 0)
	// but carries no edge produced by the requested verb's evidence kind.
	corrEv map[string]int64
	err    error
}

func (f fakeCounter) CountNodes(_ context.Context, label string) (int64, error) {
	return f.nodes[label], f.err
}

func (f fakeCounter) CountEdges(_ context.Context, rel string) (int64, error) {
	return f.edges[rel], f.err
}

func (f fakeCounter) CountCorrelation(_ context.Context, from, rel, to string) (int64, error) {
	return f.corr[from+"|"+rel+"|"+to], f.err
}

func (f fakeCounter) CountCorrelationWithEvidence(_ context.Context, from, rel, to string, kinds []string) (int64, error) {
	return f.corrEv[from+"|"+rel+"|"+to+"|"+strings.Join(kinds, ",")], f.err
}

func TestCheckGraphRequiredOnlyPassesOnExistence(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	c := fakeCounter{corr: map[string]int64{
		"Repository|CORRELATES_DEPLOYABLE_UNIT|Repository": 2,
		"Function|RUNS_IN|Workload":                        1,
		"Repository|DEPENDS_ON|Repository":                 7,
		"KubernetesWorkload|RUNS_IMAGE|OciImageManifest":   1,
	}}
	var r Report
	if err := checkGraph(context.Background(), c, snap, true, map[string]bool{"rc-1": true, "rc-3": true}, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected pass; findings: %+v", r.Findings)
	}
}

func TestCheckGraphAdvisoryCorrelationDoesNotBlock(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	// rc-1 and rc-3 present; rc-2 and rc-4 absent but advisory (not in blocking set).
	c := fakeCounter{corr: map[string]int64{
		"Repository|CORRELATES_DEPLOYABLE_UNIT|Repository": 1,
		"Repository|DEPENDS_ON|Repository":                 1,
	}}
	var r Report
	if err := checkGraph(context.Background(), c, snap, true, map[string]bool{"rc-1": true, "rc-3": true}, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("advisory rc-2/rc-4 absence must not fail the minimal gate; findings: %+v", r.Findings)
	}
}

func TestCheckGraphRequiredFailsWhenCorrelationMissing(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	var r Report
	if err := checkGraph(context.Background(), fakeCounter{}, snap, true, map[string]bool{"rc-1": true, "rc-3": true}, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("expected failure when blocking correlations are absent")
	}
}

func TestQueryClientChecksHTTPShapes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"repositories":[{"id":"r1","name":"a"}]}`))
		case "/api/v0/status/operator-control-plane":
			_, _ = w.Write([]byte(`{"version":"1","as_of":"now","health":"ok","queue":{},"reducer_domains":[],"collector_families":[],"dead_letters":[],"retry_policies":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	client := newQueryClient(srv.URL, "k")
	var r Report
	if err := checkQuery(context.Background(), client, snap, &r); err != nil {
		t.Fatalf("checkQuery err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected query shapes to pass; findings: %+v", r.Findings)
	}
}

func TestQueryClientFailsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	var r Report
	if err := checkQuery(context.Background(), newQueryClient(srv.URL, ""), snap, &r); err != nil {
		t.Fatalf("checkQuery err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("expected failure on HTTP 500")
	}
}

func TestParseHTTPShapeKey(t *testing.T) {
	if _, p, err := parseHTTPShapeKey("GET /api/v0/repositories"); err != nil || p != "/api/v0/repositories" {
		t.Errorf("GET parse = %q, %v", p, err)
	}
	if _, _, err := parseHTTPShapeKey("POST /x"); err == nil {
		t.Error("POST must be rejected")
	}
	if _, _, err := parseHTTPShapeKey("bogus"); err == nil {
		t.Error("malformed key must be rejected")
	}
}

func TestSplitCSVTrimsAndDropsEmpty(t *testing.T) {
	got := splitCSV("rc-1, rc-3 ,, code_calls")
	want := []string{"rc-1", "rc-3", "code_calls"}
	if len(got) != len(want) {
		t.Fatalf("splitCSV = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitCSV[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if len(splitCSV("")) != 0 || len(splitCSV("  ,  ")) != 0 {
		t.Error("empty / whitespace-only input must yield no elements")
	}
}

func TestPhaseSet(t *testing.T) {
	if s := phaseSet("all"); !s["drains"] || !s["graph"] || !s["query"] || !s["timing"] {
		t.Errorf("all => %+v", s)
	}
	s := phaseSet("drains,graph")
	if !s["drains"] || !s["graph"] || s["query"] || s["timing"] {
		t.Errorf("subset => %+v", s)
	}
}
