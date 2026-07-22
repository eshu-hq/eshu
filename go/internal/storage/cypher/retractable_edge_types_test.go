// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

func TestRetractableEdgeTypesAreSortedRegisteredAndDefensive(t *testing.T) {
	got := RetractableEdgeTypes()
	if len(got) == 0 {
		t.Fatal("RetractableEdgeTypes returned no edge types")
	}

	sorted := append([]string(nil), got...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(got, sorted) {
		t.Fatalf("RetractableEdgeTypes not sorted:\n got %v\nwant %v", got, sorted)
	}

	seen := make(map[string]struct{}, len(got))
	for _, edgeType := range got {
		if !edgetype.IsRegistered(edgeType) {
			t.Fatalf("RetractableEdgeTypes includes unregistered edge type %q", edgeType)
		}
		if _, exists := seen[edgeType]; exists {
			t.Fatalf("RetractableEdgeTypes includes duplicate edge type %q", edgeType)
		}
		seen[edgeType] = struct{}{}
	}

	got[0] = "BROKEN"
	again := RetractableEdgeTypes()
	if again[0] == "BROKEN" {
		t.Fatal("RetractableEdgeTypes returned mutable backing storage")
	}
}

func TestRetractableEdgeTypesCoverStaticRetractFamilies(t *testing.T) {
	got := make(map[string]struct{})
	for _, edgeType := range RetractableEdgeTypes() {
		got[edgeType] = struct{}{}
	}

	for _, edgeType := range []edgetype.EdgeType{
		edgetype.Calls,
		edgetype.Inherits,
		edgetype.Contains,
		edgetype.HelmValueReference,
		edgetype.DefinesJob,
		edgetype.AtlantisDependsOn,
		edgetype.Triggers,
		edgetype.ReferencesTable,
		edgetype.WritesTo,
		edgetype.RunsImage,
		edgetype.EvidencesRepositoryRelationship,
		edgetype.TargetsEnvironment,
		edgetype.CanPerform,
		edgetype.AllowsIngress,
		edgetype.TaintFlowsTo,
		edgetype.SecretsIamUsesServiceAccount,
	} {
		if _, exists := got[string(edgeType)]; !exists {
			t.Fatalf("RetractableEdgeTypes missing %s", edgeType)
		}
	}
}
