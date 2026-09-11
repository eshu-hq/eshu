// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
)

func TestRunAssertGCPProjectEdgeScopesCommand(t *testing.T) {
	originalReader := openAssertGCPProjectEdgeScopesReader
	t.Cleanup(func() {
		openAssertGCPProjectEdgeScopesReader = originalReader
	})

	openAssertGCPProjectEdgeScopesReader = func(context.Context) (graphdump.Reader, func(), error) {
		return fakeEdgeReader{edges: ifaGCPProjectEdges(99, 1, 2)}, func() {}, nil
	}

	var stdout bytes.Buffer
	args := []string{"-synth-seed", "99", "-synth-projects", "1", "-synth-resources", "2"}
	if err := runAssertGCPProjectEdgeScopesCommand(context.Background(), args, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runAssertGCPProjectEdgeScopesCommand(valid): %v", err)
	}
	if got := stdout.String(); got != "ifa assert-gcp-project-edge-scopes: checked=124 cross_scope=0\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestRunAssertGCPProjectEdgeScopesCommandRejectsEmptyAndExtraArgs(t *testing.T) {
	originalReader := openAssertGCPProjectEdgeScopesReader
	t.Cleanup(func() {
		openAssertGCPProjectEdgeScopesReader = originalReader
	})
	openAssertGCPProjectEdgeScopesReader = func(context.Context) (graphdump.Reader, func(), error) {
		return fakeEdgeReader{}, func() {}, nil
	}

	if err := runAssertGCPProjectEdgeScopesCommand(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "edge count = 0, want 63") {
		t.Fatalf("empty graph error = %v", err)
	}
	if err := runAssertGCPProjectEdgeScopesCommand(context.Background(), []string{"unexpected"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("extra argument accepted")
	}
	if err := runAssertGCPProjectEdgeScopesCommand(context.Background(), []string{"-synth-projects", "0"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "must be positive") {
		t.Fatalf("invalid project count error = %v", err)
	}
}

func TestIFAGCPProjectScopeExpectationsFollowFixtureOverrides(t *testing.T) {
	t.Parallel()

	expected, err := ifaGCPProjectScopeExpectations(7, 2, 5)
	if err != nil {
		t.Fatalf("ifaGCPProjectScopeExpectations: %v", err)
	}
	for scopeID, projectID := range map[string]string{
		"gcp:project:acme-demo-gcp-00:seed:7": "acme-demo-gcp-00",
		"gcp:project:acme-demo-gcp-01:seed:7": "acme-demo-gcp-01",
	} {
		if got := expected[scopeID]; got.ProjectID != projectID || got.EdgeCount != 4 {
			t.Fatalf("expectation %q = %+v, want project %q with 4 edges", scopeID, got, projectID)
		}
	}
}

func ifaGCPProjectEdges(seed, projects, resources int) []graphdump.Edge {
	expected, err := ifaGCPProjectScopeExpectations(seed, projects, resources)
	if err != nil {
		panic(err)
	}
	edges := make([]graphdump.Edge, 0)
	for scopeID, expectation := range expected {
		for range expectation.EdgeCount {
			edges = append(edges, graphdump.Edge{
				Type:       "GCP_contains",
				FromLabels: []string{"CloudResource"},
				FromProps:  map[string]any{"account_id": expectation.ProjectID},
				ToLabels:   []string{"CloudResource"},
				ToProps:    map[string]any{"account_id": expectation.ProjectID},
				Props: map[string]any{
					"scope_id":        scopeID,
					"evidence_source": "reducer/gcp-relationships",
				},
			})
		}
	}
	return edges
}
