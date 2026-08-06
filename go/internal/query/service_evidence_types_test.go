// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
)

// TestBuildSpecFileResolverTreatsFragmentOnlyRefAsNoExternalFile is a PR #5933
// review fix (Copilot, service_evidence_types.go:69). openAPIRefFilePath
// returns "" for a fragment-only $ref such as "#/components/schemas/Widget"
// (no external file, just an in-document JSON pointer). Before this fix,
// buildSpecFileResolver's closure did not check for that empty result and
// called reader.GetFileContent(ctx, repoID, "") anyway -- a needless store
// query that, on a backend rejecting an empty path, surfaces as a confusing
// wrapped error (`get referenced spec file ""`). A fragment-only ref must
// resolve to "no external file" (empty string, nil error) without ever
// reaching the reader.
func TestBuildSpecFileResolverTreatsFragmentOnlyRefAsNoExternalFile(t *testing.T) {
	t.Parallel()

	reader := &countingSpecFileReader{err: errors.New("get referenced spec file \"\": store rejected empty path")}
	resolver := buildSpecFileResolver(context.Background(), reader, "repo-api")

	content, err := resolver("specs/index.yaml", "#/components/schemas/Widget")
	if err != nil {
		t.Fatalf("resolver() error = %v, want nil for a fragment-only ref", err)
	}
	if content != "" {
		t.Fatalf("resolver() content = %q, want empty string for a fragment-only ref", content)
	}
	if reader.getFileContentCalls != 0 {
		t.Fatalf(
			"GetFileContent called %d time(s) for a fragment-only ref, want 0 (openAPIRefFilePath resolved to \"\" and must short-circuit before any store read)",
			reader.getFileContentCalls,
		)
	}
}

// TestBuildSpecFileResolverStillReadsAFileScopedRef proves the fix above does
// not regress the ordinary case: a ref that resolves to a real relative path
// still reaches the reader.
func TestBuildSpecFileResolverStillReadsAFileScopedRef(t *testing.T) {
	t.Parallel()

	reader := &countingSpecFileReader{content: "resolved-content"}
	resolver := buildSpecFileResolver(context.Background(), reader, "repo-api")

	content, err := resolver("specs/index.yaml", "../api/paths/index.yaml")
	if err != nil {
		t.Fatalf("resolver() error = %v, want nil", err)
	}
	if content != "resolved-content" {
		t.Fatalf("resolver() content = %q, want %q", content, "resolved-content")
	}
	if reader.getFileContentCalls != 1 {
		t.Fatalf("GetFileContent called %d time(s), want 1 for a file-scoped ref", reader.getFileContentCalls)
	}
}

// countingSpecFileReader is a minimal serviceEvidenceReader that records how
// many times GetFileContent is called, so a test can prove a call was (or was
// not) made without depending on its error text.
type countingSpecFileReader struct {
	getFileContentCalls int
	content             string
	err                 error
}

func (r *countingSpecFileReader) ListRepoFiles(context.Context, string, int) ([]FileContent, error) {
	return nil, nil
}

func (r *countingSpecFileReader) GetFileContent(_ context.Context, repoID, relativePath string) (*FileContent, error) {
	r.getFileContentCalls++
	if r.err != nil {
		return nil, r.err
	}
	return &FileContent{RepoID: repoID, RelativePath: relativePath, Content: r.content}, nil
}
