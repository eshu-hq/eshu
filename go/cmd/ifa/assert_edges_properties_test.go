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

func submodulePropertyExpectation() []materializededges.ExpectedEdge {
	return []materializededges.ExpectedEdge{{
		RelationshipType: "PINS_SUBMODULE",
		SourceEntityID:   "repo-parent",
		TargetEntityID:   "repo-target",
		Identity:         map[string]string{"path": "vendor/libfoo"},
		Properties:       map[string]string{"pinned_sha": "sha-newer"},
	}}
}

func submodulePropertyReader(properties map[string]any) fakeEdgeReader {
	return fakeEdgeReader{edges: []graphdump.Edge{{
		Type:       "PINS_SUBMODULE",
		FromProps:  map[string]any{"id": "repo-parent"},
		ToProps:    map[string]any{"id": "repo-target"},
		Props:      properties,
		FromLabels: []string{"Repository"},
		ToLabels:   []string{"Repository"},
	}}}
}

func assertSubmoduleProperties(t *testing.T, properties map[string]any) error {
	t.Helper()
	return assertMaterializedEdges(
		context.Background(),
		submodulePropertyReader(properties),
		"submodule_pin_edges",
		map[string]struct{}{"PINS_SUBMODULE": {}},
		nil,
		map[string][]string{"PINS_SUBMODULE": {"path"}},
		submodulePropertyExpectation(),
	)
}

// TestAssertMaterializedEdgesRejectsWrongExpectedProperty proves a live edge
// with the right endpoints and MERGE identity still fails when an asserted
// SET-only property is wrong.
func TestAssertMaterializedEdgesRejectsWrongExpectedProperty(t *testing.T) {
	t.Parallel()

	err := assertSubmoduleProperties(t, map[string]any{
		"path": "vendor/libfoo", "pinned_sha": "sha-older",
	})
	if err == nil {
		t.Fatal("assertMaterializedEdges accepted the older pinned_sha")
	}
	if !strings.Contains(err.Error(), "pinned_sha=sha-newer") || !strings.Contains(err.Error(), "pinned_sha=sha-older") {
		t.Fatalf("error %q does not report the expected and live pinned_sha values", err)
	}
}

// TestAssertMaterializedEdgesRejectsMissingExpectedProperty proves omission is
// diagnosed as a property defect rather than only an opaque set mismatch.
func TestAssertMaterializedEdgesRejectsMissingExpectedProperty(t *testing.T) {
	t.Parallel()

	err := assertSubmoduleProperties(t, map[string]any{"path": "vendor/libfoo"})
	if err == nil {
		t.Fatal("assertMaterializedEdges accepted a missing pinned_sha")
	}
	if !strings.Contains(err.Error(), "asserted-property defects") || !strings.Contains(err.Error(), "pinned_sha") {
		t.Fatalf("error %q does not diagnose the missing pinned_sha", err)
	}
}

func TestAssertMaterializedEdgesAcceptsMatchingExpectedProperty(t *testing.T) {
	t.Parallel()

	err := assertSubmoduleProperties(t, map[string]any{
		"path": "vendor/libfoo", "pinned_sha": "sha-newer",
	})
	if err != nil {
		t.Fatalf("assertMaterializedEdges rejected matching pinned_sha: %v", err)
	}
}

// TestIndexExpectedPropertyKeysRejectsDistinctNULContainingKeySets prevents a
// delimiter-based comparison from treating ["a", "b"] and ["a\x00b"] as the
// same asserted-property key set for one MERGE identity.
func TestIndexExpectedPropertyKeysRejectsDistinctNULContainingKeySets(t *testing.T) {
	t.Parallel()

	base := materializededges.ExpectedEdge{
		RelationshipType: "PINS_SUBMODULE",
		SourceEntityID:   "repo-parent",
		TargetEntityID:   "repo-target",
		Identity:         map[string]string{"path": "vendor/libfoo"},
	}
	first := base
	first.Properties = map[string]string{"a": "one", "b": "two"}
	second := base
	second.Properties = map[string]string{"a\x00b": "three"}

	if _, err := indexExpectedPropertyKeys([]materializededges.ExpectedEdge{first, second}); err == nil {
		t.Fatal("indexExpectedPropertyKeys accepted inconsistent property key sets")
	}
}
