// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"strings"
	"testing"
)

const notebookFixture = `{
  "cells": [
    {"cell_type": "markdown", "id": "c1", "source": ["# Analysis\n", "See [guide](docs/guide.md).\n"]},
    {"cell_type": "code", "id": "c2", "source": ["print(1)\n"], "outputs": []},
    {"cell_type": "raw", "id": "c3", "source": ["raw text\n"]}
  ]
}`

func TestExtractNotebookDocumentation(t *testing.T) {
	t.Parallel()

	t.Run("cells become sections with markdown links", func(t *testing.T) {
		t.Parallel()

		document, sections, links := extractNotebookDocumentation(testDocRepo(), "analysis.ipynb", "d", "c", []byte(notebookFixture))
		if document.Format != "notebook" || document.Title != "Analysis" {
			t.Fatalf("document = %+v, want notebook/Analysis", document)
		}
		if len(sections) == 0 {
			t.Fatal("no sections extracted")
		}
		if sections[0].HeadingText != "Analysis" {
			t.Fatalf("first section = %+v, want Analysis heading", sections[0])
		}
		if len(links) != 1 || links[0].TargetURI != "docs/guide.md" {
			t.Fatalf("links = %+v, want one guide link", links)
		}
	})

	t.Run("malformed json warns without sections", func(t *testing.T) {
		t.Parallel()

		document, sections, links := extractNotebookDocumentation(testDocRepo(), "bad.ipynb", "d", "c", []byte("{nope"))
		if document.SourceMetadata["warning"] != "malformed_notebook" {
			t.Fatalf("metadata = %v, want malformed_notebook", document.SourceMetadata)
		}
		if sections != nil || links != nil {
			t.Fatalf("sections/links = (%v, %v), want nil", sections, links)
		}
	})

	t.Run("empty cells warn", func(t *testing.T) {
		t.Parallel()

		document, sections, _ := extractNotebookDocumentation(testDocRepo(), "empty.ipynb", "d", "c", []byte(`{"cells": []}`))
		if !strings.Contains(document.SourceMetadata["warning"], "empty_notebook") {
			t.Fatalf("metadata = %v, want empty_notebook", document.SourceMetadata)
		}
		if len(sections) != 0 {
			t.Fatalf("sections = %v, want none", sections)
		}
	})
}
