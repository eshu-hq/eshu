// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func TestDeadCodeIncomingEntityIDsCompleteReachabilitySnapshotSkipsLegacyDeadCluster(t *testing.T) {
	t.Parallel()

	store := &coverageReachabilityIncomingStore{
		coverageByRepo: map[string]codeReachabilityCoverage{
			"repo-1": {Available: true, Truncated: false},
		},
		legacyByRepo: map[string]map[string]deadCodeIncomingEdge{
			"repo-1": {
				"dead-a": {MaxConfidence: codeprovenance.LegacyConfidence},
				"dead-b": {MaxConfidence: codeprovenance.LegacyConfidence},
			},
		},
	}
	handler := &CodeHandler{Content: store}

	incoming, err := handler.deadCodeIncomingEntityIDs(context.Background(), []map[string]any{
		{"entity_id": "dead-a", "repo_id": "repo-1"},
		{"entity_id": "dead-b", "repo_id": "repo-1"},
	})
	if err != nil {
		t.Fatalf("deadCodeIncomingEntityIDs() error = %v, want nil", err)
	}
	if len(incoming) != 0 {
		t.Fatalf("incoming = %#v, want no incoming for dead cluster unreachable by roots", incoming)
	}
	if got, want := store.reachabilityCalls, 1; got != want {
		t.Fatalf("reachability calls = %d, want %d", got, want)
	}
	if got, want := store.coverageCalls, 1; got != want {
		t.Fatalf("coverage calls = %d, want %d", got, want)
	}
	if got, want := store.legacyCalls, 0; got != want {
		t.Fatalf("legacy incoming calls = %d, want %d", got, want)
	}
}

func TestDeadCodeIncomingEntityIDsTruncatedReachabilitySnapshotFallsBack(t *testing.T) {
	t.Parallel()

	store := &coverageReachabilityIncomingStore{
		coverageByRepo: map[string]codeReachabilityCoverage{
			"repo-1": {Available: true, Truncated: true},
		},
		legacyByRepo: map[string]map[string]deadCodeIncomingEdge{
			"repo-1": {
				"maybe-live": {
					MaxConfidence: codeprovenance.LegacyConfidence,
					Method:        codeprovenance.MethodUnspecified,
				},
			},
		},
	}
	handler := &CodeHandler{Content: store}

	incoming, err := handler.deadCodeIncomingEntityIDs(context.Background(), []map[string]any{
		{"entity_id": "maybe-live", "repo_id": "repo-1"},
	})
	if err != nil {
		t.Fatalf("deadCodeIncomingEntityIDs() error = %v, want nil", err)
	}
	if got, want := incoming["maybe-live"].Method, codeprovenance.MethodUnspecified; got != want {
		t.Fatalf("incoming[maybe-live].Method = %q, want %q", got, want)
	}
	if got, want := store.legacyCalls, 1; got != want {
		t.Fatalf("legacy incoming calls = %d, want %d", got, want)
	}
}

