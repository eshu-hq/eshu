// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"strings"
	"testing"
)

func TestBoundedDocumentationBody(t *testing.T) {
	t.Parallel()

	t.Run("short body passes through without warnings", func(t *testing.T) {
		t.Parallel()

		text, warnings := boundedDocumentationBody([]byte("hello"))
		if text != "hello" {
			t.Fatalf("boundedDocumentationBody() = %q, want %q", text, "hello")
		}
		if len(warnings) != 0 {
			t.Fatalf("warnings = %v, want none", warnings)
		}
	})

	t.Run("crlf normalizes to lf", func(t *testing.T) {
		t.Parallel()

		text, _ := boundedDocumentationBody([]byte("a\r\nb\rc\r"))
		if text != "a\nb\nc\n" {
			t.Fatalf("boundedDocumentationBody() = %q, want %q", text, "a\nb\nc\n")
		}
	})

	t.Run("invalid utf8 is stripped", func(t *testing.T) {
		t.Parallel()

		text, _ := boundedDocumentationBody([]byte{'o', 'k', 0xff, '!'})
		if text != "ok!" {
			t.Fatalf("boundedDocumentationBody() = %q, want %q", text, "ok!")
		}
	})

	t.Run("oversize body truncates with warning", func(t *testing.T) {
		t.Parallel()

		if DocumentationMaxBodyBytes != 512*1024 {
			t.Fatalf("DocumentationMaxBodyBytes = %d, want 524288: the collector budget moved", DocumentationMaxBodyBytes)
		}
		big := strings.Repeat("x", 512*1024+10)
		text, warnings := boundedDocumentationBody([]byte(big))
		if len(text) != 512*1024 {
			t.Fatalf("truncated length = %d, want 524288", len(text))
		}
		if len(warnings) != 1 || warnings[0] != "body_truncated" {
			t.Fatalf("warnings = %v, want [body_truncated]", warnings)
		}
	})

	t.Run("notebook limit exceeds doc limit", func(t *testing.T) {
		t.Parallel()

		mid := strings.Repeat("y", DocumentationMaxBodyBytes+10)
		if _, warnings := boundedDocumentationBody([]byte(mid)); len(warnings) == 0 {
			t.Fatal("doc body over its limit reported no warning")
		}
		text, warnings := boundedNotebookBody([]byte(mid))
		if len(warnings) != 0 {
			t.Fatalf("notebook warnings = %v, want none", warnings)
		}
		if len(text) != len(mid) {
			t.Fatalf("notebook length = %d, want %d", len(text), len(mid))
		}
	})
}

func TestBoundedDocumentationSectionContent(t *testing.T) {
	t.Parallel()

	t.Run("short content passes through with nil warnings", func(t *testing.T) {
		t.Parallel()

		got, warnings := boundedDocumentationSectionContent("hello")
		if got != "hello" {
			t.Fatalf("content = %q, want %q", got, "hello")
		}
		if warnings != nil {
			t.Fatalf("warnings = %v, want nil", warnings)
		}
	})

	t.Run("oversize content truncates on rune boundary", func(t *testing.T) {
		t.Parallel()

		big := strings.Repeat("é", DocumentationMaxSectionChars+5)
		got, warnings := boundedDocumentationSectionContent(big)
		if len([]rune(got)) != DocumentationMaxSectionChars {
			t.Fatalf("truncated runes = %d, want %d", len([]rune(got)), DocumentationMaxSectionChars)
		}
		if len(warnings) != 1 || warnings[0] != "section_truncated" {
			t.Fatalf("warnings = %v, want [section_truncated]", warnings)
		}
	})
}

func TestAddDocumentationWarnings(t *testing.T) {
	t.Parallel()

	t.Run("no warnings leaves metadata untouched", func(t *testing.T) {
		t.Parallel()

		meta := map[string]string{}
		addDocumentationWarnings(meta)
		if len(meta) != 0 {
			t.Fatalf("metadata = %v, want empty", meta)
		}
	})

	t.Run("first warning seeds the key", func(t *testing.T) {
		t.Parallel()

		meta := map[string]string{}
		addDocumentationWarnings(meta, "body_truncated")
		if meta["warning"] != "body_truncated" {
			t.Fatalf("warning = %q, want body_truncated", meta["warning"])
		}
	})

	t.Run("duplicate and blank warnings collapse", func(t *testing.T) {
		t.Parallel()

		meta := map[string]string{"warning": "body_truncated"}
		addDocumentationWarnings(meta, "body_truncated", " ", "malformed_html", "malformed_html ")
		if meta["warning"] != "body_truncated,malformed_html" {
			t.Fatalf("warning = %q, want combined deduped value", meta["warning"])
		}
	})

	t.Run("all duplicates leave existing value alone", func(t *testing.T) {
		t.Parallel()

		meta := map[string]string{"warning": "body_truncated"}
		addDocumentationWarnings(meta, "body_truncated")
		if meta["warning"] != "body_truncated" {
			t.Fatalf("warning = %q, want unchanged body_truncated", meta["warning"])
		}
	})
}
