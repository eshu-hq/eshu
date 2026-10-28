// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"strings"
	"testing"
)

func TestExtractSpreadsheetDocumentation(t *testing.T) {
	t.Parallel()

	t.Run("csv headers rows and cell urls", func(t *testing.T) {
		t.Parallel()

		body := []byte("owner,service,docs\nanna,api,https://example.com/runbook\nbo,web,plain\n")
		document, sections, links := extractSpreadsheetDocumentation(testDocRepo(), "docs/owners.csv", "d", "c", body, "csv")
		if document.Format != "csv" {
			t.Fatalf("format = %q", document.Format)
		}
		if len(sections) != 1 {
			t.Fatalf("sections = %d, want 1", len(sections))
		}
		if !strings.Contains(sections[0].Content, "anna") {
			t.Fatalf("content = %q, want sampled rows", sections[0].Content)
		}
		if len(links) != 1 || links[0].TargetURI != "https://example.com/runbook" {
			t.Fatalf("links = %+v, want one runbook link", links)
		}
	})

	t.Run("tsv parses on tabs", func(t *testing.T) {
		t.Parallel()

		body := []byte("a\tb\n1\t2\n")
		_, sections, _ := extractSpreadsheetDocumentation(testDocRepo(), "docs/matrix.tsv", "d", "c", body, "tsv")
		if len(sections) != 1 {
			t.Fatalf("sections = %d, want 1", len(sections))
		}
	})

	t.Run("malformed csv warns without sections", func(t *testing.T) {
		t.Parallel()

		body := []byte("a,b\n\"unterminated\n")
		document, sections, links := extractSpreadsheetDocumentation(testDocRepo(), "docs/owners.csv", "d", "c", body, "csv")
		if document.SourceMetadata["warning"] != "malformed_spreadsheet" {
			t.Fatalf("metadata = %v, want exactly malformed_spreadsheet", document.SourceMetadata)
		}
		if sections != nil || links != nil {
			t.Fatalf("sections/links = (%v, %v), want nil", sections, links)
		}
	})

	t.Run("empty body warns without sections", func(t *testing.T) {
		t.Parallel()

		document, sections, _ := extractSpreadsheetDocumentation(testDocRepo(), "docs/owners.csv", "d", "c", []byte(""), "csv")
		if document.SourceMetadata["warning"] != "empty_spreadsheet" {
			t.Fatalf("metadata = %v, want exactly empty_spreadsheet", document.SourceMetadata)
		}
		if len(sections) != 0 {
			t.Fatalf("sections = %v, want none", sections)
		}
	})
}
