// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestStreamFactsEmitsDelimitedSpreadsheetDocumentation(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "service-inventory.csv"), strings.Join([]string{
		"service,owner_email,dependency",
		"payments-api,ops@example.invalid,postgres",
		"billing-worker,billing@example.invalid,queue",
	}, "\n"))
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "audit.tsv"), strings.Join([]string{
		"control\tstatus\tevidence",
		"logging\tpass\thttps://docs.example.test/logging",
	}, "\n"))

	collected := buildStreamingGeneration(
		repoPath,
		testCollectorRepositoryMetadata(repoPath),
		"run-1",
		time.Date(2026, time.June, 9, 7, 0, 0, 0, time.UTC),
		RepositorySnapshot{
			FileCount: 2,
			DocumentationFileMetas: []ContentFileMeta{
				{RelativePath: "docs/service-inventory.csv", Digest: "sha256:inventory", Language: "csv"},
				{RelativePath: "docs/audit.tsv", Digest: "sha256:audit", Language: "tsv"},
			},
		},
		false,
		"",
	)
	envelopes := drainFactChannel(collected.Facts)

	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 2; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	formats := map[string]bool{}
	for _, document := range documents {
		formats[payloadString(document.Payload, "format")] = true
		assertDocumentationFactLinkedRepository(t, document, "repository:r_12345678")
	}
	for _, want := range []string{"csv", "tsv"} {
		if !formats[want] {
			t.Fatalf("missing document format %q in %#v", want, formats)
		}
	}

	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 2; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	inventory := sectionByHeading(sections, "service-inventory")
	if inventory == nil {
		t.Fatalf("missing inventory section in %#v", sections)
	}
	content := payloadString(inventory.Payload, "content")
	for _, want := range []string{"columns: service, owner_email, dependency", "payments-api", "postgres"} {
		if !strings.Contains(content, want) {
			t.Fatalf("inventory content missing %q in %q", want, content)
		}
	}
	for _, forbidden := range []string{"ops@example.invalid", "billing@example.invalid"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("sensitive cell value leaked into spreadsheet content: %q", content)
		}
	}
	assertPayloadWarning(t, inventory.Payload, "sensitive_cell_redacted")
	if got, want := payloadSourceMetadataValue(inventory.Payload, "row_count"), "2"; got != want {
		t.Fatalf("row_count = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(inventory.Payload, "column_count"), "3"; got != want {
		t.Fatalf("column_count = %q, want %q", got, want)
	}

	links := factsByKind(envelopes, facts.DocumentationLinkFactKind)
	assertLinkTargetPresent(t, links, "https://docs.example.test/logging")
}

func TestStreamFactsBoundsLargeDelimitedSpreadsheet(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	lines := []string{"service,owner,dependency"}
	for i := 0; i < spreadsheetMaxRows+5; i++ {
		lines = append(lines, fmt.Sprintf("service-%03d,team-%03d,dependency-%03d", i, i, i))
	}
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "large-inventory.csv"), strings.Join(lines, "\n"))

	envelopes := streamSpreadsheetFacts(t, repoPath, "docs/large-inventory.csv")
	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 1; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	section := sections[0]
	assertPayloadWarning(t, section.Payload, "row_limit_exceeded")
	if got, want := payloadSourceMetadataValue(section.Payload, "row_count"), fmt.Sprintf("%d", spreadsheetMaxRows); got != want {
		t.Fatalf("row_count = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(section.Payload, "sample_row_count"), fmt.Sprintf("%d", spreadsheetSampleRows); got != want {
		t.Fatalf("sample_row_count = %q, want %q", got, want)
	}
	content := payloadString(section.Payload, "content")
	if strings.Contains(content, "service-020") {
		t.Fatalf("spreadsheet content includes rows beyond bounded sample: %q", content)
	}
}

func TestStreamFactsHandlesMalformedDelimitedSpreadsheet(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "broken.csv"), "service,owner\n\"unterminated\n")

	envelopes := streamSpreadsheetFacts(t, repoPath, "docs/broken.csv")
	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "malformed_spreadsheet")
	if got, want := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)), 0; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
}

func TestNativeSnapshotConservativelyClassifiesDelimitedSpreadsheets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "app.py"), "def handler():\n    return 1\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "docs", "service-inventory.csv"), "service,owner\npayments-api,platform\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "data", "raw-export.csv"), "id,value\n1,raw\n")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	got, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if gotLen, want := len(got.DocumentationFileMetas), 1; gotLen != want {
		t.Fatalf("len(DocumentationFileMetas) = %d, want %d: %#v", gotLen, want, got.DocumentationFileMetas)
	}
	if got.DocumentationFileMetas[0].RelativePath != "docs/service-inventory.csv" {
		t.Fatalf("documentation path = %q, want docs/service-inventory.csv", got.DocumentationFileMetas[0].RelativePath)
	}
	if gotParsedFilePathCount(got.FileData, "raw-export.csv") != 0 {
		t.Fatalf("raw CSV export was parsed as source data: %#v", got.FileData)
	}
}

func TestGitDocumentationSourceURIAndFormatClassifiesSpreadsheetPaths(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		path       string
		wantOK     bool
		wantFormat string
	}{
		{path: "docs/service-inventory.csv", wantOK: true, wantFormat: "csv"},
		{path: "runbooks/audit.tsv", wantOK: true, wantFormat: "tsv"},
		{path: "data/raw-export.csv", wantOK: false},
		{path: "tmp/debug.tsv", wantOK: false},
	} {
		sourceURI, format, ok := gitDocumentationSourceURIAndFormat(tc.path)
		if ok != tc.wantOK {
			t.Fatalf("gitDocumentationSourceURIAndFormat(%q) ok = %v, want %v", tc.path, ok, tc.wantOK)
		}
		if !tc.wantOK {
			continue
		}
		if sourceURI != tc.path {
			t.Fatalf("sourceURI = %q, want %q", sourceURI, tc.path)
		}
		if format.format != tc.wantFormat {
			t.Fatalf("format = %q, want %q", format.format, tc.wantFormat)
		}
	}
}

func streamSpreadsheetFacts(t *testing.T, repoPath string, relativePath string) []facts.Envelope {
	t.Helper()

	collected := buildStreamingGeneration(
		repoPath,
		testCollectorRepositoryMetadata(repoPath),
		"run-1",
		time.Date(2026, time.June, 9, 7, 30, 0, 0, time.UTC),
		RepositorySnapshot{
			FileCount: 1,
			DocumentationFileMetas: []ContentFileMeta{{
				RelativePath: relativePath,
				Digest:       "sha256:spreadsheet",
				Language:     filepath.Ext(relativePath)[1:],
				ArtifactType: "documentation",
				CommitSHA:    "abc123",
			}},
		},
		false,
		"",
	)
	return drainFactChannel(collected.Facts)
}
