// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// cancelHydrationStore counts GetFileContents statements and runs onRead, when
// set, on every statement so a test can cancel the parent context mid-flight
// or fail a chunk.
type cancelHydrationStore struct {
	querycontract.ContentStore
	calls  atomic.Int64
	onRead func() error
}

func (s *cancelHydrationStore) GetFileContents(_ context.Context, _ string, paths []string) ([]querycontract.FileContent, error) {
	s.calls.Add(1)
	if s.onRead != nil {
		if err := s.onRead(); err != nil {
			return nil, err
		}
	}
	rows := make([]querycontract.FileContent, 0, len(paths))
	for _, path := range paths {
		rows = append(rows, querycontract.FileContent{RelativePath: path, Content: "name: ci\n"})
	}
	return rows, nil
}

// cancelHydrationFiles returns enough workflow candidates for twice the worker
// count of chunk reads, so a cancel always lands while chunks are still unsent.
func cancelHydrationFiles() []querycontract.FileContent {
	count := 2 * repositoryArtifactHydrationConcurrency * anchorHydrationChunkSize
	files := make([]querycontract.FileContent, 0, count)
	for i := range count {
		files = append(files, querycontract.FileContent{
			RelativePath: fmt.Sprintf(".github/workflows/w%04d.yml", i),
		})
	}
	return files
}

// TestHydrateRepositoryCandidateFilesPreCanceledContextReturnsCanceledWithoutReads
// is the #8889 regression. With the parent context canceled before the call,
// both select cases of the send loop are ready (a worker waiting on jobs and a
// closed Done channel) and Go picks one at random, so the pass used to start
// reads and, with no firstErr, return the unhydrated rows with a nil error. A
// canceled request must return context.Canceled and issue no read.
func TestHydrateRepositoryCandidateFilesPreCanceledContextReturnsCanceledWithoutReads(t *testing.T) {
	t.Parallel()

	files := cancelHydrationFiles()
	store := &cancelHydrationStore{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	hydrated, err := HydrateRepositoryCandidateFiles(ctx, store, "repo-a", files, IsGitHubActionsWorkflowFile)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("HydrateRepositoryCandidateFiles() error = %v, want context.Canceled", err)
	}
	if hydrated != nil {
		t.Fatalf("HydrateRepositoryCandidateFiles() rows = %d, want nil on error", len(hydrated))
	}
	if got := store.calls.Load(); got != 0 {
		t.Fatalf("GetFileContents statements = %d, want 0 on a canceled context", got)
	}
}

// TestHydrateRepositoryCandidateFilesMidFlightCancelNeverReturnsPartialRows
// cancels the parent context from inside the first chunk read. Some chunks are
// already hydrated and the rest never start, so a nil error would hand the
// caller a partly hydrated slice as a complete answer. The pass must return the
// parent context error and no rows.
func TestHydrateRepositoryCandidateFilesMidFlightCancelNeverReturnsPartialRows(t *testing.T) {
	t.Parallel()

	files := cancelHydrationFiles()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &cancelHydrationStore{onRead: func() error {
		cancel()
		return nil
	}}

	hydrated, err := HydrateRepositoryCandidateFiles(ctx, store, "repo-a", files, IsGitHubActionsWorkflowFile)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("HydrateRepositoryCandidateFiles() error = %v, want context.Canceled", err)
	}
	if hydrated != nil {
		t.Fatalf("HydrateRepositoryCandidateFiles() rows = %d with error %v, want nil rows", len(hydrated), err)
	}
	total := int64(len(files) / anchorHydrationChunkSize)
	if got := store.calls.Load(); got >= total {
		t.Fatalf("GetFileContents statements = %d of %d chunks, want the cancel to stop later chunks", got, total)
	}
}

// TestHydrateRepositoryCandidateFilesWorkerErrorBeatsContextError fails a
// chunk and cancels the parent context in the same read. The chunk failure is
// the cause, so it must win over the context error the cancel would produce.
func TestHydrateRepositoryCandidateFilesWorkerErrorBeatsContextError(t *testing.T) {
	t.Parallel()

	errChunk := errors.New("test chunk failure")
	files := cancelHydrationFiles()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &cancelHydrationStore{onRead: func() error {
		cancel()
		return errChunk
	}}

	hydrated, err := HydrateRepositoryCandidateFiles(ctx, store, "repo-a", files, IsGitHubActionsWorkflowFile)
	if !errors.Is(err, errChunk) {
		t.Fatalf("HydrateRepositoryCandidateFiles() error = %v, want the chunk failure", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("HydrateRepositoryCandidateFiles() error = %v, want the worker error to win over context.Canceled", err)
	}
	if hydrated != nil {
		t.Fatalf("HydrateRepositoryCandidateFiles() rows = %d, want nil on error", len(hydrated))
	}
}
