// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"testing"
)

func TestGitDocumentationFormatForPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		path   string
		want   GitDocumentationFormat
		wantOK bool
	}{
		{name: "markdown", path: "README.md", want: GitDocumentationFormat{Format: "markdown", Language: "markdown"}, wantOK: true},
		{name: "markdown uppercase ext", path: "README.MD", want: GitDocumentationFormat{Format: "markdown", Language: "markdown"}, wantOK: true},
		{name: "markdown mdx", path: "site/guide.mdx", want: GitDocumentationFormat{Format: "markdown", Language: "markdown"}, wantOK: true},
		{name: "markdown long ext", path: "notes.markdown", want: GitDocumentationFormat{Format: "markdown", Language: "markdown"}, wantOK: true},
		{name: "quarto", path: "paper.qmd", want: GitDocumentationFormat{Format: "quarto", Language: "quarto"}, wantOK: true},
		{name: "plain text", path: "notes.txt", want: GitDocumentationFormat{Format: "text", Language: "text"}, wantOK: true},
		{name: "requirements lockfile is not docs", path: "requirements.txt", wantOK: false},
		{name: "requirements dash variant is not docs", path: "requirements-dev.txt", wantOK: false},
		{name: "requirements underscore variant is not docs", path: "requirements_prod.txt", wantOK: false},
		{name: "constraints file is not docs", path: "constraints.txt", wantOK: false},
		{name: "restructuredtext", path: "doc.rst", want: GitDocumentationFormat{Format: "restructuredtext", Language: "restructuredtext"}, wantOK: true},
		{name: "asciidoc adoc", path: "doc.adoc", want: GitDocumentationFormat{Format: "asciidoc", Language: "asciidoc"}, wantOK: true},
		{name: "asciidoc long ext", path: "doc.asciidoc", want: GitDocumentationFormat{Format: "asciidoc", Language: "asciidoc"}, wantOK: true},
		{name: "html", path: "index.html", want: GitDocumentationFormat{Format: "html", Language: "html"}, wantOK: true},
		{name: "html short ext", path: "page.htm", want: GitDocumentationFormat{Format: "html", Language: "html"}, wantOK: true},
		{name: "notebook", path: "analysis.ipynb", want: GitDocumentationFormat{Format: "notebook", Language: "python"}, wantOK: true},
		{name: "docx under docs dir", path: "docs/plan.docx", want: GitDocumentationFormat{Format: "docx", Language: "docx"}, wantOK: true},
		{name: "docx by token", path: "architecture-review.docx", want: GitDocumentationFormat{Format: "docx", Language: "docx"}, wantOK: true},
		{name: "docx elsewhere is not docs", path: "report.docx", wantOK: false},
		{name: "pptx under docs dir", path: "docs/deck.pptx", want: GitDocumentationFormat{Format: "pptx", Language: "pptx"}, wantOK: true},
		{name: "pptx elsewhere is not docs", path: "deck.pptx", wantOK: false},
		{name: "mermaid mmd", path: "flow.mmd", want: GitDocumentationFormat{Format: "mermaid", Language: "mermaid"}, wantOK: true},
		{name: "mermaid long ext", path: "flow.mermaid", want: GitDocumentationFormat{Format: "mermaid", Language: "mermaid"}, wantOK: true},
		{name: "d2", path: "arch.d2", want: GitDocumentationFormat{Format: "d2", Language: "d2"}, wantOK: true},
		{name: "plantuml puml", path: "sys.puml", want: GitDocumentationFormat{Format: "plantuml", Language: "plantuml"}, wantOK: true},
		{name: "plantuml long ext", path: "sys.plantuml", want: GitDocumentationFormat{Format: "plantuml", Language: "plantuml"}, wantOK: true},
		{name: "drawio", path: "board.drawio", want: GitDocumentationFormat{Format: "drawio", Language: "drawio"}, wantOK: true},
		{name: "excalidraw", path: "board.excalidraw", want: GitDocumentationFormat{Format: "excalidraw", Language: "excalidraw"}, wantOK: true},
		{name: "svg", path: "img.svg", want: GitDocumentationFormat{Format: "svg", Language: "svg"}, wantOK: true},
		{name: "graphql sdl", path: "schema.graphql", want: GitDocumentationFormat{Format: "graphql_sdl", Language: "graphql"}, wantOK: true},
		{name: "graphql sdl plural", path: "schema.graphqls", want: GitDocumentationFormat{Format: "graphql_sdl", Language: "graphql"}, wantOK: true},
		{name: "openapi yaml", path: "openapi.yaml", want: GitDocumentationFormat{Format: "openapi", Language: "yaml"}, wantOK: true},
		{name: "swagger json", path: "swagger.json", want: GitDocumentationFormat{Format: "swagger", Language: "json"}, wantOK: true},
		{name: "asyncapi yml", path: "asyncapi.yml", want: GitDocumentationFormat{Format: "asyncapi", Language: "yaml"}, wantOK: true},
		{name: "plain yaml is not docs", path: "config.yaml", wantOK: false},
		{name: "plain json is not docs", path: "package.json", wantOK: false},
		{name: "csv by token", path: "docs/owners.csv", want: GitDocumentationFormat{Format: "csv", Language: "csv"}, wantOK: true},
		{name: "csv elsewhere is not docs", path: "data.csv", wantOK: false},
		{name: "tsv by token", path: "docs/matrix.tsv", want: GitDocumentationFormat{Format: "tsv", Language: "tsv"}, wantOK: true},
		{name: "xlsx by token", path: "docs/inventory.xlsx", want: GitDocumentationFormat{Format: "xlsx", Language: "xlsx"}, wantOK: true},
		{name: "xlsx elsewhere is not docs", path: "data.xlsx", wantOK: false},
		{name: "xls elsewhere is not docs", path: "data.xls", wantOK: false},
		{name: "zip under docs dir", path: "docs/evidence.zip", want: GitDocumentationFormat{Format: "zip", Language: "zip"}, wantOK: true},
		{name: "tar.gz under docs dir", path: "docs/notes.tar.gz", want: GitDocumentationFormat{Format: "tar.gz", Language: "tar.gz"}, wantOK: true},
		{name: "tgz under docs dir", path: "docs/notes.tgz", want: GitDocumentationFormat{Format: "tar.gz", Language: "tar.gz"}, wantOK: true},
		{name: "tar by token", path: "archive.tar", want: GitDocumentationFormat{Format: "tar", Language: "tar"}, wantOK: true},
		{name: "go source is not docs", path: "main.go", wantOK: false},
		{name: "empty path is not docs", path: "", wantOK: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := gitDocumentationFormatForPath(tc.path)
			if ok != tc.wantOK {
				t.Fatalf("gitDocumentationFormatForPath(%q) ok = %v, want %v", tc.path, ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Fatalf("gitDocumentationFormatForPath(%q) = %+v, want %+v", tc.path, got, tc.want)
			}
			if IsGitDocumentationPath(tc.path) != tc.wantOK {
				t.Fatalf("IsGitDocumentationPath(%q) = %v, want %v", tc.path, !tc.wantOK, tc.wantOK)
			}
		})
	}
}

