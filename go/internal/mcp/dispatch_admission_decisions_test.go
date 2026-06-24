// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsAdmissionDecisionsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_admission_decisions", map[string]any{
		"domain":           "deployable_unit",
		"scope_id":         "git-repository-scope:team/api",
		"generation_id":    "generation-1",
		"state":            "missing_evidence",
		"anchor_kind":      "repository",
		"anchor_id":        "repo://team/api",
		"include_evidence": true,
		"limit":            float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/evidence/admission-decisions"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"domain":           "deployable_unit",
		"scope_id":         "git-repository-scope:team/api",
		"generation_id":    "generation-1",
		"state":            "missing_evidence",
		"anchor_kind":      "repository",
		"anchor_id":        "repo://team/api",
		"include_evidence": "true",
		"limit":            "25",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %#v, want %#v", key, got, want)
		}
	}
}
