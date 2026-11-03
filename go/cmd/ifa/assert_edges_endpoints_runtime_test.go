// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
)

// runtimeDeclarationTargetProps is the exact shape a live DECLARES_RUNTIME
// edge's target carries: a RuntimeDeclaration node MERGEd on its five-part
// key (repo_id, relative_path, line_number, runtime, order_index) with no uid
// and no id. line_number and order_index are int64 because that is what the
// graph returns for integer properties; the offline guard renders the same
// parts from int rows, so this shape also pins the numeric parity the shared
// derivation must hold.
func runtimeDeclarationTargetProps() map[string]any {
	return map[string]any{
		"repo_id":       "repository:eshu-fixture-runtime-pins",
		"relative_path": ".tool-versions",
		"line_number":   int64(1),
		"runtime":       "node",
		"order_index":   int64(0),
	}
}

// TestCompositeFallbackResolvesRuntimeDeclarationEndpoint proves the fifth
// endpointID fallback: a RuntimeDeclaration endpoint carrying neither uid nor
// id still resolves by its five-part MERGE key rather than reporting as an
// unmaterialized endpoint -- the exact shape a live DECLARES_RUNTIME edge's
// target carries. ToLabels is set to RuntimeDeclaration because the fallback
// is scoped to that label (see
// TestCompositeFallbackIsScopedToRuntimeDeclaration) -- a real live edge
// always carries its endpoint's labels, so this is the realistic shape, not
// a simplification the scoping happens to tolerate.
func TestCompositeFallbackResolvesRuntimeDeclarationEndpoint(t *testing.T) {
	t.Parallel()

	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("runtime_declaration")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(runtime_declaration): %v", err)
	}
	graph := fakeEdgeReader{edges: []graphdump.Edge{{
		Type:       "DECLARES_RUNTIME",
		FromLabels: []string{"Repository"},
		FromProps:  map[string]any{"id": "repository:eshu-fixture-runtime-pins"},
		ToLabels:   []string{"RuntimeDeclaration"},
		ToProps:    runtimeDeclarationTargetProps(),
	}}}
	expected := []materializededges.ExpectedEdge{
		{
			RelationshipType: "DECLARES_RUNTIME",
			SourceEntityID:   "repository:eshu-fixture-runtime-pins",
			TargetEntityID:   "repository:eshu-fixture-runtime-pins|.tool-versions|1|node|0",
		},
	}

	if err := assertMaterializedEdges(context.Background(), graph, "runtime_declaration", types, nil, nil, expected); err != nil {
		t.Fatalf("assertMaterializedEdges(keyless RuntimeDeclaration target) = %v, want nil; the five-part composite must resolve the endpoint", err)
	}
}

// TestCompositeFallbackIsScopedToRuntimeDeclaration proves the composite
// fallback does NOT apply globally: an endpoint of some OTHER label that
// lost its real identity (no uid, no id) but happens to carry the same five
// properties must still report as unmaterialized, not silently resolve by
// them. An unscoped fallback would mask exactly this failure mode -- a node
// in a uid/id-keyed family that lost its real identity would read as
// "identified" instead of "unmaterialized".
func TestCompositeFallbackIsScopedToRuntimeDeclaration(t *testing.T) {
	t.Parallel()

	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("runtime_declaration")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(runtime_declaration): %v", err)
	}
	graph := fakeEdgeReader{edges: []graphdump.Edge{{
		Type:       "DECLARES_RUNTIME",
		FromLabels: []string{"Repository"},
		FromProps:  map[string]any{"id": "repository:eshu-fixture-runtime-pins"},
		ToLabels:   []string{"SomeOtherLabel"},
		ToProps:    runtimeDeclarationTargetProps(), // identical properties -- must NOT resolve.
	}}}
	expected := []materializededges.ExpectedEdge{
		{
			RelationshipType: "DECLARES_RUNTIME",
			SourceEntityID:   "repository:eshu-fixture-runtime-pins",
			TargetEntityID:   "repository:eshu-fixture-runtime-pins|.tool-versions|1|node|0",
		},
	}

	err = assertMaterializedEdges(context.Background(), graph, "runtime_declaration", types, nil, nil, expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(non-RuntimeDeclaration endpoint with composite properties) = nil, want an endpoint-defect failure")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("error %q does not report the endpoint defect", err)
	}
}

// TestCompositeFallbackMissingKeyPartIsEndpointDefect proves the composite
// fallback fails closed: a RuntimeDeclaration-labeled endpoint missing any
// one of its five MERGE-key parts must report as unmaterialized, never
// render a partial join. A partial join would assert a wrong identity and
// pass a graph whose node the writer could never have MERGEd.
func TestCompositeFallbackMissingKeyPartIsEndpointDefect(t *testing.T) {
	t.Parallel()

	types, err := materializededges.MaterializedEdgeDomainEdgeTypes("runtime_declaration")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(runtime_declaration): %v", err)
	}
	parts := []string{"repo_id", "relative_path", "line_number", "runtime", "order_index"}
	for _, missing := range parts {
		t.Run("missing_"+missing, func(t *testing.T) {
			t.Parallel()

			props := runtimeDeclarationTargetProps()
			delete(props, missing)
			graph := fakeEdgeReader{edges: []graphdump.Edge{{
				Type:       "DECLARES_RUNTIME",
				FromLabels: []string{"Repository"},
				FromProps:  map[string]any{"id": "repository:eshu-fixture-runtime-pins"},
				ToLabels:   []string{"RuntimeDeclaration"},
				ToProps:    props,
			}}}
			expected := []materializededges.ExpectedEdge{
				{
					RelationshipType: "DECLARES_RUNTIME",
					SourceEntityID:   "repository:eshu-fixture-runtime-pins",
					TargetEntityID:   "repository:eshu-fixture-runtime-pins|.tool-versions|1|node|0",
				},
			}

			err := assertMaterializedEdges(context.Background(), graph, "runtime_declaration", types, nil, nil, expected)
			if err == nil {
				t.Fatalf("assertMaterializedEdges(RuntimeDeclaration endpoint missing %q) = nil, want an endpoint-defect failure", missing)
			}
			if !strings.Contains(err.Error(), "endpoint") {
				t.Errorf("error %q does not report the endpoint defect", err)
			}
		})
	}
}
