// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// emptyGraph answers every graph read with no rows, so a load sees no related
// repository.
type emptyGraph struct{}

func (emptyGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (emptyGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// TestConfigArtifactReadStatsCountBatchedBytes proves the stage statistics see
// the bytes of a batched read. HydratedBytes is the content length the store
// returned across every read shape, so it must equal the bytes of the rows the
// fake served, and Statements must count one statement per chunk.
func TestConfigArtifactReadStatsCountBatchedBytes(t *testing.T) {
	t.Parallel()

	files := tfFiles(23)
	for i := range files {
		// Distinct sizes, so a sum cannot match by accident.
		files[i].Content += strings.Repeat("# pad\n", i+1)
	}
	var want int64
	for _, file := range files {
		want += int64(len(file.Content))
	}
	store := newHydrationStore(files...)
	_, stats, err := LoadDeploymentArtifactOverviewWithStats(
		context.Background(), emptyGraph{}, store, hydrationTestRepoID, "svc", store.listed(t), nil, nil,
	)
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	if stats.HydratedBytes != want {
		t.Fatalf("HydratedBytes = %d, want %d: the bytes of 23 consumed files read in 3 chunks", stats.HydratedBytes, want)
	}
	if got, wantStatements := len(store.batchCalls), 3; got != wantStatements || stats.BatchStatements != wantStatements {
		t.Fatalf("chunks = %d, stats.BatchStatements = %d, want %d", got, stats.BatchStatements, wantStatements)
	}
	if store.singleCalls != 0 {
		t.Fatalf("%d GetFileContent statements, want none", store.singleCalls)
	}
	wantAll := len(store.batchCalls) + len(store.blankCalls) + store.listCalls + store.singleCalls
	if stats.Statements != wantAll {
		t.Fatalf("stats.Statements = %d, want %d (every list, read, chunk and blank check)", stats.Statements, wantAll)
	}
}

// meetingStore lets the reads of one repository run only once as many are in
// flight as that repository has chunks. The peak concurrency of a repository
// is then its chunk count, whatever the scheduler does.
type meetingStore struct {
	querycontract.ContentStore

	mu      sync.Mutex
	files   map[string][]querycontract.FileContent
	chunks  map[string]int
	waiting map[string]int
	gate    map[string]chan struct{}
}

func newMeetingStore(files map[string][]querycontract.FileContent) *meetingStore {
	s := &meetingStore{
		files:   files,
		chunks:  map[string]int{},
		waiting: map[string]int{},
		gate:    map[string]chan struct{}{},
	}
	for repo, list := range files {
		s.chunks[repo] = (len(list) + 9) / 10
		s.gate[repo] = make(chan struct{})
	}
	return s
}

func (s *meetingStore) ListRepoFiles(_ context.Context, repoID string, _ int) ([]querycontract.FileContent, error) {
	listed := make([]querycontract.FileContent, 0, len(s.files[repoID]))
	for _, file := range s.files[repoID] {
		file.RepoID = repoID
		file.Content = ""
		listed = append(listed, file)
	}
	return listed, nil
}

func (s *meetingStore) GetFileContents(ctx context.Context, repoID string, paths []string) ([]querycontract.FileContent, error) {
	s.mu.Lock()
	s.waiting[repoID]++
	if s.waiting[repoID] == s.chunks[repoID] {
		close(s.gate[repoID])
	}
	gate := s.gate[repoID]
	s.mu.Unlock()
	select {
	case <-gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("repository %q: reads did not meet", repoID)
	}
	byPath := map[string]querycontract.FileContent{}
	for _, file := range s.files[repoID] {
		byPath[file.RelativePath] = file
	}
	rows := make([]querycontract.FileContent, 0, len(paths))
	for _, path := range paths {
		row := byPath[path]
		row.RepoID = repoID
		rows = append(rows, row)
	}
	return rows, nil
}

// TestConfigArtifactReadStatsKeepTheLargestInFlightAcrossRepositories runs the
// real loader over two repositories whose reads can only peak at 3 and then
// at 2. The stage statistic is the maximum over the stage, so it must read 3
// after the second repository, not the 2 that overwrote it.
func TestConfigArtifactReadStatsKeepTheLargestInFlightAcrossRepositories(t *testing.T) {
	t.Parallel()

	store := newMeetingStore(map[string][]querycontract.FileContent{
		"repo-wide":   tfFiles(30),
		"repo-narrow": tfFiles(12),
	})
	sources := []repositoryArtifactSource{
		{RepoID: "repo-wide", RepoName: "wide"},
		{RepoID: "repo-narrow", RepoName: "narrow"},
	}
	var stats ConfigArtifactReadStats
	if _, err := loadRepositoryConfigArtifactsForSources(
		context.Background(), store, sources, &stats, productionConfigArtifactPasses,
	); err != nil {
		t.Fatalf("load error = %v", err)
	}
	if stats.BatchChunksInFlightMax != 3 {
		t.Fatalf("BatchChunksInFlightMax = %d, want 3 (the wide repository's three chunks, kept after the two-chunk one)", stats.BatchChunksInFlightMax)
	}
	if stats.BatchStatements != 5 || stats.BatchChunkSize != 10 {
		t.Fatalf("BatchStatements = %d, BatchChunkSize = %d, want 5 and 10", stats.BatchStatements, stats.BatchChunkSize)
	}
}
