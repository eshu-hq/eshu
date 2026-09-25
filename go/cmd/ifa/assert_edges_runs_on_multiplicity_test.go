// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	materialized "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/materialized"
)

// runsOnEdge builds a live-shaped RUNS_ON edge: id-keyed endpoints, the
// canonical identity_key both writers MERGE on, and the given evidence_source
// (omitted when empty, the shape #6671 observed on the racing copy).
func runsOnEdge(instance, platform, evidenceSource string) graphdump.Edge {
	props := map[string]any{"identity_key": "canonical"}
	if evidenceSource != "" {
		props["evidence_source"] = evidenceSource
	}
	return graphdump.Edge{
		Type:       "RUNS_ON",
		FromLabels: []string{"WorkloadInstance"},
		FromProps:  map[string]any{"id": instance},
		ToLabels:   []string{"Platform"},
		ToProps:    map[string]any{"id": platform},
		Props:      props,
	}
}

func assertRepoDependencyRunsOn(t *testing.T, graph []graphdump.Edge, expected []materializededges.ExpectedEdge) error {
	t.Helper()
	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("repo_dependency")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(repo_dependency): %v", err)
	}
	endpoints, ok := materialized.MaterializedEdgeEndpointLabels("repo_dependency")
	if !ok {
		t.Fatal("repo_dependency carries no endpoint constraints")
	}
	identity, err := materialized.MaterializedEdgeIdentityProperties("repo_dependency")
	if err != nil {
		t.Fatalf("MaterializedEdgeIdentityProperties(repo_dependency): %v", err)
	}
	return assertMaterializedEdges(context.Background(), fakeEdgeReader{edges: graph}, "repo_dependency", types, endpoints, identity, expected)
}

// TestRunsOnDuplicateIsVisibleRegardlessOfEvidenceStamp is the #6671 guard.
//
// Both RUNS_ON writers MERGE one shared canonical edge per
// (WorkloadInstance, Platform). The family's provenance filter keeps only
// resolver-stamped edges for the exact-set comparison, so a second edge on the
// same pair that carries no stamp, or the workload stamp, used to be skipped
// before multiplicity was counted — the duplicate the issue found on a live
// barrier run was invisible to the gate. Every such pair must now fail.
func TestRunsOnDuplicateIsVisibleRegardlessOfEvidenceStamp(t *testing.T) {
	t.Parallel()

	expected := []materializededges.ExpectedEdge{{
		RelationshipType: "RUNS_ON",
		SourceEntityID:   "inst-a",
		TargetEntityID:   "plat-a",
		Identity:         map[string]string{"identity_key": "canonical"},
	}}

	cases := map[string]string{
		"unstamped duplicate":        "",
		"workload-stamped duplicate": reducer.EvidenceSourceWorkloads,
		"resolver-stamped duplicate": reducer.CrossRepoEvidenceSource,
	}
	for name, secondStamp := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			graph := []graphdump.Edge{
				runsOnEdge("inst-a", "plat-a", reducer.CrossRepoEvidenceSource),
				runsOnEdge("inst-a", "plat-a", secondStamp),
			}
			err := assertRepoDependencyRunsOn(t, graph, expected)
			if err == nil {
				t.Fatal("two RUNS_ON edges on one (WorkloadInstance, Platform) pair passed the repo_dependency assertion; the duplicate is invisible to the gate")
			}
			if !strings.Contains(err.Error(), "RUNS_ON|inst-a|plat-a") || !strings.Contains(err.Error(), "graph=2") {
				t.Fatalf("assertion error does not name the duplicated pair and its count:\n%v", err)
			}
		})
	}
}

// TestRunsOnDuplicateOfAnotherWritersEdgeFails covers the pair this family
// does not own: two copies of a workload-stamped edge are still a broken
// shared canonical identity, and the gate must not wave them through merely
// because neither copy carries the resolver stamp.
func TestRunsOnDuplicateOfAnotherWritersEdgeFails(t *testing.T) {
	t.Parallel()

	graph := []graphdump.Edge{
		runsOnEdge("inst-a", "plat-a", reducer.CrossRepoEvidenceSource),
		runsOnEdge("inst-w", "plat-w", reducer.EvidenceSourceWorkloads),
		runsOnEdge("inst-w", "plat-w", ""),
	}
	expected := []materializededges.ExpectedEdge{{
		RelationshipType: "RUNS_ON",
		SourceEntityID:   "inst-a",
		TargetEntityID:   "plat-a",
		Identity:         map[string]string{"identity_key": "canonical"},
	}}
	err := assertRepoDependencyRunsOn(t, graph, expected)
	if err == nil {
		t.Fatal("a duplicated workload-owned RUNS_ON pair passed; one canonical edge per pair is shared by both writers")
	}
	if !strings.Contains(err.Error(), "RUNS_ON|inst-w|plat-w") {
		t.Fatalf("assertion error does not name the duplicated workload pair:\n%v", err)
	}
}

// TestRunsOnOneEdgePerPairStillPartitionsByWriter is the negative control:
// with exactly one edge per pair, the workload-owned edge on a different pair
// stays out of this family's exact set, exactly as before #6671.
func TestRunsOnOneEdgePerPairStillPartitionsByWriter(t *testing.T) {
	t.Parallel()

	graph := []graphdump.Edge{
		runsOnEdge("inst-a", "plat-a", reducer.CrossRepoEvidenceSource),
		runsOnEdge("inst-w", "plat-w", reducer.EvidenceSourceWorkloads),
	}
	expected := []materializededges.ExpectedEdge{{
		RelationshipType: "RUNS_ON",
		SourceEntityID:   "inst-a",
		TargetEntityID:   "plat-a",
		Identity:         map[string]string{"identity_key": "canonical"},
	}}
	if err := assertRepoDependencyRunsOn(t, graph, expected); err != nil {
		t.Fatalf("one RUNS_ON per pair failed the assertion: %v", err)
	}
}
