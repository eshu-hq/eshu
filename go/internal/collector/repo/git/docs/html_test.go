// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func parseHTMLNode(t *testing.T, body string) *html.Node {
	t.Helper()

	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("html.Parse() error = %v", err)
	}
	return root
}

func TestHTMLHeadingLevel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		want   int
		wantOK bool
	}{
		{name: "h1", want: 1, wantOK: true},
		{name: "h6", want: 6, wantOK: true},
		{name: "h0", wantOK: false},
		{name: "h7", wantOK: false},
		{name: "h", wantOK: false},
		{name: "div", wantOK: false},
		{name: "H2", wantOK: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := htmlHeadingLevel(tc.name)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("htmlHeadingLevel(%q) = (%d, %v), want (%d, %v)",
					tc.name, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestHTMLNodeHelpers(t *testing.T) {
	t.Parallel()

	root := parseHTMLNode(t, `<html><body><h1 id=" Top ">Hi <b>there</b></h1><script>var x = 1;</script></body></html>`)
	var heading *html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "h1" {
			heading = node
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if heading == nil {
		t.Fatal("no h1 node found")
	}
	if got := htmlNodeAttr(heading, "ID"); got != "Top" {
		t.Fatalf("htmlNodeAttr(id) = %q, want trimmed Top", got)
	}
	if got := htmlNodeAttr(heading, "missing"); got != "" {
		t.Fatalf("htmlNodeAttr(missing) = %q, want empty", got)
	}
	if got := htmlNodeText(heading); got != "Hi there" {
		t.Fatalf("htmlNodeText() = %q, want joined text", got)
	}
	if got := htmlNodeText(root); strings.Contains(got, "var x") {
		t.Fatalf("htmlNodeText() = %q, want script skipped", got)
	}
}

func TestHTMLMalformedWarnings(t *testing.T) {
	t.Parallel()

	if warnings := htmlMalformedWarnings("<div><span></span></div>"); warnings != nil {
		t.Fatalf("balanced html warnings = %v, want nil", warnings)
	}
	warnings := htmlMalformedWarnings("<div><span")
	if len(warnings) != 1 || warnings[0] != "malformed_html" {
		t.Fatalf("warnings = %v, want [malformed_html]", warnings)
	}
}

func TestExtractHTMLDocumentation(t *testing.T) {
	t.Parallel()

	t.Run("headings links and script skip", func(t *testing.T) {
		t.Parallel()

		body := []byte(`<html><body><h1 id="top">Hi</h1><p>Hello <a href="https://e.com">link</a></p><script>var x = 1;</script></body></html>`)
		document, sections, links := extractHTMLDocumentation(testDocRepo(), "page.html", "d", "c", body)
		if document.Title != "Hi" || document.Format != "html" {
			t.Fatalf("document = %+v, want Hi/html", document)
		}
		if len(sections) != 1 || sections[0].SourceStartRef != "dom:#top" {
			t.Fatalf("sections = %+v, want one dom:#top section", sections)
		}
		for _, section := range sections {
			if strings.Contains(section.Content, "var x") {
				t.Fatalf("section content = %q, want script skipped", section.Content)
			}
		}
		if len(links) != 1 || links[0].TargetURI != "https://e.com" || links[0].TargetKind != "https" {
			t.Fatalf("links = %+v, want one https link", links)
		}
	})

	t.Run("link before heading opens a body section", func(t *testing.T) {
		t.Parallel()

		body := []byte(`<html><body><a href="docs/x.md">x</a></body></html>`)
		_, sections, links := extractHTMLDocumentation(testDocRepo(), "l.html", "d", "c", body)
		if len(sections) != 1 || sections[0].SectionAnchor != "body" {
			t.Fatalf("sections = %+v, want one body section", sections)
		}
		if len(links) != 1 || links[0].SectionID != sections[0].SectionID {
			t.Fatalf("links = %+v, want link pinned to the body section", links)
		}
	})

	t.Run("malformed html warns", func(t *testing.T) {
		t.Parallel()

		document, _, _ := extractHTMLDocumentation(testDocRepo(), "bad.html", "d", "c", []byte("<div><p oops"))
		if document.SourceMetadata["warning"] != "malformed_html" {
			t.Fatalf("metadata = %v, want exactly malformed_html warning", document.SourceMetadata)
		}
	})

	t.Run("nil root yields no drafts", func(t *testing.T) {
		t.Parallel()

		drafts, links := htmlSectionDrafts(nil, "x.html", nil)
		if drafts != nil || links != nil {
			t.Fatalf("htmlSectionDrafts(nil) = (%v, %v), want (nil, nil)", drafts, links)
		}
	})
}
