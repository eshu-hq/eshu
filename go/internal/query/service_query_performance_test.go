// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

func TestEnrichServiceQueryContextQueriesProvisioningCandidatesOnce(t *testing.T) {
	t.Parallel()

	graph := &countingProvisioningGraph{}
	workloadContext := map[string]any{
		"id":        "workload:service-edge-api",
		"name":      "service-edge-api",
		"kind":      "Deployment",
		"repo_id":   "repo-service-edge-api",
		"repo_name": "service-edge-api",
		"instances": []map[string]any{},
	}

	err := enrichServiceQueryContextWithOptions(
		context.Background(),
		graph,
		content.FakePortContentStore{},
		workloadContext,
		serviceQueryEnrichmentOptions{IncludeRelatedModuleUsage: true},
	)
	if err != nil {
		t.Fatalf("enrichServiceQueryContextWithOptions() error = %v, want nil", err)
	}
	if graph.provisioningCandidateCalls != 1 {
		t.Fatalf("provisioning candidate graph calls = %d, want 1", graph.provisioningCandidateCalls)
	}
}

type countingProvisioningGraph struct {
	provisioningCandidateCalls int
}

func (g *countingProvisioningGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE") {
		g.provisioningCandidateCalls++
		if got, want := params["repo_id"], "repo-service-edge-api"; got != want {
			return nil, nil
		}
		return []map[string]any{
			{
				"repo_id":             "repo-terraform-stack",
				"repo_name":           "terraform-stack-staging",
				"relationship_type":   "PROVISIONS_DEPENDENCY_FOR",
				"relationship_reason": "terraform_provider_reference",
			},
		}, nil
	}
	return nil, nil
}

func (g *countingProvisioningGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestQueryRepoAPISurfaceBoundsEndpointRowsAndKeepsAggregateCount(t *testing.T) {
	t.Parallel()

	graph := &recordingAPISurfaceGraph{t: t}
	got, _ := repository.QueryRepoAPISurface(context.Background(), graph, map[string]any{"repo_id": "repo-service"})
	if got == nil {
		t.Fatal("repository.QueryRepoAPISurface() = nil, want API surface")
	}
	if count := querycontract.IntVal(got, "endpoint_count"); count != 73 {
		t.Fatalf("endpoint_count = %d, want aggregate count 73", count)
	}
	if endpoints := querycontract.MapSliceValue(got, "endpoints"); len(endpoints) != 1 {
		t.Fatalf("len(endpoints) = %d, want bounded detail rows", len(endpoints))
	}
	if graph.countCalls != 1 {
		t.Fatalf("countCalls = %d, want 1", graph.countCalls)
	}
	if graph.detailCalls != 1 {
		t.Fatalf("detailCalls = %d, want 1", graph.detailCalls)
	}
	if limit := querycontract.IntVal(graph.detailParams, "limit"); limit != repository.APISurfaceEndpointLimit {
		t.Fatalf("detail limit = %d, want %d", limit, repository.APISurfaceEndpointLimit)
	}
}

type recordingAPISurfaceGraph struct {
	t            *testing.T
	countCalls   int
	detailCalls  int
	detailParams map[string]any
}

func (g *recordingAPISurfaceGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	switch {
	case strings.Contains(cypher, "count(endpoint) AS endpoint_count"):
		g.countCalls++
		return []map[string]any{{"endpoint_count": 73}}, nil
	case strings.Contains(cypher, "LIMIT $limit"):
		g.detailCalls++
		g.detailParams = params
		return []map[string]any{{
			"endpoint_id":   "endpoint-1",
			"path":          "/widgets",
			"methods":       []any{"GET"},
			"operation_ids": []any{"listWidgets"},
		}}, nil
	default:
		g.t.Fatalf("unexpected Cypher:\n%s", cypher)
		return nil, nil
	}
}

func (g *recordingAPISurfaceGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestContentReaderListFrameworkRoutesIsBoundedAtSQL(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"relative_path", "framework_semantics"},
			rows:    [][]driver.Value{},
		},
	})
	reader := NewContentReader(db)
	_, err := reader.ListFrameworkRoutes(context.Background(), "repo-service")
	if err != nil {
		t.Fatalf("ListFrameworkRoutes() error = %v, want nil", err)
	}
	if len(recorder.queries) != 1 {
		t.Fatalf("len(queries) = %d, want 1", len(recorder.queries))
	}
	if !strings.Contains(recorder.queries[0], "LIMIT $2") {
		t.Fatalf("query = %q, want SQL row limit", recorder.queries[0])
	}
	if got, want := numericDriverValue(t, recorder.args[0][1]), int64(frameworkRouteEvidenceLimit); got != want {
		t.Fatalf("framework route limit = %d, want %d", got, want)
	}
}

func TestQueryRepoDeploymentEvidenceBoundsGraphDirections(t *testing.T) {
	t.Parallel()

	reader := &recordingDeploymentEvidenceGraphReader{}
	if _, err := repository.QueryRepoDeploymentEvidence(context.Background(), reader, nil, map[string]any{"repo_id": "repo-service"}); err != nil {
		t.Fatalf("repository.QueryRepoDeploymentEvidence() error = %v, want nil", err)
	}
	if len(reader.cypherCalls) != 2 {
		t.Fatalf("len(cypherCalls) = %d, want 2", len(reader.cypherCalls))
	}
	if len(reader.params) != 2 {
		t.Fatalf("len(params) = %d, want 2", len(reader.params))
	}
	for i, cypher := range reader.cypherCalls {
		if !strings.Contains(cypher, "LIMIT $limit") {
			t.Fatalf("cypher call %d = %q, want LIMIT $limit", i, cypher)
		}
		if got, want := querycontract.IntVal(reader.params[i], "limit"), repository.DeploymentEvidenceArtifactLimit+1; got != want {
			t.Fatalf("params[%d].limit = %d, want %d", i, got, want)
		}
	}
}