// TestDeadCodeIncomingEntityIDsHiddenOnlyEntryStillRunsTheLegacyProbe pins the
// difference between an answer and a marker.
//
// When the materialized reachability snapshot is truncated, the
// producer-anchored legacy probe runs for every entity the snapshot did not
// answer for. An entity whose only snapshot entry is the hidden-consumer marker
// has no answer: the marker says a consumer exists outside the caller's grant,
// and says nothing about the granted same-repo callers the legacy probe reads.
// Treating the marker as coverage skipped that probe, so a strong granted edge
// went unread and the candidate stayed ambiguous instead of being dropped as
// reachable.
func TestDeadCodeIncomingEntityIDsHiddenOnlyEntryStillRunsTheLegacyProbe(t *testing.T) {
	t.Parallel()

	store := &coverageReachabilityIncomingStore{
		coverageByRepo: map[string]codeReachabilityCoverage{
			"repo-1": {Available: true, Truncated: true},
		},
		incomingByRepo: map[string]map[string]deadCodeIncomingEdge{
			"repo-1": {
				"hidden-only": {HiddenConsumer: true},
				"answered":    {MaxConfidence: codeprovenance.LegacyConfidence, Method: codeprovenance.MethodUnspecified},
			},
		},
		legacyByRepo: map[string]map[string]deadCodeIncomingEdge{
			"repo-1": {
				"hidden-only": {
					MaxConfidence: codeprovenance.Confidence(codeprovenance.MethodImportBinding),
					Method:        codeprovenance.MethodImportBinding,
				},
			},
		},
	}
	handler := &CodeHandler{Content: store}

	incoming, err := handler.deadCodeIncomingEntityIDs(context.Background(), []map[string]any{
		{"entity_id": "hidden-only", "repo_id": "repo-1"},
		{"entity_id": "answered", "repo_id": "repo-1"},
	})
	if err != nil {
		t.Fatalf("deadCodeIncomingEntityIDs() error = %v, want nil", err)
	}
	if got, want := store.legacyCalls, 1; got != want {
		t.Fatalf("legacy incoming calls = %d, want %d", got, want)
	}
	if got, want := store.legacyAskedFor["repo-1"], []string{"hidden-only"}; !slices.Equal(got, want) {
		t.Fatalf("legacy probe asked for %#v, want %#v: a hidden-only entry is a marker, not coverage", got, want)
	}
	edge := incoming["hidden-only"]
	if got, want := edge.Method, codeprovenance.MethodImportBinding; got != want {
		t.Fatalf("incoming[hidden-only].Method = %q, want %q: the legacy edge must be merged in", got, want)
	}
	if !edge.HiddenConsumer {
		t.Fatalf("incoming[hidden-only] = %#v, want the hidden-consumer marker kept alongside the legacy edge", edge)
	}
	if got, want := incoming["answered"].Method, codeprovenance.MethodUnspecified; got != want {
		t.Fatalf("incoming[answered].Method = %q, want %q: an answered entity must not be re-probed", got, want)
	}
}

type coverageReachabilityIncomingStore struct {
	fakePortContentStore
	incomingByRepo    map[string]map[string]deadCodeIncomingEdge
	coverageByRepo    map[string]codeReachabilityCoverage
	legacyByRepo      map[string]map[string]deadCodeIncomingEdge
	legacyAskedFor    map[string][]string
	reachabilityCalls int
	coverageCalls     int
	legacyCalls       int
}

func (s *coverageReachabilityIncomingStore) CodeReachabilityIncomingEntityIDs(
	_ context.Context,
	repoID string,
	entityIDs []string,
	_ []string,
) (map[string]deadCodeIncomingEdge, error) {
	s.reachabilityCalls++
	incoming := make(map[string]deadCodeIncomingEdge)
	for _, entityID := range entityIDs {
		if edge, ok := s.incomingByRepo[repoID][entityID]; ok {
			incoming[entityID] = edge
		}
	}
	return incoming, nil
}

func (s *coverageReachabilityIncomingStore) CodeReachabilityCoverage(
	_ context.Context,
	repoID string,
) (codeReachabilityCoverage, error) {
	s.coverageCalls++
	if coverage, ok := s.coverageByRepo[repoID]; ok {
		return coverage, nil
	}
	return codeReachabilityCoverage{}, nil
}

func (s *coverageReachabilityIncomingStore) DeadCodeIncomingEntityIDs(
	_ context.Context,
	repoID string,
	entityIDs []string,
) (map[string]deadCodeIncomingEdge, error) {
	s.legacyCalls++
	if s.legacyAskedFor == nil {
		s.legacyAskedFor = make(map[string][]string)
	}
	s.legacyAskedFor[repoID] = append(s.legacyAskedFor[repoID], entityIDs...)
	incoming := make(map[string]deadCodeIncomingEdge)
	for _, entityID := range entityIDs {
		if edge, ok := s.legacyByRepo[repoID][entityID]; ok {
			incoming[entityID] = edge
		}
	}
	return incoming, nil
}
