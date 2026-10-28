// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"testing"

	factsdocs "github.com/eshu-hq/eshu/go/internal/facts/docs"
)

func TestMarkdownHeading(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		line        string
		wantLevel   int
		wantHeading string
		wantOK      bool
	}{
		{name: "h1", line: "# Title", wantLevel: 1, wantHeading: "Title", wantOK: true},
		{name: "h3 indented", line: "  ### Deep", wantLevel: 3, wantHeading: "Deep", wantOK: true},
		{name: "h6", line: "###### Six", wantLevel: 6, wantHeading: "Six", wantOK: true},
		{name: "seven hashes rejected", line: "####### Too many", wantOK: false},
		{name: "missing space rejected", line: "#Nope", wantOK: false},
		{name: "empty heading rejected", line: "# ", wantOK: false},
		{name: "plain text rejected", line: "just text", wantOK: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			level, heading, ok := markdownHeading(tc.line)
			if ok != tc.wantOK || level != tc.wantLevel || heading != tc.wantHeading {
				t.Fatalf("markdownHeading(%q) = (%d, %q, %v), want (%d, %q, %v)",
					tc.line, level, heading, ok, tc.wantLevel, tc.wantHeading, tc.wantOK)
			}
		})
	}
}

func TestMarkdownAnchor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"Hello World", "hello-world"},
		{"Hello, World!", "hello-world!"},
		{"Getting Started: Install", "getting-started-install"},
		{"a/b `code`", "a-b-code"},
		{"  spaced   out  ", "spaced-out"},
	} {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			if got := markdownAnchor(tc.in); got != tc.want {
				t.Fatalf("markdownAnchor(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMarkdownContentLines(t *testing.T) {
	t.Parallel()

	t.Run("frontmatter and fences are skipped with original line numbers", func(t *testing.T) {
		t.Parallel()

		body := "---\ntitle: x\n---\n# Real\n```go\ncode()\n```\ntext\n~~~\ntilde()\n~~~\nmore\n"
		lines := markdownContentLines(body)
		var texts []string
		var numbers []int
		for _, line := range lines {
			texts = append(texts, line.Text)
			numbers = append(numbers, line.Number)
		}
		wantTexts := []string{"# Real", "text", "more", ""}
		if len(texts) != len(wantTexts) {
			t.Fatalf("lines = %v, want %v", texts, wantTexts)
		}
		for i := range wantTexts {
			if texts[i] != wantTexts[i] {
				t.Fatalf("lines = %v, want %v", texts, wantTexts)
			}
		}
		if len(numbers) != 4 || numbers[0] != 4 || numbers[1] != 8 || numbers[2] != 12 || numbers[3] != 13 {
			t.Fatalf("line numbers = %v, want [4 8 12 13]", numbers)
		}
	})
}

func TestMarkdownSections(t *testing.T) {
	t.Parallel()

	lines := []markdownLine{
		{Number: 1, Text: "preamble"},
		{Number: 2, Text: "# Alpha"},
		{Number: 3, Text: "alpha body"},
		{Number: 4, Text: "## Beta"},
		{Number: 5, Text: "beta body"},
		{Number: 6, Text: "# Alpha"},
		{Number: 7, Text: "second alpha"},
	}
	sections := markdownSections("doc:1", "rev:1", "README.md", lines, "markdown")
	if len(sections) != 4 {
		t.Fatalf("sections = %d, want 4", len(sections))
	}
	if sections[0].HeadingText != "README" || sections[0].SectionAnchor != "body" {
		t.Fatalf("first section = %+v, want preamble body section", sections[0])
	}
	if sections[1].SectionID != "section:alpha" {
		t.Fatalf("second section ID = %q, want section:alpha", sections[1].SectionID)
	}
	if sections[2].SectionID != "section:beta" || sections[2].ParentSectionID != "section:alpha" {
		t.Fatalf("beta section = %+v, want parent section:alpha", sections[2])
	}
	if sections[3].SectionID != "section:alpha-2" {
		t.Fatalf("duplicate anchor section ID = %q, want section:alpha-2", sections[3].SectionID)
	}
	if sections[1].ParentSectionID != "" {
		t.Fatalf("h1 parent = %q, want empty", sections[1].ParentSectionID)
	}

	child := markdownSections("doc:1", "rev:1", "x.md",
		[]markdownLine{{Number: 1, Text: "# Top"}, {Number: 2, Text: "## Sub"}}, "markdown")
	if len(child) != 2 || child[1].ParentSectionID != child[0].SectionID {
		t.Fatalf("child parent = %+v, want parent %q", child, child[0].SectionID)
	}
}

func TestMarkdownLinks(t *testing.T) {
	t.Parallel()

	sections := []factsdocs.SectionPayload{{
		DocumentID: "doc:1",
		RevisionID: "rev:1",
		SectionID:  "section:alpha",
		Content:    "See [guide](docs/guide.md) and [site](https://example.com \"Title\") plus ![img](pic.png) and [empty]()",
	}}
	links := markdownLinks("README.md", sections)
	if len(links) != 2 {
		t.Fatalf("links = %d, want 2 (image and empty target skipped)", len(links))
	}
	if links[0].TargetURI != "docs/guide.md" || links[0].TargetKind != "source_path" {
		t.Fatalf("first link = %+v, want guide target", links[0])
	}
	if links[1].TargetURI != "https://example.com" || links[1].TargetKind != "https" {
		t.Fatalf("second link = %+v, want https target", links[1])
	}
}

func TestDocumentationTitleAndType(t *testing.T) {
	t.Parallel()

	sections := []factsdocs.SectionPayload{{HeadingText: ""}, {HeadingText: "Real Title"}}
	if got := documentationTitle("dir/file.md", sections); got != "Real Title" {
		t.Fatalf("title = %q, want first non-empty heading", got)
	}
	if got := documentationTitle("dir/my-doc.md", nil); got != "my-doc" {
		t.Fatalf("title = %q, want basename without ext", got)
	}
	for _, tc := range []struct{ path, fallback, want string }{
		{"README.md", "markdown", "readme"},
		{"docs/readme.txt", "text", "readme"},
		{"docs/adr/0001.md", "markdown", "adr"},
		{"runbook-deploy.md", "markdown", "runbook"},
		{"notes.md", "markdown", "markdown"},
	} {
		if got := documentationDocumentType(tc.path, tc.fallback); got != tc.want {
			t.Fatalf("documentationDocumentType(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestDocumentationLinkTargetKind(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ target, want string }{
		{"https://example.com/x", "https"},
		{"mailto:a@b.c", "mailto"},
		{"other/page.md", "source_path"},
		{"#anchor", "source_path"},
	} {
		if got := documentationLinkTargetKind(tc.target); got != tc.want {
			t.Fatalf("documentationLinkTargetKind(%q) = %q, want %q", tc.target, got, tc.want)
		}
	}
}

func TestDocumentationSectionMetadata(t *testing.T) {
	t.Parallel()

	got := documentationSectionMetadata("x.md", map[string]string{"k": " v ", "empty": "  "})
	if len(got) != 2 || got["path"] != "x.md" || got["k"] != "v" {
		t.Fatalf("metadata = %v, want trimmed path+k only", got)
	}
}
