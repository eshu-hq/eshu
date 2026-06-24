// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestDocumentationArchiveRoutingEnablesArchiveFormats(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		path       string
		wantOK     bool
		wantFormat string
	}{
		{path: "docs/bundle.zip", wantOK: true, wantFormat: "zip"},
		{path: "docs/bundle.tar", wantOK: true, wantFormat: "tar"},
		{path: "docs/bundle.tar.gz", wantOK: true, wantFormat: "tar.gz"},
		{path: "docs/bundle.tgz", wantOK: true, wantFormat: "tar.gz"},
	} {
		_, format, ok := gitDocumentationSourceURIAndFormat(tc.path)
		if ok != tc.wantOK {
			t.Fatalf("gitDocumentationSourceURIAndFormat(%q) ok = %v, want %v", tc.path, ok, tc.wantOK)
		}
		if tc.wantOK && format.format != tc.wantFormat {
			t.Fatalf("format = %q, want %q", format.format, tc.wantFormat)
		}
	}

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "app.py"), "def handler():\n    return 1\n")
	writeCollectorTestBytes(t, filepath.Join(repoRoot, "docs", "bundle.zip"), defaultOffZipFixture(t))
	tarBody := defaultOffTarFixture(t)
	writeCollectorTestBytes(t, filepath.Join(repoRoot, "docs", "bundle.tar"), tarBody)
	writeCollectorTestBytes(t, filepath.Join(repoRoot, "docs", "bundle.tar.gz"), gzipCollectorTestBytes(t, tarBody))
	writeCollectorTestBytes(t, filepath.Join(repoRoot, "docs", "bundle.tgz"), gzipCollectorTestBytes(t, tarBody))

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	got, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if len(got.DocumentationFileMetas) != 4 {
		t.Fatalf("len(DocumentationFileMetas) = %d, want 4: %#v", len(got.DocumentationFileMetas), got.DocumentationFileMetas)
	}
	gotPaths := map[string]bool{}
	for _, meta := range got.DocumentationFileMetas {
		gotPaths[meta.RelativePath] = true
	}
	for _, want := range []string{
		"docs/bundle.zip",
		"docs/bundle.tar",
		"docs/bundle.tar.gz",
		"docs/bundle.tgz",
	} {
		if !gotPaths[want] {
			t.Fatalf("missing documentation path %q in %#v", want, got.DocumentationFileMetas)
		}
	}
	if gotParsedFilePathCount(got.FileData, "bundle.zip") != 0 ||
		gotParsedFilePathCount(got.FileData, "bundle.tar") != 0 ||
		gotParsedFilePathCount(got.FileData, "bundle.tar.gz") != 0 ||
		gotParsedFilePathCount(got.FileData, "bundle.tgz") != 0 {
		t.Fatalf("archive files entered parser file data: %#v", got.FileData)
	}
}

func defaultOffZipFixture(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	fileWriter, err := writer.Create("docs/guide.md")
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if _, err := fileWriter.Write([]byte("guide\n")); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

func defaultOffTarFixture(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	body := []byte("guide\n")
	if err := writer.WriteHeader(&tar.Header{
		Name: "docs/guide.md",
		Mode: 0o644,
		Size: int64(len(body)),
	}); err != nil {
		t.Fatalf("WriteHeader() error = %v, want nil", err)
	}
	if _, err := writer.Write(body); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

func writeCollectorTestBytes(t *testing.T, path string, body []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}
}

func gzipCollectorTestBytes(t *testing.T, body []byte) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(body); err != nil {
		t.Fatalf("gzip Write() error = %v, want nil", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip Close() error = %v, want nil", err)
	}
	return buffer.Bytes()
}
