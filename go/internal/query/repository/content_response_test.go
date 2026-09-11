// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// These pins live in package repository (not the root
// repository_content_test.go) because the route-coverage gate requires a
// matching Test function in the handler's own directory. They cover the
// pure response builders only: tests that drive the content success
// envelope stay in root package query, where the full production
// capability matrix (code_search.content_search) is linked -- see
// main_test.go.

func TestRepositoryContentResponseTruncatesLargeFile(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("a", repositoryContentMaxBytes+100)
	resp := repositoryContentResponse(&querycontract.FileContent{
		RelativePath: "main.go",
		CommitSHA:    "abc123",
		Content:      big,
		Language:     "go",
	})

	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := resp["size"], len(big); got != want {
		t.Fatalf("size = %#v, want %#v (original size, not truncated)", got, want)
	}
	content, ok := resp["content"].(string)
	if !ok || len(content) > repositoryContentMaxBytes {
		t.Fatalf("content length = %d, want <= %d", len(content), repositoryContentMaxBytes)
	}
	if got, want := resp["encoding"], "utf-8"; got != want {
		t.Fatalf("encoding = %#v, want %#v", got, want)
	}
	if got, want := resp["language"], "go"; got != want {
		t.Fatalf("language = %#v, want %#v", got, want)
	}
}

func TestRepositoryContentResponseBase64OnBinary(t *testing.T) {
	t.Parallel()

	resp := repositoryContentResponse(&querycontract.FileContent{
		RelativePath: "logo.png",
		CommitSHA:    "abc123",
		Content:      "\xff\xfe\x00binary",
	})

	if got, want := resp["encoding"], "base64"; got != want {
		t.Fatalf("encoding = %#v, want %#v", got, want)
	}
	if got, want := resp["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}
