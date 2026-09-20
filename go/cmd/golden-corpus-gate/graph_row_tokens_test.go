// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	gg "github.com/eshu-hq/eshu/go/internal/goldengate"
)

func (f fakeCounter) ListGraphElementProperties(context.Context) ([]gg.GraphElementProperties, error) {
	return f.elements, f.err
}

// TestCheckGraphFailsOnUnresolvedRowToken proves checkGraph runs the #6782
// row-token check in both gate modes and that a junk token fails the gate.
func TestCheckGraphFailsOnUnresolvedRowToken(t *testing.T) {
	t.Parallel()

	for _, requiredOnly := range []bool{true, false} {
		c := fakeCounter{elements: []gg.GraphElementProperties{
			{Kind: "edge", Name: "DEPENDS_ON", Properties: map[string]any{"source_tool": "row.source_tool"}},
		}}
		var r Report
		if err := checkGraph(context.Background(), c, Snapshot{}, requiredOnly, nil, nil, &r); err != nil {
			t.Fatalf("checkGraph(requiredOnly=%t) error = %v", requiredOnly, err)
		}
		var found *Finding
		for i := range r.Findings {
			if r.Findings[i].Check == "unresolved_row_tokens" {
				found = &r.Findings[i]
			}
		}
		if found == nil || found.OK || !found.Required {
			t.Fatalf("requiredOnly=%t: row-token finding = %+v, want a required failure", requiredOnly, found)
		}
		if !strings.Contains(found.Detail, "edge DEPENDS_ON.source_tool=row.source_tool (1)") {
			t.Fatalf("detail does not name the offending property: %s", found.Detail)
		}
	}
}

// TestCheckGraphPropagatesRowTokenReadError keeps a failed read from passing
// silently as an empty graph.
func TestCheckGraphPropagatesRowTokenReadError(t *testing.T) {
	t.Parallel()

	c := fakeCounter{err: errors.New("bolt down")}
	var r Report
	if err := checkUnresolvedRowTokens(context.Background(), c, &r); err == nil {
		t.Fatal("checkUnresolvedRowTokens() error = nil, want the read error")
	}
}

func TestGraphElementNameSortsNodeLabels(t *testing.T) {
	t.Parallel()

	if got := graphElementName([]any{"Workload", "Entity"}); got != "Entity:Workload" {
		t.Fatalf("graphElementName(labels) = %q, want Entity:Workload", got)
	}
	if got := graphElementName("CALLS"); got != "CALLS" {
		t.Fatalf("graphElementName(type) = %q, want CALLS", got)
	}
}