func TestContentReaderRepositoryDeploymentEvidenceIsBoundedAtSQL(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{
				"direction", "resolved_id", "generation_id", "source_repo_id", "source_name",
				"source_remote_url", "source_scope_id", "target_repo_id", "target_name",
				"target_remote_url", "target_scope_id", "relationship_type", "confidence", "details",
			},
			rows: [][]driver.Value{},
		},
	})
	reader := NewContentReader(db)
	_, err := reader.RepositoryDeploymentEvidence(context.Background(), "repo-service")
	if err != nil {
		t.Fatalf("RepositoryDeploymentEvidence() error = %v, want nil", err)
	}
	if len(recorder.queries) != 1 {
		t.Fatalf("len(queries) = %d, want 1", len(recorder.queries))
	}
	if !strings.Contains(recorder.queries[0], "LIMIT $2") {
		t.Fatalf("query = %q, want SQL row limit", recorder.queries[0])
	}
	if got, want := numericDriverValue(t, recorder.args[0][1]), int64(repository.DeploymentEvidenceArtifactLimit+1); got != want {
		t.Fatalf("deployment evidence limit = %d, want %d", got, want)
	}
}

func TestHydrateRepositoryCandidateFilesStartsChunkReadConcurrently(t *testing.T) {
	t.Parallel()

	store := &blockingArtifactHydrationStore{
		started: make(chan string, 2),
		release: make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type hydrateOutcome struct {
		files []querycontract.FileContent
		err   error
	}
	done := make(chan hydrateOutcome, 1)
	go func() {
		hydrated, err := artifacts.HydrateRepositoryCandidateFiles(ctx, store, "repo-service", []querycontract.FileContent{
			{RepoID: "repo-service", RelativePath: "compose-a.yaml", ArtifactType: "docker_compose"},
			{RepoID: "repo-service", RelativePath: "compose-b.yaml", ArtifactType: "docker_compose"},
		}, artifacts.IsDockerComposeArtifact)
		done <- hydrateOutcome{files: hydrated, err: err}
	}()

	seen := map[string]struct{}{}
	for len(seen) < 2 {
		select {
		case path := <-store.started:
			seen[path] = struct{}{}
		case <-time.After(300 * time.Millisecond):
			t.Fatalf("started hydration chunk paths = %#v, want both paths before release", seen)
		}
	}
	close(store.release)
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("artifacts.HydrateRepositoryCandidateFiles() error = %v, want nil", outcome.err)
	}
	for _, file := range outcome.files {
		if file.Content == "" {
			t.Fatalf("hydrated %q has no content, want the chunked read to hydrate it", file.RelativePath)
		}
	}
}

// TestHydrateRepositoryCandidateFilesReadsWholeCandidateWindow proves the
// anchor hydration pass no longer stops at RepositoryArtifactHydrationLimit:
// every candidate in the window is read, through chunked GetFileContents
// statements (#7951 follow-up).
func TestHydrateRepositoryCandidateFilesReadsWholeCandidateWindow(t *testing.T) {
	t.Parallel()

	store := &countingArtifactHydrationStore{}
	files := make([]querycontract.FileContent, 0, artifacts.RepositoryArtifactHydrationLimit+5)
	for i := 0; i < artifacts.RepositoryArtifactHydrationLimit+5; i++ {
		files = append(files, querycontract.FileContent{
			RepoID:       "repo-service",
			RelativePath: fmt.Sprintf(".github/workflows/deploy-%02d.yaml", i),
			ArtifactType: "github_actions_workflow",
		})
	}

	hydrated, err := artifacts.HydrateRepositoryCandidateFiles(context.Background(), store, "repo-service", files, artifacts.IsGitHubActionsWorkflowFile)
	if err != nil {
		t.Fatalf("artifacts.HydrateRepositoryCandidateFiles() error = %v, want nil", err)
	}
	for i, file := range hydrated {
		if file.Content == "" {
			t.Fatalf("hydrated[%d] %q has no content, want the whole window hydrated", i, file.RelativePath)
		}
	}
	if got, want := int(store.paths.Load()), artifacts.RepositoryArtifactHydrationLimit+5; got != want {
		t.Fatalf("GetFileContents paths = %d, want %d", got, want)
	}
	if got := int(store.chunks.Load()); got == 0 {
		t.Fatal("GetFileContents chunks = 0, want chunked reads")
	}
}

type blockingArtifactHydrationStore struct {
	querycontract.ContentStore
	started chan string
	release chan struct{}
}

func (s *blockingArtifactHydrationStore) GetFileContents(ctx context.Context, repoID string, relativePaths []string) ([]querycontract.FileContent, error) {
	for _, relativePath := range relativePaths {
		select {
		case s.started <- relativePath:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	select {
	case <-s.release:
		rows := make([]querycontract.FileContent, 0, len(relativePaths))
		for _, relativePath := range relativePaths {
			rows = append(rows, querycontract.FileContent{RepoID: repoID, RelativePath: relativePath, Content: "services: {}"})
		}
		return rows, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type countingArtifactHydrationStore struct {
	querycontract.ContentStore
	chunks atomic.Int64
	paths  atomic.Int64
}

func (s *countingArtifactHydrationStore) GetFileContents(_ context.Context, repoID string, relativePaths []string) ([]querycontract.FileContent, error) {
	s.chunks.Add(1)
	s.paths.Add(int64(len(relativePaths)))
	rows := make([]querycontract.FileContent, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		rows = append(rows, querycontract.FileContent{RepoID: repoID, RelativePath: relativePath, Content: "name: deploy"})
	}
	return rows, nil
}
