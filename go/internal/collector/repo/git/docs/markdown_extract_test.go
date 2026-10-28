// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestExtractMarkdownDocumentation(t *testing.T) {
	t.Parallel()

	t.Run("readme title sections links and blob uri", func(t *testing.T) {
		t.Parallel()

		body := []byte("# Hello\n\nSee [guide](docs/guide.md).\n\n## Sub\n\nMore.\n")
		document, sections, links := extractMarkdownDocumentationWithFormat(
			testDocRepo(), "README.md", "abc", "deadbee", body, "markdown")
		if document.Title != "Hello" {
			t.Fatalf("title = %q, want Hello", document.Title)
		}
		if document.DocumentType != "readme" {
			t.Fatalf("doc type = %q, want readme", document.DocumentType)
		}
		if len(sections) != 2 {
			t.Fatalf("sections = %d, want 2", len(sections))
		}
		if sections[1].ParentSectionID != sections[0].SectionID {
			t.Fatalf("sub parent = %q, want %q", sections[1].ParentSectionID, sections[0].SectionID)
		}
		if len(links) != 1 || links[0].TargetURI != "docs/guide.md" {
			t.Fatalf("links = %+v, want one guide link", links)
		}
		wantURI := "https://github.com/o/r/blob/deadbee/README.md"
		if document.CanonicalURI != wantURI {
			t.Fatalf("canonical URI = %q, want %q", document.CanonicalURI, wantURI)
		}
	})

	t.Run("quarto format is preserved", func(t *testing.T) {
		t.Parallel()

		document, sections, _ := extractMarkdownDocumentationWithFormat(
			testDocRepo(), "paper.qmd", "d", "c", []byte("# Q\n\nbody\n"), "quarto")
		if document.Format != "quarto" || len(sections) != 1 || sections[0].ContentFormat != "quarto" {
			t.Fatalf("document = %+v sections = %+v, want quarto throughout", document, sections)
		}
	})

	t.Run("non github remote uses fragment uri", func(t *testing.T) {
		t.Parallel()

		repo := repositoryidentity.Metadata{ID: "repository:g", RemoteURL: "https://gitlab.example.com/o/r"}
		document, _, _ := extractMarkdownDocumentationWithFormat(
			repo, "notes.md", "d", "c", []byte("x"), "markdown")
		wantURI := "https://gitlab.example.com/o/r#notes.md"
		if document.CanonicalURI != wantURI {
			t.Fatalf("canonical URI = %q, want %q", document.CanonicalURI, wantURI)
		}
	})

	t.Run("missing remote uses git scheme uri", func(t *testing.T) {
		t.Parallel()

		repo := repositoryidentity.Metadata{ID: "repository:abc"}
		document, _, _ := extractMarkdownDocumentationWithFormat(
			repo, "notes.md", "", "", []byte("x"), "markdown")
		if document.CanonicalURI != "git://abc/notes.md" {
			t.Fatalf("canonical URI = %q", document.CanonicalURI)
		}
		if document.RevisionID != "unknown" {
			t.Fatalf("revision = %q, want unknown", document.RevisionID)
		}
	})

	t.Run("oversize body warns on the document", func(t *testing.T) {
		t.Parallel()

		body := []byte(strings.Repeat("z", DocumentationMaxBodyBytes+1))
		document, _, _ := extractMarkdownDocumentationWithFormat(
			testDocRepo(), "big.md", "d", "c", body, "markdown")
		if document.SourceMetadata["warning"] != "body_truncated" {
			t.Fatalf("metadata = %v, want body_truncated warning", document.SourceMetadata)
		}
	})
}
