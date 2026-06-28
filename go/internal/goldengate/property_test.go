// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import "testing"

// TestValueAllowed locks the membership helper that backs both the edge- and
// node-property value checks.
func TestValueAllowed(t *testing.T) {
	cases := []struct {
		v       string
		allowed []string
		want    bool
	}{
		{"ansible", []string{"ansible", "gomod"}, true},
		{"terraform", []string{"ansible", "gomod"}, false},
		{"", []string{"ansible"}, false},
		{"ansible", nil, false}, // empty allowed set matches nothing
	}
	for _, tc := range cases {
		if got := valueAllowed(tc.v, tc.allowed); got != tc.want {
			t.Errorf("valueAllowed(%q, %v) = %v, want %v", tc.v, tc.allowed, got, tc.want)
		}
	}
}

// TestEvaluateEdgeProperty proves the absence-zero edge-property contract: among
// the matching (already evidence-narrowed) edges, an edge missing the property or
// carrying a value outside the allowed set is offending, and any offending edge
// fails the finding. A no-matching-edges case passes vacuously because the
// separate rc count finding guards existence.
func TestEvaluateEdgeProperty(t *testing.T) {
	rc := RequiredCorrelation{ID: "rc-x", Relationship: "DEPENDS_ON", FromLabel: "Repository", ToLabel: "Repository"}
	cases := []struct {
		name    string
		values  []string
		allowed []string
		wantOK  bool
	}{
		{"all carry property", []string{"ansible", "ansible"}, nil, true},
		{"one edge missing property", []string{"ansible", ""}, nil, false},
		{"all edges missing property", []string{"", ""}, nil, false},
		{"value outside allowed set", []string{"ansible", "terraform"}, []string{"ansible"}, false},
		{"all values in allowed set", []string{"ansible", "ansible"}, []string{"ansible", "gomod"}, true},
		{"no matching edges passes vacuously", nil, nil, true},
	}
	for _, tc := range cases {
		f := EvaluateEdgeProperty(rc, "source_tool", tc.values, tc.allowed, true)
		if f.OK != tc.wantOK {
			t.Errorf("%s: OK=%v want %v (detail %q)", tc.name, f.OK, tc.wantOK, f.Detail)
		}
		if !f.Required {
			t.Errorf("%s: edge-property finding must inherit required=true", tc.name)
		}
	}
}

// TestEvaluateNodeProperty proves the presence-positive node-property contract:
// at least MinimumCount nodes must carry a non-empty value (in the allowed set
// when pinned). Unlike edges, a label legitimately contains property-less nodes
// (a LICENSE has no language), so the gate asserts a floor of tagged nodes rather
// than the absence of any untagged node.
func TestEvaluateNodeProperty(t *testing.T) {
	rn := RequiredNode{ID: "rn-file", Label: "File", MinimumCount: 2}
	cases := []struct {
		name    string
		values  []string
		allowed []string
		wantOK  bool
	}{
		{"enough carriers", []string{"go", "python", "yaml"}, nil, true},
		{"exactly the floor", []string{"go", "python"}, nil, true},
		{"below the floor", []string{"go", ""}, nil, false},
		{"all empty", []string{"", ""}, nil, false},
		{"allowed set filters out non-canonical", []string{"go", "Golang"}, []string{"go"}, false},
		{"allowed set met", []string{"go", "python", "java"}, []string{"go", "python"}, true},
	}
	for _, tc := range cases {
		f := EvaluateNodeProperty(rn, "language", tc.values, tc.allowed)
		if f.OK != tc.wantOK {
			t.Errorf("%s: OK=%v want %v (detail %q)", tc.name, f.OK, tc.wantOK, f.Detail)
		}
		if !f.Required {
			t.Errorf("%s: node-property finding must be required", tc.name)
		}
	}
}
