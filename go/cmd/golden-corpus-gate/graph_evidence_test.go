// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
)

// TestEdgeEvidenceContainsAll locks the Go-side membership filter that backs
// CountCorrelationWithEvidence. The filter runs in Go because NornicDB does not
// evaluate a WHERE clause on the relationship-count shape (verified against the
// pinned binary), so this pure-function coverage is the regression guard for the
// isolation logic itself, independent of any backend.
func TestEdgeEvidenceContainsAll(t *testing.T) {
	cases := []struct {
		name     string
		raw      any
		required []string
		want     bool
	}{
		{"bolt list any contains kind", []any{"ARGOCD_APPLICATION_SOURCE"}, []string{"ARGOCD_APPLICATION_SOURCE"}, true},
		{"missing kind", []any{"ARGOCD_APPLICATION_SOURCE"}, []string{"KUSTOMIZE_RESOURCE_REFERENCE"}, false},
		{"all-of multiple present", []any{"A", "B", "C"}, []string{"A", "C"}, true},
		{"all-of one missing", []any{"A", "B"}, []string{"A", "Z"}, false},
		{"string slice form", []string{"KUSTOMIZE_RESOURCE_REFERENCE"}, []string{"KUSTOMIZE_RESOURCE_REFERENCE"}, true},
		{"nil property", nil, []string{"A"}, false},
		{"non-list property", "ARGOCD_APPLICATION_SOURCE", []string{"ARGOCD_APPLICATION_SOURCE"}, false},
		{"non-string element ignored", []any{42, "A"}, []string{"A"}, true},
		{"empty required matches nothing", []any{"A"}, nil, false},
		{"empty required on empty list", nil, []string{}, false},
	}
	for _, tc := range cases {
		if got := edgeEvidenceContainsAll(tc.raw, tc.required); got != tc.want {
			t.Errorf("%s: edgeEvidenceContainsAll(%v, %v) = %v, want %v", tc.name, tc.raw, tc.required, got, tc.want)
		}
	}
}

// evidenceFilteredSnapshot is a hermetic snapshot carrying a single
// evidence-filtered required correlation, modelling "kustomize DEPLOYS_FROM"
// riding the shared, tool-agnostic DEPLOYS_FROM edge.
func evidenceFilteredSnapshot() Snapshot {
	return Snapshot{
		Graph: GraphSnapshot{
			RequiredCorrelations: []RequiredCorrelation{{
				ID:            "rc-test-kustomize",
				Relationship:  "DEPLOYS_FROM",
				FromLabel:     "Repository",
				ToLabel:       "Repository",
				MinimumCount:  1,
				EvidenceKinds: []string{"KUSTOMIZE_RESOURCE_REFERENCE"},
			}},
		},
	}
}

// TestCheckGraphEvidenceFilteredIsolatesVerb is the keystone assertion: a shared
// DEPLOYS_FROM edge exists in quantity (e.g. emitted by ArgoCD), but none carries
// the kustomize evidence kind. An evidence-filtered rc MUST fail here. If the
// gate fell back to the bare (From)-[Rel]->(To) count it would falsely pass —
// exactly the false-green the predicate exists to prevent.
func TestCheckGraphEvidenceFilteredIsolatesVerb(t *testing.T) {
	c := fakeCounter{
		corr:   map[string]int64{"Repository|DEPLOYS_FROM|Repository": 5},
		corrEv: map[string]int64{}, // no edge carries KUSTOMIZE_RESOURCE_REFERENCE
	}
	var r Report
	if err := checkGraph(context.Background(), c, evidenceFilteredSnapshot(), true,
		map[string]bool{"rc-test-kustomize": true}, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("evidence-filtered rc must fail when the shared edge exists but carries no matching evidence kind (no bare-count fallback)")
	}
}

// TestCheckGraphEvidenceFilteredPassesWhenKindPresent confirms the same rc passes
// once at least one edge carries the verb's signature evidence kind.
func TestCheckGraphEvidenceFilteredPassesWhenKindPresent(t *testing.T) {
	c := fakeCounter{
		corr: map[string]int64{"Repository|DEPLOYS_FROM|Repository": 5},
		corrEv: map[string]int64{
			"Repository|DEPLOYS_FROM|Repository|KUSTOMIZE_RESOURCE_REFERENCE": 1,
		},
	}
	var r Report
	if err := checkGraph(context.Background(), c, evidenceFilteredSnapshot(), true,
		map[string]bool{"rc-test-kustomize": true}, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected pass when a matching evidence kind is present; findings: %+v", r.Findings)
	}
}
