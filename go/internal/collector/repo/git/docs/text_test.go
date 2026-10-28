// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func testDocRepo() repositoryidentity.Metadata {
	return repositoryidentity.Metadata{
		ID:        "repository:test",
		Name:      "Test",
		RemoteURL: "https://github.com/o/r",
	}
}

func TestAsciidocHeading(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		line        string
		wantLevel   int
		wantHeading string
		wantOK      bool
	}{
		{name: "level one", line: "= Title", wantLevel: 1, wantHeading: "Title", wantOK: true},
		{name: "level two", line: "== Section", wantLevel: 2, wantHeading: "Section", wantOK: true},
		{name: "missing space rejected", line: "=Nope", wantOK: false},
		{name: "bare marker rejected", line: "=", wantOK: false},
		{name: "empty heading rejected", line: "= ", wantOK: false},
		{name: "plain text rejected", line: "text", wantOK: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			level, heading, ok := asciidocHeading(tc.line)
			if ok != tc.wantOK || level != tc.wantLevel || heading != tc.wantHeading {
				t.Fatalf("asciidocHeading(%q) = (%d, %q, %v), want (%d, %q, %v)",
					tc.line, level, heading, ok, tc.wantLevel, tc.wantHeading, tc.wantOK)
			}
		})
	}
}

func TestRstHeading(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		text        string
		underline   string
		wantLevel   int
		wantHeading string
		wantOK      bool
	}{
		{name: "equals is level one", text: "Title", underline: "=====", wantLevel: 1, wantHeading: "Title", wantOK: true},
		{name: "dash is level two", text: "Sub", underline: "---", wantLevel: 2, wantHeading: "Sub", wantOK: true},
		{name: "tilde is level three", text: "Sub", underline: "~~~", wantLevel: 3, wantHeading: "Sub", wantOK: true},
		{name: "caret is level four", text: "Sub", underline: "^^^", wantLevel: 4, wantHeading: "Sub", wantOK: true},
		{name: "short underline rejected", text: "Long title", underline: "==", wantOK: false},
		{name: "mixed markers rejected", text: "Title", underline: "=-===", wantOK: false},
		{name: "unknown marker rejected", text: "Title", underline: "*****", wantOK: false},
		{name: "empty heading rejected", text: "", underline: "===", wantOK: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			level, heading, ok := rstHeading(tc.text, tc.underline)
			if ok != tc.wantOK || level != tc.wantLevel || heading != tc.wantHeading {
				t.Fatalf("rstHeading(%q, %q) = (%d, %q, %v), want (%d, %q, %v)",
					tc.text, tc.underline, level, heading, ok, tc.wantLevel, tc.wantHeading, tc.wantOK)
			}
		})
	}
}

func TestUnsupportedDocumentationDirective(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		line string
		want bool
	}{
		{".. include:: other.rst", true},
		{"include::other.adoc[]", true},
		{"  .. include:: x", true},
		{"#include <x>", false},
		{"included text", false},
		{"", false},
	} {
		if got := unsupportedDocumentationDirective(tc.line); got != tc.want {
			t.Fatalf("unsupportedDocumentationDirective(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestNormalizeDocumentationURL(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"https://example.com/a.,;:", "https://example.com/a"},
		{"  https://example.com/a  ", "https://example.com/a"},
		{"docs/guide.md", "docs/guide.md"},
	} {
		if got := normalizeDocumentationURL(tc.in); got != tc.want {
			t.Fatalf("normalizeDocumentationURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractTextDocumentation(t *testing.T) {
	t.Parallel()

	t.Run("plain text title links and canonical uri", func(t *testing.T) {
		t.Parallel()

		body := []byte("Title\n\nSee https://example.com/a and https://example.com/a again\n")
		document, sections, links := extractTextDocumentation(testDocRepo(), "notes.txt", "abc", "deadbee", body, "text")
		if document.DocumentID != "doc:git:repository:test:notes.txt" {
			t.Fatalf("document ID = %q", document.DocumentID)
		}
		if document.RevisionID != "deadbee" || document.Title != "Title" || document.Format != "text" {
			t.Fatalf("document = %+v", document)
		}
		if document.ContentHash != "abc" {
			t.Fatalf("content hash = %q, want digest passthrough", document.ContentHash)
		}
		if document.SourceMetadata["source_revision"] != "deadbee" {
			t.Fatalf("metadata = %v, want source_revision", document.SourceMetadata)
		}
		wantURI := "https://github.com/o/r/blob/deadbee/notes.txt"
		if document.CanonicalURI != wantURI {
			t.Fatalf("canonical URI = %q, want %q", document.CanonicalURI, wantURI)
		}
		if len(sections) != 1 || sections[0].SectionID != "section:title" {
			t.Fatalf("sections = %+v, want single title section", sections)
		}
		if len(links) != 1 || links[0].TargetURI != "https://example.com/a" {
			t.Fatalf("links = %+v, want one deduped link", links)
		}
	})

	t.Run("single line falls back to body section", func(t *testing.T) {
		t.Parallel()

		_, sections, _ := extractTextDocumentation(testDocRepo(), "n.txt", "", "", []byte("just one line"), "text")
		if len(sections) != 1 || sections[0].SectionAnchor != "body" {
			t.Fatalf("sections = %+v, want single body section", sections)
		}
	})

	t.Run("missing ids fall back to unknown revision", func(t *testing.T) {
		t.Parallel()

		document, _, _ := extractTextDocumentation(testDocRepo(), "n.txt", "", "", []byte("x"), "text")
		if document.RevisionID != "unknown" {
			t.Fatalf("revision = %q, want unknown", document.RevisionID)
		}
		if _, ok := document.SourceMetadata["source_revision"]; ok {
			t.Fatalf("metadata = %v, want no source_revision", document.SourceMetadata)
		}
	})

	t.Run("asciidoc headings links and directives", func(t *testing.T) {
		t.Parallel()

		body := []byte("= Doc\n\nlink:https://example.com[x]\ninclude::other[]\n")
		document, sections, links := extractTextDocumentation(testDocRepo(), "doc.adoc", "d", "c", body, "asciidoc")
		if document.Format != "asciidoc" {
			t.Fatalf("format = %q", document.Format)
		}
		if len(sections) != 1 || sections[0].HeadingText != "Doc" {
			t.Fatalf("sections = %+v, want one Doc section", sections)
		}
		if !sections[0].ContainsWarnings {
			t.Fatal("section with include directive reports no warnings")
		}
		if len(links) != 1 || links[0].TargetURI != "https://example.com" {
			t.Fatalf("links = %+v, want one asciidoc link", links)
		}
	})

	t.Run("rst underline heading and bare url", func(t *testing.T) {
		t.Parallel()

		body := []byte("Head\n====\n\nbody https://example.org\n")
		_, sections, links := extractTextDocumentation(testDocRepo(), "doc.rst", "d", "c", body, "restructuredtext")
		if len(sections) != 1 || sections[0].HeadingText != "Head" {
			t.Fatalf("sections = %+v, want one Head section", sections)
		}
		if len(links) != 1 || links[0].TargetURI != "https://example.org" {
			t.Fatalf("links = %+v, want one bare-url link", links)
		}
	})
}
