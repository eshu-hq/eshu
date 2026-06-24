// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestMediaDocumentationFormatsRemainDefaultOff(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	relativePaths := []string{
		"docs/incident.mp3",
		"docs/walkthrough.wav",
		"docs/demo.m4a",
		"docs/review.ogg",
		"docs/capture.mp4",
		"docs/capture.mov",
		"docs/demo.webm",
		"docs/archive.mkv",
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
