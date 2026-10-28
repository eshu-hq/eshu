// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"context"
	"testing"
)

func TestCleanDiagramLinkTarget(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"docs/guide.md", "docs/guide.md"},
		{"  \"docs/guide.md\"  ", "docs/guide.md"},
		{"", ""},
		{"https://example.com/x", ""},
		{"//example.com/x", ""},
		{"javascript:alert(1)", ""},
		{"../escape.md", ""},
		{"/abs.md", ""},
		{`back\slash.md`, ""},
		{"a:b", ""},
		{"page?token=abc", ""},
		{"someone@example.com", ""},
	} {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()

			if got := CleanDiagramLinkTarget(tc.raw); got != tc.want {
				t.Fatalf("CleanDiagramLinkTarget(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestExtractDiagramDocumentation(t *testing.T) {
	t.Parallel()

	body := []byte("flowchart TD\n  A[Start] --> B[End]\n")
	document, _, links := extractDiagramDocumentation(context.Background(), testDocRepo(), "flow.mmd", "d", "c", body, "mermaid")
	if document.Format != "mermaid" || document.DocumentType != "diagram" {
		t.Fatalf("document = %+v, want mermaid diagram", document)
	}
	if document.Title != "flow" {
		t.Fatalf("title = %q, want basename fallback", document.Title)
	}
	if len(links) != 0 {
		t.Fatalf("links = %v, want none for a link-free diagram", links)
	}

	document, _, _ = extractDiagramDocumentation(context.TODO(), testDocRepo(), "flow.mmd", "", "", body, "mermaid")
	if document.RevisionID != "unknown" {
		t.Fatalf("revision = %q, want unknown", document.RevisionID)
	}
}
