// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestUnsupportedMacroEnabledOOXMLDocumentationFormatsRemainDefaultOff(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	relativePaths := []string{
		"docs/runbook.docm",
		"docs/inventory.xlsm",
		"docs/review.pptm",
	}
	for _, relativePath := range relativePaths {
		file := filepath.Join(repoPath, filepath.FromSlash(relativePath))
		parserFiles, documentationFiles := partitionNativeSnapshotFiles([]string{file}, parser.Registry{})
		if len(documentationFiles) != 0 {
			t.Fatalf("partitionNativeSnapshotFiles(%q) documentationFiles = %#v, want none", file, documentationFiles)
		}
		if got, want := len(parserFiles), 1; got != want {
			t.Fatalf("partitionNativeSnapshotFiles(%q) parserFiles len = %d, want %d", file, got, want)
		}
		if _, _, ok := gitDocumentationSourceURIAndFormat(relativePath); ok {
			t.Fatalf("gitDocumentationSourceURIAndFormat(%q) ok = true, want false", relativePath)
		}
		if metas := documentationFileMetasForPaths(repoPath, []string{file}, "commit"); len(metas) != 0 {
			t.Fatalf("documentationFileMetasForPaths(%q) = %#v, want none", file, metas)
		}
	}
}

func TestOfficeSpreadsheetDocumentationFormatsAreDocumentationFiles(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	for _, tc := range []struct {
		relativePath string
		wantFormat   string
	}{
		{relativePath: "docs/runbook.docx", wantFormat: "docx"},
		{relativePath: "docs/inventory.xlsx", wantFormat: "xlsx"},
		{relativePath: "docs/legacy.xls", wantFormat: "xls"},
		{relativePath: "docs/review.pptx", wantFormat: "pptx"},
	} {
		file := filepath.Join(repoPath, filepath.FromSlash(tc.relativePath))
		writeCollectorTestFile(t, file, "workbook placeholder")
		parserFiles, documentationFiles := partitionNativeSnapshotFiles([]string{file}, parser.Registry{})
		if got, want := len(parserFiles), 0; got != want {
			t.Fatalf("partitionNativeSnapshotFiles(%q) parserFiles len = %d, want %d", file, got, want)
		}
		if got, want := len(documentationFiles), 1; got != want {
			t.Fatalf("partitionNativeSnapshotFiles(%q) documentationFiles len = %d, want %d", file, got, want)
		}
		_, format, ok := gitDocumentationSourceURIAndFormat(tc.relativePath)
		if !ok {
			t.Fatalf("gitDocumentationSourceURIAndFormat(%q) ok = false, want true", tc.relativePath)
		}
		if got := format.format; got != tc.wantFormat {
			t.Fatalf("format = %q, want %q", got, tc.wantFormat)
		}
		if metas := documentationFileMetasForPaths(repoPath, []string{file}, "commit"); len(metas) != 1 {
			t.Fatalf("documentationFileMetasForPaths(%q) len = %d, want 1: %#v", file, len(metas), metas)
		}
	}
}
