// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDocFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestIsNotebookDocumentationPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		path string
		want bool
	}{
		{"analysis.ipynb", true},
		{"dir/NB.IPYNB", true},
		{"notes.md", false},
		{"ipynb", false},
	} {
		if got := IsNotebookDocumentationPath(tc.path); got != tc.want {
			t.Fatalf("IsNotebookDocumentationPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestDocumentationFileMetasWithReadFailures(t *testing.T) {
	t.Parallel()

	t.Run("doc files yield metas and others are skipped", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		doc := filepath.Join(dir, "docs", "guide.md")
		code := filepath.Join(dir, "main.go")
		writeDocFile(t, doc, "# Guide\n")
		writeDocFile(t, code, "package main\n")

		metas, failures := DocumentationFileMetasWithReadFailures(dir, []string{doc, code}, "deadbee")
		if failures != 0 {
			t.Fatalf("read failures = %d, want 0", failures)
		}
		if len(metas) != 1 {
			t.Fatalf("metas = %+v, want one entry", metas)
		}
		meta := metas[0]
		if meta.RelativePath != "docs/guide.md" {
			t.Fatalf("relative path = %q, want docs/guide.md", meta.RelativePath)
		}
		if meta.Digest == "" {
			t.Fatal("digest is empty")
		}
		if meta.Language != "markdown" || meta.ArtifactType != "documentation" || meta.CommitSHA != "deadbee" {
			t.Fatalf("meta = %+v, want markdown documentation at deadbee", meta)
		}
	})

	t.Run("missing digest counts a read failure", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		missing := filepath.Join(dir, "docs", "gone.md")

		metas, failures := DocumentationFileMetasWithReadFailures(dir, []string{missing}, "c")
		if failures != 1 {
			t.Fatalf("read failures = %d, want 1", failures)
		}
		if len(metas) != 0 {
			t.Fatalf("metas = %+v, want none", metas)
		}
	})

	t.Run("wrapper drops the failure count", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		doc := filepath.Join(dir, "notes.txt")
		writeDocFile(t, doc, "hello\n")

		metas := DocumentationFileMetasForPaths(dir, []string{doc}, "")
		if len(metas) != 1 || metas[0].RelativePath != "notes.txt" || metas[0].Language != "text" {
			t.Fatalf("metas = %+v, want one text entry", metas)
		}
	})
}