func TestGitDocumentationSourceURIAndFormat(t *testing.T) {
	t.Parallel()

	if got := filepathToSourceURI(`docs\guide.md`); got != "docs/guide.md" {
		t.Fatalf("filepathToSourceURI backslash path = %q, want %q", got, "docs/guide.md")
	}
	uri, format, ok := GitDocumentationSourceURIAndFormat("docs/sub/../guide.md")
	if !ok {
		t.Fatal("GitDocumentationSourceURIAndFormat(unclean path) = false, want true")
	}
	if uri != "docs/guide.md" {
		t.Fatalf("source URI = %q, want %q", uri, "docs/guide.md")
	}
	if format.Format != "markdown" {
		t.Fatalf("format = %q, want markdown", format.Format)
	}

	for _, path := range []string{"../evil.md", "", "  ", "/abs.md"} {
		if _, _, ok := GitDocumentationSourceURIAndFormat(path); ok {
			t.Fatalf("GitDocumentationSourceURIAndFormat(%q) = true, want false", path)
		}
	}
}

func TestGitDocumentationFormatTruthAndArchive(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"mermaid", "d2", "plantuml", "drawio", "excalidraw", "svg"} {
		if gitDocumentationFormatEmitsTruth(GitDocumentationFormat{Format: format}) {
			t.Fatalf("gitDocumentationFormatEmitsTruth(%q) = true, want false", format)
		}
	}
	for _, format := range []string{"markdown", "text", "html", "openapi", "notebook", "docx"} {
		if !gitDocumentationFormatEmitsTruth(GitDocumentationFormat{Format: format}) {
			t.Fatalf("gitDocumentationFormatEmitsTruth(%q) = false, want true", format)
		}
	}
	for _, format := range []string{"zip", "tar", "tar.gz"} {
		if !gitDocumentationFormatIsArchive(GitDocumentationFormat{Format: format}) {
			t.Fatalf("gitDocumentationFormatIsArchive(%q) = false, want true", format)
		}
	}
	for _, format := range []string{"markdown", "zipx", ""} {
		if gitDocumentationFormatIsArchive(GitDocumentationFormat{Format: format}) {
			t.Fatalf("gitDocumentationFormatIsArchive(%q) = true, want false", format)
		}
	}
}
