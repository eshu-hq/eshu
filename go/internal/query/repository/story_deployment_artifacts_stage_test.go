// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// metadataOnlyContent lists files without content, as the real reader does
// (ListRepoFiles selects an empty content column); GetFileContent returns the
// fixture row with its content.
type metadataOnlyContent struct {
	content.FakePortContentStore
}

func (s metadataOnlyContent) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	files, err := s.FakePortContentStore.ListRepoFiles(ctx, repoID, limit)
	for i := range files {
		files[i].Content = ""
	}
	return files, err
}

// failingHydrationContent fails every content read, GetFileContent and the
// GetFileContents chunk read a deployment-artifact load needs for a config
// file.
type failingHydrationContent struct {
	metadataOnlyContent
}

func (failingHydrationContent) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	return nil, errors.New("content store unavailable")
}

func (failingHydrationContent) GetFileContents(context.Context, string, []string) ([]querycontract.FileContent, error) {
	return nil, errors.New("content store unavailable")
}

func deploymentArtifactsStoryFixture(contentStore querycontract.ContentStore, logs *bytes.Buffer) (*Handler, *httptest.ResponseRecorder, *http.Request) {
	handler := &Handler{
		Neo4j: graph.FakeGraphReader{
			RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{"id": "repo-da-1", "name": "repo-da-one"}, nil
			},
			RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) { return nil, nil },
		},
		Content: contentStore,
		Logger:  slog.New(slog.NewTextHandler(logs, nil)),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-da-1/story", nil)
	req.SetPathValue("repo_id", "repo-da-1")
	return handler, httptest.NewRecorder(), req
}

func deploymentArtifactsStageLine(t *testing.T, logs, event string) string {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "stage=deployment_artifacts") && strings.Contains(line, event) {
			return line
		}
	}
	t.Fatalf("no %s line for stage deployment_artifacts in:\n%s", event, logs)
	return ""
}

func TestGetRepositoryStoryDeploymentArtifactsStageReportsReadCounts(t *testing.T) {
	t.Parallel()

	store := metadataOnlyContent{FakePortContentStore: content.FakePortContentStore{RepoFiles: []querycontract.FileContent{
		{RepoID: "repo-da-1", RelativePath: "infra/main.tf", Content: "module \"m\" {\n  source = \"./m\"\n}\n"},
		{RepoID: "repo-da-1", RelativePath: "web/package.json", Content: `{"name":"x"}`},
		{RepoID: "repo-da-1", RelativePath: "env/prod.tfvars", Content: "a = 1\n"},
	}}}
	var logs bytes.Buffer
	handler, rec, req := deploymentArtifactsStoryFixture(store, &logs)

	handler.getRepositoryStory(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	line := deploymentArtifactsStageLine(t, logs.String(), "repository_query.stage_completed")
	for _, want := range []string{
		"found=", "error=false", "related_repository_count=0", "files_listed=3",
		"files_hydrated=1", "files_skipped=2", "hydrated_bytes=", "statements=",
		"blank_check_statements=1", "list_seconds=", "hydrate_seconds=", "parse_seconds=",
		// One consumed file (infra/main.tf): one chunk statement, never more
		// than one chunk in flight, the chunk size of the batched reader.
		"batch_statements=1", "batch_chunk_size=10", "batch_chunks_in_flight_max=1",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("stage_completed = %q, want it to contain %q", line, want)
		}
	}
	if strings.Contains(line, "failure_class=") {
		t.Errorf("stage_completed = %q, want no failure_class on success", line)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if limitations, _ := body["limitations"].([]any); testutil.AnySliceContains(limitations, deploymentArtifactsReadDegradedReason) {
		t.Fatalf("limitations = %#v, want no %q on success", limitations, deploymentArtifactsReadDegradedReason)
	}
}

func TestGetRepositoryStoryDeploymentArtifactsReadFailureIsNamedNotSwallowed(t *testing.T) {
	t.Parallel()

	store := failingHydrationContent{metadataOnlyContent{FakePortContentStore: content.FakePortContentStore{RepoFiles: []querycontract.FileContent{
		{RepoID: "repo-da-1", RelativePath: "infra/main.tf", Content: "x = 1\n"},
	}}}}
	var logs bytes.Buffer
	handler, rec, req := deploymentArtifactsStoryFixture(store, &logs)

	handler.getRepositoryStory(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the stage is auxiliary); body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	limitations, _ := body["limitations"].([]any)
	if !testutil.AnySliceContains(limitations, deploymentArtifactsReadDegradedReason) {
		t.Fatalf("limitations = %#v, want %q", limitations, deploymentArtifactsReadDegradedReason)
	}
	completed := deploymentArtifactsStageLine(t, logs.String(), "repository_query.stage_completed")
	for _, want := range []string{"error=true", "failure_class=" + deploymentArtifactsReadDegradedReason} {
		if !strings.Contains(completed, want) {
			t.Errorf("stage_completed = %q, want it to contain %q", completed, want)
		}
	}
	degraded := deploymentArtifactsStageLine(t, logs.String(), "repository_query.deployment_artifacts_degraded")
	for _, want := range []string{"level=WARN", "content store unavailable", "files_listed=1"} {
		if !strings.Contains(degraded, want) {
			t.Errorf("degraded line = %q, want it to contain %q", degraded, want)
		}
	}
}
