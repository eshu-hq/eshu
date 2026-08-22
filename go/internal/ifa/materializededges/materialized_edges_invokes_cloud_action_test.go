// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestInvokesCloudActionRowsToExpectedEdgesIsOneToOne pins the family's
// simplest-of-the-three shape: unlike HANDLES_ROUTE (dedupe collapse) and
// RUNS_IN (fan-out), each upsert row maps directly to exactly one edge, with
// no family-specific dedupe or workload-projection step.
func TestInvokesCloudActionRowsToExpectedEdgesIsOneToOne(t *testing.T) {
	t.Parallel()

	rows := []reducer.SharedProjectionIntentRow{
		{
			ProjectionDomain: reducer.DomainInvokesCloudAction,
			Payload: map[string]any{
				"function_id": "content-entity:fn1",
				"action_id":   "cloud-action:s3:putobject",
			},
		},
		{
			ProjectionDomain: reducer.DomainInvokesCloudAction,
			Payload: map[string]any{
				"function_id": "content-entity:fn2",
				"action_id":   "cloud-action:dynamodb:getitem",
			},
		},
	}

	edges := invokesCloudActionRowsToExpectedEdges(rows, "INVOKES_CLOUD_ACTION")
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges (one per row), got %d: %+v", len(edges), edges)
	}
	want := []ExpectedEdge{
		{RelationshipType: "INVOKES_CLOUD_ACTION", SourceEntityID: "content-entity:fn1", TargetEntityID: "cloud-action:s3:putobject"},
		{RelationshipType: "INVOKES_CLOUD_ACTION", SourceEntityID: "content-entity:fn2", TargetEntityID: "cloud-action:dynamodb:getitem"},
	}
	for i, e := range want {
		if !reflect.DeepEqual(edges[i], e) {
			t.Errorf("edge %d = %+v, want %+v", i, edges[i], e)
		}
	}
}

// TestSingleRegistryEdgeTypeRejectsNonSingleton proves singleRegistryEdgeType
// fails closed rather than silently picking an arbitrary type when a
// registry carries anything other than exactly one relationship type -- the
// invariant every symbol-runtime guard depends on to avoid hardcoding a
// relationship-type literal that could drift from the writer registry.
func TestSingleRegistryEdgeTypeRejectsNonSingleton(t *testing.T) {
	t.Parallel()

	if _, err := singleRegistryEdgeType("some_family", map[string]struct{}{}); err == nil {
		t.Fatal("expected an error for an empty registry, got nil")
	}
	if _, err := singleRegistryEdgeType("some_family", map[string]struct{}{"A": {}, "B": {}}); err == nil {
		t.Fatal("expected an error for a multi-type registry, got nil")
	}
	got, err := singleRegistryEdgeType("some_family", map[string]struct{}{"HANDLES_ROUTE": {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "HANDLES_ROUTE" {
		t.Fatalf("got %q, want %q", got, "HANDLES_ROUTE")
	}
}
