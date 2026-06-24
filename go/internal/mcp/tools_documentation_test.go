// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestDocumentationToolsAreRegisteredAndRouted(t *testing.T) {
	t.Parallel()

	tools := documentationTools()
	if got, want := len(tools), 4; got != want {
		t.Fatalf("len(documentationTools()) = %d, want %d", got, want)
	}

	cases := []struct {
		name       string
		args       map[string]any
		wantMethod string
		wantPath   string
	}{
		{
			name:       "list_documentation_findings",
			args:       map[string]any{"status": "contradicted", "limit": 25},
			wantMethod: "GET",
			wantPath:   "/api/v0/documentation/findings",
		},
		{
			name:       "list_documentation_facts",
			args:       map[string]any{"scope_id": "docs-scope", "fact_kind": "documentation_document", "limit": 25},
			wantMethod: "GET",
			wantPath:   "/api/v0/documentation/facts",
		},
		{
			name:       "get_documentation_evidence_packet",
			args:       map[string]any{"finding_id": "finding:docs:1"},
			wantMethod: "GET",
			wantPath:   "/api/v0/documentation/findings/finding:docs:1/evidence-packet",
		},
		{
			name:       "check_documentation_evidence_packet_freshness",
			args:       map[string]any{"packet_id": "doc-packet:1", "packet_version": "1"},
			wantMethod: "GET",
			wantPath:   "/api/v0/documentation/evidence-packets/doc-packet:1/freshness",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			route, err := resolveRoute(tc.name, tc.args)
			if err != nil {
				t.Fatalf("resolveRoute() error = %v, want nil", err)
			}
			if got := route.method; got != tc.wantMethod {
				t.Fatalf("method = %q, want %q", got, tc.wantMethod)
			}
			if got := route.path; got != tc.wantPath {
				t.Fatalf("path = %q, want %q", got, tc.wantPath)
			}
		})
	}
}

func TestListDocumentationFactsSchemaIncludesBoundedFilters(t *testing.T) {
	t.Parallel()

	tools := documentationTools()
	schema := tools[1].InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	for _, name := range []string{"fact_kind", "scope_id", "generation_id", "source_id", "document_id", "section_id", "q", "limit", "cursor"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("list_documentation_facts InputSchema missing routed filter %q", name)
		}
	}
}

func TestListDocumentationFactsRouteIncludesScopeAndSearchFilters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_documentation_facts", map[string]any{
		"fact_kind":   "documentation_section",
		"scope_id":    "docs-scope",
		"document_id": "doc:confluence:123",
		"section_id":  "body",
		"q":           "deployment",
		"limit":       25,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	for _, key := range []string{"fact_kind", "scope_id", "document_id", "section_id", "q", "limit"} {
		if got := route.query[key]; got == "" {
			t.Fatalf("route.query[%q] = empty, want routed filter", key)
		}
	}
}

func TestListDocumentationFactsRouteIncludesDiagramReadbackFilters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_documentation_facts", map[string]any{
		"fact_kind":   "section",
		"repo":        "repository:r_diagram",
		"source_id":   "doc-source:git:repository:r_diagram",
		"document_id": "doc:git:repository:r_diagram:docs/architecture.mmd",
		"q":           "Documentation API",
		"limit":       float64(10),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/documentation/facts"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"fact_kind":   "section",
		"repo":        "repository:r_diagram",
		"source_id":   "doc-source:git:repository:r_diagram",
		"document_id": "doc:git:repository:r_diagram:docs/architecture.mmd",
		"q":           "Documentation API",
		"limit":       "10",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%q] = %#v, want %#v", key, got, want)
		}
	}
}

func TestListDocumentationFindingsSchemaIncludesRoutedFilters(t *testing.T) {
	t.Parallel()

	tools := documentationTools()
	schema := tools[0].InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	for _, name := range []string{"freshness_state", "updated_since", "scope_id", "generation_id", "repo"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("list_documentation_findings InputSchema missing routed filter %q", name)
		}
	}
}

func TestListDocumentationFindingsRouteIncludesPersistedScopeFilters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_documentation_findings", map[string]any{
		"scope_id":      "docs-scope",
		"generation_id": "gen-1",
		"repo":          "acme/api",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	for _, key := range []string{"scope_id", "generation_id", "repo"} {
		if got := route.query[key]; got == "" {
			t.Fatalf("route.query[%q] = empty, want routed filter", key)
		}
	}
}
