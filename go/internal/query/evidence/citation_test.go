// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"
)

func TestNormalizeEvidenceCitationRequestPreservesDistinctFileCitations(t *testing.T) {
	t.Parallel()

	handles, limit, truncated, err := normalizeEvidenceCitationRequest(evidenceCitationRequest{
		Limit: 10,
		Handles: []evidence.EvidenceCitationHandle{
			{
				Kind:         "file",
				RepoID:       "repo-service",
				RelativePath: "README.md",
				StartLine:    1,
				EndLine:      4,
				Reason:       "overview",
			},
			{
				Kind:         "file",
				RepoID:       "repo-service",
				RelativePath: "README.md",
				StartLine:    20,
				EndLine:      24,
				Reason:       "operations notes",
			},
		},
	})
	if err != nil {
		t.Fatalf("normalizeEvidenceCitationRequest() error = %v, want nil", err)
	}
	if got, want := limit, 10; got != want {
		t.Fatalf("limit = %d, want %d", got, want)
	}
	if truncated {
		t.Fatal("truncated = true, want false")
	}
	if got, want := len(handles), 2; got != want {
		t.Fatalf("len(handles) = %d, want %d", got, want)
	}
	if got, want := handles[0].StartLine, 1; got != want {
		t.Fatalf("first start line = %d, want %d", got, want)
	}
	if got, want := handles[1].StartLine, 20; got != want {
		t.Fatalf("second start line = %d, want %d", got, want)
	}
}

func TestNormalizeEvidenceCitationRequestStopsAfterTruncationProbe(t *testing.T) {
	t.Parallel()

	req := evidenceCitationRequest{Limit: 2}
	for _, path := range []string{"one.md", "two.md", "three.md"} {
		req.Handles = append(req.Handles, evidence.EvidenceCitationHandle{
			Kind:         "file",
			RepoID:       "repo-service",
			RelativePath: path,
		})
	}
	req.Handles = append(req.Handles, evidence.EvidenceCitationHandle{Kind: "bogus"})

	handles, _, truncated, err := normalizeEvidenceCitationRequest(req)
	if err != nil {
		t.Fatalf("normalizeEvidenceCitationRequest() error = %v, want nil", err)
	}
	if got, want := len(handles), 2; got != want {
		t.Fatalf("len(handles) = %d, want %d", got, want)
	}
	if !truncated {
		t.Fatal("truncated = false, want true")
	}
}

func TestNormalizeEvidenceCitationRequestRejectsOversizedHandleArrays(t *testing.T) {
	t.Parallel()

	req := evidenceCitationRequest{Limit: 50}
	for i := 0; i < evidenceCitationMaxInputHandles+1; i++ {
		req.Handles = append(req.Handles, evidence.EvidenceCitationHandle{
			Kind:         "file",
			RepoID:       "repo-service",
			RelativePath: "README.md",
		})
	}
	if _, _, _, err := normalizeEvidenceCitationRequest(req); err == nil {
		t.Fatal("normalizeEvidenceCitationRequest() error = nil, want non-nil")
	}
}

func TestBoundedLineExcerptReturnsZeroRangeForStartPastEOF(t *testing.T) {
	t.Parallel()

	excerpt, startLine, endLine := boundedLineExcerpt("one\ntwo\n", 10, 12)
	if excerpt != "" {
		t.Fatalf("excerpt = %#v, want empty", excerpt)
	}
	if startLine != 0 || endLine != 0 {
		t.Fatalf("range = %d-%d, want 0-0", startLine, endLine)
	}
}
