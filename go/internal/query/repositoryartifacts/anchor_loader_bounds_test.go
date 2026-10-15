// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestAnchorHydrationReadsWholeCandidateWindow proves the anchor-only loaders'
// shared hydration pass reads every candidate in the list window, not just
// the first RepositoryArtifactHydrationLimit of them (#7951 follow-up). Sixty
// workflow candidates must all come back hydrated through the chunked
// GetFileContents statements, with no per-file GetFileContent statement.
func TestAnchorHydrationReadsWholeCandidateWindow(t *testing.T) {
	t.Parallel()

	const candidates = RepositoryArtifactHydrationLimit + 10
	files := make([]querycontract.FileContent, 0, candidates)
	rows := make([]querycontract.FileContent, 0, candidates)
	for i := 0; i < candidates; i++ {
		path := fmt.Sprintf(".github/workflows/deploy-%02d.yml", i)
		files = append(files, querycontract.FileContent{RelativePath: path})
		rows = append(rows, querycontract.FileContent{RelativePath: path, Content: "name: deploy\n"})
	}
	store := newFakeContentStore(map[string][]querycontract.FileContent{"repo-a": rows})

	hydrated, err := HydrateRepositoryCandidateFiles(context.Background(), store, "repo-a", files, IsGitHubActionsWorkflowFile)
	if err != nil {
		t.Fatalf("HydrateRepositoryCandidateFiles() error = %v, want nil", err)
	}
	if len(hydrated) != candidates {
		t.Fatalf("len(hydrated) = %d, want %d", len(hydrated), candidates)
	}
	for i, file := range hydrated {
		if file.Content == "" {
			t.Fatalf("hydrated[%d] %q has no content: candidate past the old 50-cap was left unread", i, file.RelativePath)
		}
	}
	if store.singleCalls != 0 {
		t.Fatalf("GetFileContent statements = %d, want 0 (anchor hydration reads in chunks)", store.singleCalls)
	}
	if got, want := len(store.batchCalls), 6; got != want {
		t.Fatalf("GetFileContents statements = %d, want %d (60 candidates at 10 paths per chunk)", got, want)
	}
}

// anchorListLimitRecorder captures the limit of every ListRepoFiles call and
// answers with fixed rows.
type anchorListLimitRecorder struct {
	querycontract.ContentStore
	limits []int
	rows   []querycontract.FileContent
}

func (s *anchorListLimitRecorder) ListRepoFiles(_ context.Context, _ string, limit int) ([]querycontract.FileContent, error) {
	s.limits = append(s.limits, limit)
	return s.rows, nil
}

// TestAnchorLoaderListsReadWholeWindowWithSentinel proves the three anchor-only
// loader list reads (workflow, runtime/compose, controller) ask for
// RepositorySemanticEntityLimit+1 rows, so a repository with more than the
// 5,000-file window is detectable instead of silently clipped (#7951).
func TestAnchorLoaderListsReadWholeWindowWithSentinel(t *testing.T) {
	t.Parallel()

	want := querycontract.RepositorySemanticEntityLimit + 1

	workflowStore := &anchorListLimitRecorder{}
	if _, _, err := LoadRepositoryWorkflowArtifacts(context.Background(), workflowStore, "repo-a", nil); err != nil {
		t.Fatalf("LoadRepositoryWorkflowArtifacts() error = %v, want nil", err)
	}

	runtimeStore := &anchorListLimitRecorder{}
	if _, _, err := loadRepositoryRuntimeArtifacts(context.Background(), runtimeStore, "repo-a", nil); err != nil {
		t.Fatalf("loadRepositoryRuntimeArtifacts() error = %v, want nil", err)
	}

	controllerStore := &anchorListLimitRecorder{}
	if _, _, err := LoadRepositoryControllerArtifacts(context.Background(), controllerStore, "repo-a", "svc", nil); err != nil {
		t.Fatalf("LoadRepositoryControllerArtifacts() error = %v, want nil", err)
	}

	for name, limits := range map[string][]int{
		"workflow":   workflowStore.limits,
		"runtime":    runtimeStore.limits,
		"controller": controllerStore.limits,
	} {
		if len(limits) != 1 || limits[0] != want {
			t.Fatalf("%s ListRepoFiles limits = %v, want [%d]", name, limits, want)
		}
	}
}

