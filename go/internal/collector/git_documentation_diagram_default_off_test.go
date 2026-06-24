// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestStructuredDiagramDocumentationFormatsAreDocumentationFiles(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	relativePaths := []string{
		"docs/architecture.svg",
		"docs/architecture.drawio",
		"docs/architecture.excalidraw",
		"docs/architecture.puml",
		"docs/architecture.plantuml",
	}
	for _, relativePath := range relativePaths {
		file := filepath.Join(repoPath, filepath.FromSlash(relativePath))
		writeCollectorTestFile(t, file, "diagram placeholder")
		parserFiles, documentationFiles := partitionNativeSnapshotFiles([]string{file}, parser.Registry{})
		if got, want := len(parserFiles), 0; got != want {
			t.Fatalf("partitionNativeSnapshotFiles(%q) parserFiles len = %d, want %d", file, got, want)
		}
		if got, want := len(documentationFiles), 1; got != want {
			t.Fatalf("partitionNativeSnapshotFiles(%q) documentationFiles len = %d, want %d", file, got, want)
		}
		if _, _, ok := gitDocumentationSourceURIAndFormat(relativePath); !ok {
			t.Fatalf("gitDocumentationSourceURIAndFormat(%q) ok = false, want true", relativePath)
		}
		if metas := documentationFileMetasForPaths(repoPath, []string{file}, "commit"); len(metas) != 1 {
			t.Fatalf("documentationFileMetasForPaths(%q) len = %d, want 1: %#v", file, len(metas), metas)
		}
	}
}
