// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestDispatchToolRepoStoryReturnsStructuredEnvelopeData(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	handler := &query.RepositoryHandler{
		Neo4j: repoStoryGraphReader{t: t, repoID: "repo-story"},
	}
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_repo_story",
		map[string]any{"repo_id": "repo-story"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured repo story envelope")
	}
	if result.Envelope.Truth == nil {
		t.Fatal("dispatchTool() envelope truth is nil, want repo story truth")
	}
	if got, want := result.Envelope.Truth.Capability, "platform_impact.context_overview"; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	repository, ok := data["repository"].(map[string]any)
	if !ok {
		t.Fatalf("repository type = %T, want map[string]any", data["repository"])
	}
	if got, want := repository["id"], "repo-story"; got != want {
		t.Fatalf("repository.id = %#v, want %#v", got, want)
	}
}

func TestDispatchToolRepositoryStatsReturnsStructuredEnvelopeData(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	handler := &query.RepositoryHandler{
		Neo4j: repoStoryGraphReader{t: t, repoID: "repo-stats"},
	}
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_repository_stats",
		map[string]any{"repo_id": "repo-stats"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured repository stats envelope")
	}
	if result.Envelope.Truth == nil {
		t.Fatal("dispatchTool() envelope truth is nil, want repository stats truth")
	}
	if got, want := result.Envelope.Truth.Capability, "platform_impact.context_overview"; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	coverage, ok := data["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage type = %T, want map[string]any", data["coverage"])
	}
	if got, want := coverage["query_shape"], "repository_identity_only"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
}

type repoStoryGraphReader struct {
	t      *testing.T
	repoID string
}

func (r repoStoryGraphReader) RunSingle(
	_ context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
		r.t.Fatalf("RunSingle cypher = %q, want repository base lookup", cypher)
	}
	if got := params["repo_id"]; got != r.repoID {
		r.t.Fatalf("repo_id param = %#v, want %#v", got, r.repoID)
	}
	return map[string]any{
		"id":         r.repoID,
		"name":       "story-service",
		"path":       "/repos/story-service",
		"local_path": "/repos/story-service",
		"has_remote": false,
	}, nil
}

func (r repoStoryGraphReader) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	if got := params["repo_id"]; got != r.repoID {
		r.t.Fatalf("repo_id param = %#v, want %#v", got, r.repoID)
	}
	switch {
	case strings.Contains(cypher, "RETURN count(DISTINCT f) AS count"):
		return []map[string]any{{"count": int64(7)}}, nil
	case strings.Contains(cypher, "RETURN f.language AS language, count(DISTINCT f) AS file_count"):
		return []map[string]any{{"language": "go", "file_count": int64(7)}}, nil
	case strings.Contains(cypher, "RETURN w.name AS workload_name"):
		return []map[string]any{{"workload_name": "story-service"}}, nil
	case strings.Contains(cypher, "RETURN p.type AS platform_type"):
		return []map[string]any{{"platform_type": "ecs"}}, nil
	case strings.Contains(cypher, "RETURN count(DISTINCT dep) AS count"):
		return []map[string]any{{"count": int64(1)}}, nil
	default:
		return nil, nil
	}
}
