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
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type repoContextDispatchReader struct {
	t      *testing.T
	repoID string
}

func (r repoContextDispatchReader) RunSingle(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
	if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
		return nil, nil
	}
	return map[string]any{
		"id": r.repoID, "name": "context-service", "path": "/repos/context-service",
		"local_path": "/repos/context-service", "repo_slug": "acme/context-service",
		"remote_url": "https://github.com/acme/context-service.git", "has_remote": true,
	}, nil
}

func (r repoContextDispatchReader) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return nil, nil
}

// TestDispatchToolRepoContextDefaultsEvidenceDetailToHandles proves the MCP
// route builder forwards evidence_detail=handles when the caller names no
// value, and forwards an explicit full verbatim (#10654).
func TestDispatchToolRepoContextDefaultsEvidenceDetailToHandles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"absent means handles", map[string]any{"repo_id": "repo-context"}, "handles"},
		{"explicit full wins", map[string]any{"repo_id": "repo-context", "evidence_detail": "full"}, "full"},
		{"explicit handles stays", map[string]any{"repo_id": "repo-context", "evidence_detail": "handles"}, "handles"},
	} {
		mux := http.NewServeMux()
		handler := &query.RepositoryHandler{
			Neo4j: repoContextDispatchReader{t: t, repoID: "repo-context"},
		}
		handler.Mount(mux)

		result, err := dispatchTool(
			context.Background(),
			mux,
			"get_repo_context",
			tc.args,
			"",
			slog.New(slog.NewTextHandler(io.Discard, nil)),
		)
		if err != nil {
			t.Fatalf("%s: dispatchTool() error = %v, want nil", tc.name, err)
		}
		if result.Envelope == nil {
			t.Fatalf("%s: dispatchTool() envelope is nil, want structured repo context envelope", tc.name)
		}
		data, ok := result.Envelope.Data.(map[string]any)
		if !ok {
			t.Fatalf("%s: envelope data type = %T, want map[string]any", tc.name, result.Envelope.Data)
		}
		if got := querycontract.StringVal(data, "evidence_detail"); got != tc.want {
			t.Fatalf("%s: evidence_detail = %q, want %q", tc.name, got, tc.want)
		}
	}
}
