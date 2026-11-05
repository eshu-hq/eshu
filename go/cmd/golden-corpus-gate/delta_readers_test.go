// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"

	gg "github.com/eshu-hq/eshu/go/internal/goldengate"
)

type stubDeltaReaderGraph struct {
	edges  []gg.DeltaReaderObservedEdge
	err    error
	repoID string
	calls  int
}

func (s *stubDeltaReaderGraph) DeltaReaderRepoEdges(_ context.Context, repoID string) ([]gg.DeltaReaderObservedEdge, error) {
	s.calls++
	s.repoID = repoID
	return s.edges, s.err
}

type stubDeltaReaderGenerations struct {
	generationID string
	isDelta      bool
	err          error
	scopeID      string
}

func (s *stubDeltaReaderGenerations) ActiveGeneration(_ context.Context, scopeID string) (string, bool, error) {
	s.scopeID = scopeID
	return s.generationID, s.isDelta, s.err
}

func gateDeltaReaderSpec() *gg.DeltaReaderAssertions {
	return &gg.DeltaReaderAssertions{
		RepositoryID: "repository:r_0c077ab3",
		ScopeID:      "git-repository-scope:repository:r_0c077ab3",
		Edges: []gg.DeltaReaderEdge{{
			ID: "dr-1", Reader: "code_call_symbols", Relationship: "CALLS",
			FromLabel: "Function", FromName: "main", ToLabel: "Function", ToName: "helper",
		}},
	}
}

func TestEvaluateDeltaReadersReadsTheSpecScopeAndRepository(t *testing.T) {
	graph := &stubDeltaReaderGraph{edges: []gg.DeltaReaderObservedEdge{{
		Relationship: "CALLS", FromLabels: []string{"Function"}, FromName: "main",
		ToLabels: []string{"Function"}, ToName: "helper",
	}}}
	generations := &stubDeltaReaderGenerations{generationID: "gen-delta", isDelta: true}
	var r Report
	if err := evaluateDeltaReaders(context.Background(), gateDeltaReaderSpec(), gg.DeltaReaderStageDelta, true, generations, graph, &r); err != nil {
		t.Fatalf("evaluateDeltaReaders: %v", err)
	}
	if r.Failed() {
		t.Fatalf("report failed: %+v", r.Findings)
	}
	if generations.scopeID != "git-repository-scope:repository:r_0c077ab3" || graph.repoID != "repository:r_0c077ab3" {
		t.Fatalf("read scope=%q repo=%q, want the spec's", generations.scopeID, graph.repoID)
	}
}

func TestEvaluateDeltaReadersAbsentSectionFailsWithoutBackendReads(t *testing.T) {
	graph := &stubDeltaReaderGraph{}
	var r Report
	if err := evaluateDeltaReaders(context.Background(), nil, gg.DeltaReaderStageDelta, true, &stubDeltaReaderGenerations{}, graph, &r); err != nil {
		t.Fatalf("evaluateDeltaReaders: %v", err)
	}
	if !r.Failed() || graph.calls != 0 {
		t.Fatalf("absent section: failed=%v graph calls=%d, want a failure and no reads", r.Failed(), graph.calls)
	}
}

func TestEvaluateDeltaReadersPropagatesReadErrors(t *testing.T) {
	boom := errors.New("boom")
	var r Report
	err := evaluateDeltaReaders(context.Background(), gateDeltaReaderSpec(), gg.DeltaReaderStageDelta, true,
		&stubDeltaReaderGenerations{err: boom}, &stubDeltaReaderGraph{}, &r)
	if !errors.Is(err, boom) {
		t.Fatalf("generation read error = %v, want boom", err)
	}
	err = evaluateDeltaReaders(context.Background(), gateDeltaReaderSpec(), gg.DeltaReaderStageDelta, true,
		&stubDeltaReaderGenerations{generationID: "g", isDelta: true}, &stubDeltaReaderGraph{err: boom}, &r)
	if !errors.Is(err, boom) {
		t.Fatalf("graph read error = %v, want boom", err)
	}
}

func TestDeltaReadersPhaseIsOptInAndReadsTheSnapshot(t *testing.T) {
	if phaseSet("all")["delta-readers"] {
		t.Error("phaseSet(all) must not include the opt-in delta-readers phase")
	}
	got := phaseSet("graph,delta-readers")
	if !got["delta-readers"] || !got["graph"] {
		t.Errorf("phaseSet(graph,delta-readers) = %v", got)
	}
	if !needsSnapshot(map[string]bool{"delta-readers": true}) {
		t.Error("delta-readers reads graph.delta_reader_assertions, so it needs the snapshot")
	}
}

func TestDeltaReaderFlagsParse(t *testing.T) {
	o, err := parseFlags([]string{"-delta-reader-stage=full", "-delta-reader-ignore-expected-failures"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if o.deltaReaderStage != "full" || !o.deltaReaderIgnoreExpectedFailures {
		t.Fatalf("parsed stage=%q ignore=%v", o.deltaReaderStage, o.deltaReaderIgnoreExpectedFailures)
	}
	o, err = parseFlags(nil)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if o.deltaReaderStage != string(gg.DeltaReaderStageDelta) || o.deltaReaderIgnoreExpectedFailures {
		t.Fatalf("default stage=%q ignore=%v, want delta and false", o.deltaReaderStage, o.deltaReaderIgnoreExpectedFailures)
	}
}

// TestCommittedSnapshotDeltaReaderSectionIsValid loads the committed B-12
// snapshot and proves its delta-reader section passes spec validation and
// carries at least one control edge with no expected failure, so the delta
// stage can never pass on expected failures alone.
func TestCommittedSnapshotDeltaReaderSectionIsValid(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	spec := snap.Graph.DeltaReaderAssertions
	if spec == nil {
		t.Fatal("committed snapshot has no graph.delta_reader_assertions")
	}
	var r Report
	gg.EvaluateDeltaReaderAssertions(spec, gg.DeltaReaderStageFull, gg.DeltaReaderObservation{ActiveGenerationID: "g"}, true, &r)
	controls := 0
	for _, f := range r.Findings {
		if f.Check == "spec" {
			t.Fatalf("committed section is invalid: %s", f.Detail)
		}
	}
	for _, e := range spec.Edges {
		if e.ExpectedFailure == nil {
			controls++
		}
	}
	if controls == 0 {
		t.Fatal("every delta-reader edge carries expected_failure; keep at least one control edge")
	}
}
