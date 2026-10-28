// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"testing"
)

func TestGraphQLDescriptionLine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		line   string
		want   string
		wantOK bool
	}{
		{`"""A user."""`, "A user.", true},
		{`"Short."`, "Short.", true},
		{`"""x`, "", false},
		{`type Query {`, "", false},
	} {
		tc := tc
		t.Run(tc.line, func(t *testing.T) {
			t.Parallel()

			got, ok := graphqlDescriptionLine(tc.line)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("graphqlDescriptionLine(%q) = (%q, %v), want (%q, %v)",
					tc.line, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestExtractGraphQLSDLDocumentation(t *testing.T) {
	t.Parallel()

	body := []byte("type Query {\n  \"\"\"A user.\"\"\"\n  user(id: ID): User\n  version: String\n}\n")
	document, sections, links := extractGraphQLSDLDocumentation(testDocRepo(), "schema.graphql", "d", "c", body)
	if document.Format != "graphql_sdl" {
		t.Fatalf("format = %q", document.Format)
	}
	if links != nil {
		t.Fatalf("links = %v, want nil", links)
	}
	if len(sections) != 2 {
		t.Fatalf("sections = %d, want 2 fields", len(sections))
	}
	if sections[0].HeadingText != "Query.user" {
		t.Fatalf("first heading = %q, want Query.user", sections[0].HeadingText)
	}
	if sections[0].Content != "A user.\nSignature: user(id: ID): User" {
		t.Fatalf("first content = %q", sections[0].Content)
	}
	if sections[1].HeadingText != "Query.version" {
		t.Fatalf("second heading = %q, want Query.version", sections[1].HeadingText)
	}
}