// BenchmarkAnchorHydrationWholeWindow measures the #7951 follow-up hydration
// pass over 500 workflow candidates: the deterministic statement count is the
// proof the pass stays chunked (one GetFileContents per ten paths).
func BenchmarkAnchorHydrationWholeWindow(b *testing.B) {
	const candidates = 500
	files := make([]querycontract.FileContent, 0, candidates)
	rows := make([]querycontract.FileContent, 0, candidates)
	for i := 0; i < candidates; i++ {
		path := fmt.Sprintf(".github/workflows/deploy-%03d.yml", i)
		files = append(files, querycontract.FileContent{RelativePath: path})
		rows = append(rows, querycontract.FileContent{RelativePath: path, Content: "name: deploy\n"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store := newFakeContentStore(map[string][]querycontract.FileContent{"repo-a": rows})
		hydrated, err := HydrateRepositoryCandidateFiles(context.Background(), store, "repo-a", files, IsGitHubActionsWorkflowFile)
		if err != nil {
			b.Fatalf("HydrateRepositoryCandidateFiles() error = %v", err)
		}
		if len(hydrated) != candidates {
			b.Fatalf("len(hydrated) = %d, want %d", len(hydrated), candidates)
		}
		if i == 0 {
			b.ReportMetric(float64(len(store.batchCalls)), "chunks")
		}
	}
}

// TestAnchorLoaderListClipsSentinelRow proves a loader list that returns the
// sentinel row past the window clips it: the sentinel workflow file past the
// window is never hydrated or built into rows, even though its content would
// otherwise produce a row.
func TestAnchorLoaderListClipsSentinelRow(t *testing.T) {
	t.Parallel()

	limit := querycontract.RepositorySemanticEntityLimit
	rows := make([]querycontract.FileContent, 0, limit+1)
	for i := 0; i < limit; i++ {
		rows = append(rows, querycontract.FileContent{RelativePath: fmt.Sprintf("file-%05d.txt", i)})
	}
	rows = append(rows, querycontract.FileContent{
		RelativePath: ".github/workflows/zz-sentinel.yml",
		Content:      "name: sentinel\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo past-the-window\n",
	})
	store := &anchorListLimitRecorder{rows: rows}

	got, truncated, err := LoadRepositoryWorkflowArtifacts(context.Background(), store, "repo-a", nil)
	if err != nil {
		t.Fatalf("LoadRepositoryWorkflowArtifacts() error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("LoadRepositoryWorkflowArtifacts() = %v, want nil (the only workflow file sits past the window)", got)
	}
	if !truncated {
		t.Fatal("LoadRepositoryWorkflowArtifacts() truncated = false, want true (sentinel row past the window was cut)")
	}
}

// TestListAnchorCandidateFilesReportsTruncation proves the fallback list
// reports whether it clipped (#8157): a list past the window comes back
// clipped with truncated true, and a short list comes back whole with
// truncated false, so a direct caller can disclose the cut the same way
// production callers do via AnchorFileListTruncatedReason.
func TestListAnchorCandidateFilesReportsTruncation(t *testing.T) {
	t.Parallel()

	limit := querycontract.RepositorySemanticEntityLimit
	clipped := make([]querycontract.FileContent, 0, limit+1)
	for i := 0; i < limit+1; i++ {
		clipped = append(clipped, querycontract.FileContent{RelativePath: fmt.Sprintf("file-%05d.txt", i)})
	}
	store := &anchorListLimitRecorder{rows: clipped}
	files, truncated, err := listAnchorCandidateFiles(context.Background(), store, "repo-a", "workflow")
	if err != nil {
		t.Fatalf("listAnchorCandidateFiles() error = %v, want nil", err)
	}
	if len(files) != limit {
		t.Fatalf("len(files) = %d, want clipped window %d", len(files), limit)
	}
	if !truncated {
		t.Fatal("listAnchorCandidateFiles() truncated = false, want true (list was cut)")
	}

	short := &anchorListLimitRecorder{rows: clipped[:10]}
	files, truncated, err = listAnchorCandidateFiles(context.Background(), short, "repo-a", "workflow")
	if err != nil {
		t.Fatalf("listAnchorCandidateFiles() error = %v, want nil", err)
	}
	if len(files) != 10 {
		t.Fatalf("len(files) = %d, want 10 (short list passes through)", len(files))
	}
	if truncated {
		t.Fatal("listAnchorCandidateFiles() truncated = true, want false (nothing was cut)")
	}
}

// TestAnchorLoaderFallbackPropagatesTruncation proves every anchor-only
// loader surfaces a clipped fallback list to its direct nil caller (#8157),
// while a non-nil production list never reports a cut.
func TestAnchorLoaderFallbackPropagatesTruncation(t *testing.T) {
	t.Parallel()

	limit := querycontract.RepositorySemanticEntityLimit
	rows := make([]querycontract.FileContent, 0, limit+1)
	for i := 0; i < limit+1; i++ {
		rows = append(rows, querycontract.FileContent{RelativePath: fmt.Sprintf("file-%05d.txt", i)})
	}

	workflowStore := &anchorListLimitRecorder{rows: rows}
	if _, truncated, err := LoadRepositoryWorkflowArtifacts(context.Background(), workflowStore, "repo-a", nil); err != nil || !truncated {
		t.Fatalf("LoadRepositoryWorkflowArtifacts() = (_, %v, %v), want (_, true, nil)", truncated, err)
	}

	runtimeStore := &anchorListLimitRecorder{rows: rows}
	if _, truncated, err := loadRepositoryRuntimeArtifacts(context.Background(), runtimeStore, "repo-a", nil); err != nil || !truncated {
		t.Fatalf("loadRepositoryRuntimeArtifacts() = (_, %v, %v), want (_, true, nil)", truncated, err)
	}

	controllerStore := &anchorListLimitRecorder{rows: rows}
	if _, truncated, err := LoadRepositoryControllerArtifacts(context.Background(), controllerStore, "repo-a", "svc", nil); err != nil || !truncated {
		t.Fatalf("LoadRepositoryControllerArtifacts() = (_, %v, %v), want (_, true, nil)", truncated, err)
	}

	whole := []querycontract.FileContent{{RelativePath: "file-00000.txt"}}
	if _, truncated, err := LoadRepositoryWorkflowArtifacts(context.Background(), &anchorListLimitRecorder{}, "repo-a", whole); err != nil || truncated {
		t.Fatalf("LoadRepositoryWorkflowArtifacts(non-nil) = (_, %v, %v), want (_, false, nil)", truncated, err)
	}
}

// failingBatchStore fails every GetFileContents chunk.
type failingBatchStore struct {
	querycontract.ContentStore
	calls int
}

func (s *failingBatchStore) GetFileContents(context.Context, string, []string) ([]querycontract.FileContent, error) {
	s.calls++
	return nil, errors.New("test chunk failure")
}

// TestAnchorHydrationFailedChunkFailsPass proves a failed chunk fails the
// pass instead of returning partial hydration (#7951 follow-up).
func TestAnchorHydrationFailedChunkFailsPass(t *testing.T) {
	t.Parallel()

	files := []querycontract.FileContent{{RelativePath: ".github/workflows/deploy.yml"}}
	store := &failingBatchStore{}
	if _, err := HydrateRepositoryCandidateFiles(context.Background(), store, "repo-a", files, IsGitHubActionsWorkflowFile); err == nil {
		t.Fatal("HydrateRepositoryCandidateFiles() error = nil, want chunk failure")
	}
	if store.calls == 0 {
		t.Fatal("GetFileContents calls = 0, want at least one attempted chunk")
	}
}
