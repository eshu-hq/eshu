// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"fmt"
	"sort"
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

// fakeContentStore is an in-memory ContentStore for the loader tests. It
// serves one repository's rows from every read shape the loader uses and
// counts each statement, so a test sees which reads the neutral package's
// per-repository function issued through the adapter.
type fakeContentStore struct {
	querycontract.ContentStore

	mu          sync.Mutex
	rows        map[string][]querycontract.FileContent
	listCalls   int
	batchCalls  [][]string
	blankCalls  [][]string
	singleCalls int
}

func newFakeContentStore(rows map[string][]querycontract.FileContent) *fakeContentStore {
	return &fakeContentStore{rows: rows}
}

func (s *fakeContentStore) ListRepoFiles(_ context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	listed := make([]querycontract.FileContent, 0, len(s.rows[repoID]))
	for _, file := range s.rows[repoID] {
		file.RepoID = repoID
		file.Content = ""
		listed = append(listed, file)
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].RelativePath < listed[j].RelativePath })
	if limit > 0 && len(listed) > limit {
		listed = listed[:limit]
	}
	return listed, nil
}

func (s *fakeContentStore) GetFileContent(_ context.Context, repoID, relativePath string) (*querycontract.FileContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.singleCalls++
	for _, file := range s.rows[repoID] {
		if file.RelativePath == relativePath {
			file.RepoID = repoID
			return &file, nil
		}
	}
	return nil, nil
}

func (s *fakeContentStore) GetFileContents(_ context.Context, repoID string, paths []string) ([]querycontract.FileContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchCalls = append(s.batchCalls, append([]string(nil), paths...))
	var out []querycontract.FileContent
	for _, path := range paths {
		for _, file := range s.rows[repoID] {
			if file.RelativePath == path {
				file.RepoID = repoID
				out = append(out, file)
			}
		}
	}
	return out, nil
}

func (s *fakeContentStore) NonBlankRepoFilePaths(_ context.Context, repoID string, paths []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blankCalls = append(s.blankCalls, append([]string(nil), paths...))
	var out []string
	for _, path := range paths {
		for _, file := range s.rows[repoID] {
			if file.RelativePath == path && strings.TrimSpace(file.Content) != "" {
				out = append(out, path)
			}
		}
	}
	return out, nil
}

const statsTestRepoID = "repo-anchor"

func contractFile(relativePath, content string) querycontract.FileContent {
	return querycontract.FileContent{RelativePath: relativePath, Content: content}
}

// terraformFiles returns n Terraform files that each hold one module source.
func terraformFiles(n int) []querycontract.FileContent {
	files := make([]querycontract.FileContent, 0, n)
	for i := 0; i < n; i++ {
		files = append(files, contractFile(
			fmt.Sprintf("infra/m%03d/main.tf", i),
			fmt.Sprintf("module \"m%d\" {\n  source = \"./mod%d\"\n}\n", i, i),
		))
	}
	return files
}

// TestConfigArtifactReadStatsCountBatchedBytes proves the stage statistics see
// the bytes of a batched read. HydratedBytes is the content length the store
// returned across every read shape, so it must equal the bytes of the rows the
// fake served, and Statements must count one statement per chunk.
func TestConfigArtifactReadStatsCountBatchedBytes(t *testing.T) {
	t.Parallel()

	files := terraformFiles(23)
	for i := range files {
		// Distinct sizes, so a sum cannot match by accident.
		files[i].Content += strings.Repeat("# pad\n", i+1)
	}
	var want int64
	for _, file := range files {
		want += int64(len(file.Content))
	}
	store := newFakeContentStore(map[string][]querycontract.FileContent{statsTestRepoID: files})
	listed, err := store.ListRepoFiles(context.Background(), statsTestRepoID, 5000)
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	store.listCalls = 0
	_, stats, err := LoadDeploymentArtifactOverviewWithStats(
		context.Background(), emptyGraph{}, store, statsTestRepoID, "svc", listed, nil, nil,
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
		"repo-wide":   terraformFiles(30),
		"repo-narrow": terraformFiles(12),
	})
	sources := []repositoryArtifactSource{
		{RepoID: "repo-wide", RepoName: "wide"},
		{RepoID: "repo-narrow", RepoName: "narrow"},
	}
	var stats ConfigArtifactReadStats
	if _, err := loadRepositoryConfigArtifactsForSources(context.Background(), store, sources, &stats); err != nil {
		t.Fatalf("load error = %v", err)
	}
	if stats.BatchChunksInFlightMax != 3 {
		t.Fatalf("BatchChunksInFlightMax = %d, want 3 (the wide repository's three chunks, kept after the two-chunk one)", stats.BatchChunksInFlightMax)
	}
	if stats.BatchStatements != 5 || stats.BatchChunkSize != 10 {
		t.Fatalf("BatchStatements = %d, BatchChunkSize = %d, want 5 and 10", stats.BatchStatements, stats.BatchChunkSize)
	}
}
