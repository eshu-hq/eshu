// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
)

func TestDocumentationIDsAndHash(t *testing.T) {
	t.Parallel()

	if got := gitDocumentationSourceID("repository:x"); got != "doc-source:git:repository:x" {
		t.Fatalf("source ID = %q", got)
	}
	if got := gitDocumentationDocumentID("repository:x", "a/b.md"); got != "doc:git:repository:x:a/b.md" {
		t.Fatalf("document ID = %q", got)
	}
	first := documentationHashText("hello")
	second := documentationHashText("hello")
	if first != second || !strings.HasPrefix(first, "sha256:") || len(first) != 7+64 {
		t.Fatalf("hash = %q, want stable sha256 value", first)
	}
	if documentationHashText("hello") == documentationHashText("world") {
		t.Fatal("distinct inputs share a hash")
	}
}

func TestDocumentationMetaRelativePaths(t *testing.T) {
	t.Parallel()

	metas := []model.ContentFileMeta{
		{RelativePath: "a.md"},
		{RelativePath: "b.md"},
		{RelativePath: "a.md"},
		{RelativePath: ""},
	}
	got := DocumentationMetaRelativePaths(metas)
	if len(got) != 2 || !got["a.md"] || !got["b.md"] {
		t.Fatalf("paths = %v, want deduped a.md+b.md", got)
	}
	if len(DocumentationMetaRelativePaths(nil)) != 0 {
		t.Fatal("nil metas yield non-empty set")
	}
}

func TestReadDocumentationBodyWithError(t *testing.T) {
	t.Parallel()

	t.Run("provided body wins without touching disk", func(t *testing.T) {
		t.Parallel()

		raw, ok, err := ReadDocumentationBodyWithError(t.TempDir(), "notes.md", []byte("in-memory"))
		if err != nil || !ok || string(raw) != "in-memory" {
			t.Fatalf("ReadDocumentationBodyWithError() = (%q, %v, %v)", raw, ok, err)
		}
		if raw, ok := ReadDocumentationBody(t.TempDir(), "notes.md", []byte("in-memory")); !ok || string(raw) != "in-memory" {
			t.Fatalf("ReadDocumentationBody() = (%q, %v)", raw, ok)
		}
	})

	t.Run("unsupported path is deterministic", func(t *testing.T) {
		t.Parallel()

		raw, ok, err := ReadDocumentationBodyWithError(t.TempDir(), "main.go", nil)
		if err != nil || ok || raw != nil {
			t.Fatalf("ReadDocumentationBodyWithError() = (%v, %v, %v), want (nil, false, nil)", raw, ok, err)
		}
	})

	t.Run("legacy xls reports supported without a body", func(t *testing.T) {
		t.Parallel()

		raw, ok, err := ReadDocumentationBodyWithError(t.TempDir(), "docs/data.xls", nil)
		if err != nil || !ok || raw != nil {
			t.Fatalf("ReadDocumentationBodyWithError() = (%v, %v, %v), want (nil, true, nil)", raw, ok, err)
		}
	})

	t.Run("missing file is a read failure", func(t *testing.T) {
		t.Parallel()

		_, ok, err := ReadDocumentationBodyWithError(t.TempDir(), "notes.md", nil)
		if err == nil || ok {
			t.Fatalf("ReadDocumentationBodyWithError() = ok=%v err=%v, want failure", ok, err)
		}
	})

	t.Run("existing file reads through the limit", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		want := "# Title\n\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, "docs", "guide.md"), []byte(want), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		raw, ok, err := ReadDocumentationBodyWithError(dir, "docs/guide.md", nil)
		if err != nil || !ok || string(raw) != want {
			t.Fatalf("ReadDocumentationBodyWithError() = (%q, %v, %v)", raw, ok, err)
		}
	})
}

func TestDocumentationReadLimitBytes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		format string
		want   int
	}{
		{"openapi", 2 * 1024 * 1024},
		{"swagger", 2 * 1024 * 1024},
		{"asyncapi", 2 * 1024 * 1024},
		{"graphql_sdl", 2 * 1024 * 1024},
		{"notebook", 8 * 1024 * 1024},
		{"docx", 50 << 20},
		{"xlsx", 50 << 20},
		{"pptx", 50 << 20},
		{"zip", 100 << 20},
		{"tar.gz", 100 << 20},
		{"markdown", 512 * 1024},
		{"text", 512 * 1024},
	} {
		if got := documentationReadLimitBytes(GitDocumentationFormat{Format: tc.format}); got != tc.want {
			t.Fatalf("documentationReadLimitBytes(%q) = %d, want %d", tc.format, got, tc.want)
		}
	}
}

func TestDocumentationPayloadMap(t *testing.T) {
	t.Parallel()

	got, err := documentationPayloadMap(map[string]string{"a": "b"})
	if err != nil || got["a"] != "b" {
		t.Fatalf("documentationPayloadMap() = (%v, %v)", got, err)
	}
	if _, err := documentationPayloadMap(make(chan int)); err == nil {
		t.Fatal("documentationPayloadMap(chan) = nil error, want marshal failure")
	}
}
