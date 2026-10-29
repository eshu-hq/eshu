// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type pagedDockerInstallStore struct {
	querycontract.ContentStore
	rows    []querycontract.EntityContent
	calls   int
	repoIDs []string
}

func (s *pagedDockerInstallStore) SearchDockerfileInstallEntities(ctx context.Context, name string, repoIDs []string, afterRepo, afterPath string, afterLine int, afterID string, limit int) ([]querycontract.EntityContent, error) {
	s.calls++
	s.repoIDs = slices.Clone(repoIDs)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]querycontract.EntityContent, 0, limit)
	for _, row := range s.rows {
		if row.EntityName != name || (repoIDs != nil && !slices.Contains(repoIDs, row.RepoID)) {
			continue
		}
		if row.RepoID < afterRepo || row.RepoID == afterRepo && (row.RelativePath < afterPath || row.RelativePath == afterPath && (row.StartLine < afterLine || row.StartLine == afterLine && row.EntityID <= afterID)) {
			continue
		}
		result = append(result, row)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func dockerInstallSearchPage(t *testing.T, mux *http.ServeMux, ctx context.Context, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/dockerfile-installs/search", bytes.NewReader(raw)).WithContext(ctx)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	var data map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
		t.Fatalf("response JSON: %v; body=%s", err, res.Body.String())
	}
	return res.Code, data
}

func TestDockerfileInstallSearchPaginatesSameLineAcrossRepositories(t *testing.T) {
	store := &pagedDockerInstallStore{rows: []querycontract.EntityContent{
		{EntityID: "id-a", RepoID: "repo-a", RelativePath: "Dockerfile", EntityName: "curl", StartLine: 5, Metadata: map[string]any{"manager": "apt-get"}},
		{EntityID: "id-b", RepoID: "repo-a", RelativePath: "Dockerfile", EntityName: "curl", StartLine: 5, Metadata: map[string]any{"manager": "apt-get", "version": "8.2"}},
		{EntityID: "id-c", RepoID: "repo-b", RelativePath: "Dockerfile", EntityName: "curl", StartLine: 9, Metadata: map[string]any{"manager": "apk"}},
	}}
	mux := http.NewServeMux()
	(&ContentHandler{Content: store}).Mount(mux)
	status, first := dockerInstallSearchPage(t, mux, context.Background(), map[string]any{"package": "curl", "limit": 2})
	if status != http.StatusOK || first["truncated"] != true || len(first["matches"].([]any)) != 2 {
		t.Fatalf("first page status=%d data=%#v", status, first)
	}
	cursor, ok := first["next_cursor"].(map[string]any)
	if !ok || cursor["entity_id"] != "id-b" || cursor["basis"] == "" {
		t.Fatalf("first cursor = %#v", first["next_cursor"])
	}
	status, second := dockerInstallSearchPage(t, mux, context.Background(), map[string]any{"package": "curl", "limit": 2, "cursor": cursor})
	matches, _ := second["matches"].([]any)
	if status != http.StatusOK || second["truncated"] != false || len(matches) != 1 || matches[0].(map[string]any)["entity_id"] != "id-c" {
		t.Fatalf("second page status=%d data=%#v", status, second)
	}
	status, _ = dockerInstallSearchPage(t, mux, context.Background(), map[string]any{"package": "other", "limit": 2, "cursor": cursor})
	if status != http.StatusBadRequest {
		t.Fatalf("cross-query cursor status=%d, want 400", status)
	}
}

func TestDockerfileInstallSearchHonorsEmptyAndScopedGrants(t *testing.T) {
	store := &pagedDockerInstallStore{rows: []querycontract.EntityContent{{EntityID: "id-b", RepoID: "repo-b", RelativePath: "Dockerfile", EntityName: "curl", StartLine: 1}}}
	mux := http.NewServeMux()
	(&ContentHandler{Content: store}).Mount(mux)
	empty := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{Mode: auth.AuthModeScoped})
	status, data := dockerInstallSearchPage(t, mux, empty, map[string]any{"package": "curl"})
	if status != http.StatusOK || len(data["matches"].([]any)) != 0 || store.calls != 0 {
		t.Fatalf("empty grant status=%d data=%#v calls=%d", status, data, store.calls)
	}
	granted := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{Mode: auth.AuthModeScoped, AllowedRepositoryIDs: []string{"repo-b"}})
	status, data = dockerInstallSearchPage(t, mux, granted, map[string]any{"package": "curl"})
	if status != http.StatusOK || len(data["matches"].([]any)) != 1 || !slices.Equal(store.repoIDs, []string{"repo-b"}) {
		t.Fatalf("scoped grant status=%d data=%#v repoIDs=%#v", status, data, store.repoIDs)
	}
}

func TestDockerfileInstallSearchCursorBindsRepositoryGrant(t *testing.T) {
	store := &pagedDockerInstallStore{rows: []querycontract.EntityContent{
		{EntityID: "a", RepoID: "repo-a", RelativePath: "Dockerfile", EntityName: "curl", StartLine: 1},
		{EntityID: "b", RepoID: "repo-a", RelativePath: "Dockerfile", EntityName: "curl", StartLine: 2},
	}}
	mux := http.NewServeMux()
	(&ContentHandler{Content: store}).Mount(mux)
	grantedA := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{Mode: auth.AuthModeScoped, AllowedRepositoryIDs: []string{"repo-a"}})
	status, first := dockerInstallSearchPage(t, mux, grantedA, map[string]any{"package": "curl", "limit": 1})
	if status != http.StatusOK || first["truncated"] != true {
		t.Fatalf("first page status=%d data=%#v", status, first)
	}
	grantedB := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{Mode: auth.AuthModeScoped, AllowedRepositoryIDs: []string{"repo-b"}})
	status, _ = dockerInstallSearchPage(t, mux, grantedB, map[string]any{"package": "curl", "limit": 1, "cursor": first["next_cursor"]})
	if status != http.StatusBadRequest || store.calls != 1 {
		t.Fatalf("changed grant cursor status=%d store calls=%d, want 400 and one call", status, store.calls)
	}
}

func TestDockerfileInstallSearchPreservesURLCharacters(t *testing.T) {
	name := "Example.org/pkg%20archive_v1.tar.gz"
	store := &pagedDockerInstallStore{rows: []querycontract.EntityContent{{EntityID: "url", RepoID: "repo-a", RelativePath: "Dockerfile", EntityName: name, StartLine: 1}}}
	mux := http.NewServeMux()
	(&ContentHandler{Content: store}).Mount(mux)
	status, data := dockerInstallSearchPage(t, mux, context.Background(), map[string]any{"package": name})
	if status != http.StatusOK || len(data["matches"].([]any)) != 1 {
		t.Fatalf("escaped URL path status=%d data=%#v", status, data)
	}
	status, data = dockerInstallSearchPage(t, mux, context.Background(), map[string]any{"package": "example.org/pkg%20archive_v1.tar.gz"})
	if status != http.StatusOK || len(data["matches"].([]any)) != 0 {
		t.Fatalf("changed URL host case status=%d data=%#v, want no exact match", status, data)
	}
}

func TestDockerfileInstallSearchKeepsDeclaredPackageCase(t *testing.T) {
	store := &pagedDockerInstallStore{rows: []querycontract.EntityContent{{EntityID: "pkg", RepoID: "repo-a", RelativePath: "Dockerfile", EntityName: "Curl", StartLine: 1}}}
	mux := http.NewServeMux()
	(&ContentHandler{Content: store}).Mount(mux)
	status, data := dockerInstallSearchPage(t, mux, context.Background(), map[string]any{"package": "curl"})
	if status != http.StatusOK || len(data["matches"].([]any)) != 0 {
		t.Fatalf("lowercase query status=%d data=%#v, want no exact match", status, data)
	}
	status, data = dockerInstallSearchPage(t, mux, context.Background(), map[string]any{"package": "Curl"})
	if status != http.StatusOK || len(data["matches"].([]any)) != 1 {
		t.Fatalf("declared case status=%d data=%#v, want one match", status, data)
	}
}

func TestDockerfileInstallSearchLimitsMatchingRowsAcrossLargeSameLineSet(t *testing.T) {
	rows := make([]querycontract.EntityContent, 0, 101)
	for i := 0; i < 101; i++ {
		rows = append(rows, querycontract.EntityContent{
			EntityID: "id-" + strconv.FormatInt(int64(1000+i), 10), RepoID: "repo-a",
			RelativePath: "Dockerfile", EntityName: "curl", StartLine: 7,
		})
	}
	store := &pagedDockerInstallStore{rows: rows}
	mux := http.NewServeMux()
	(&ContentHandler{Content: store}).Mount(mux)
	var cursor any
	var ids []string
	for page := 0; page < 3; page++ {
		body := map[string]any{"package": "curl", "limit": 50}
		if cursor != nil {
			body["cursor"] = cursor
		}
		status, data := dockerInstallSearchPage(t, mux, context.Background(), body)
		if status != http.StatusOK {
			t.Fatalf("page %d status=%d data=%#v", page, status, data)
		}
		for _, item := range data["matches"].([]any) {
			ids = append(ids, item.(map[string]any)["entity_id"].(string))
		}
		cursor = data["next_cursor"]
		if page < 2 && data["truncated"] != true || page == 2 && data["truncated"] != false {
			t.Fatalf("page %d truncation=%#v", page, data["truncated"])
		}
	}
	if len(ids) != 101 || !slices.Equal(ids, func() []string {
		wanted := make([]string, 0, len(rows))
		for _, row := range rows {
			wanted = append(wanted, row.EntityID)
		}
		return wanted
	}()) {
		t.Fatalf("paged IDs count=%d; first=%#v last=%#v", len(ids), ids[:min(3, len(ids))], ids[max(0, len(ids)-3):])
	}
}

func TestDockerfileInstallSearchRejectsUnauthorizedRepositorySelector(t *testing.T) {
	store := &pagedDockerInstallStore{rows: []querycontract.EntityContent{{EntityID: "private", RepoID: "repo-b", RelativePath: "Dockerfile", EntityName: "curl", StartLine: 1}}}
	mux := http.NewServeMux()
	(&ContentHandler{Content: store}).Mount(mux)
	granted := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{Mode: auth.AuthModeScoped, AllowedRepositoryIDs: []string{"repo-a"}})
	status, _ := dockerInstallSearchPage(t, mux, granted, map[string]any{"package": "curl", "repo_ids": []string{"repo-b"}})
	if status == http.StatusOK || store.calls != 0 {
		t.Fatalf("denied selector status=%d store calls=%d, want an error before search", status, store.calls)
	}
}

func TestDockerfileInstallSearchHonorsCancellation(t *testing.T) {
	store := &pagedDockerInstallStore{}
	mux := http.NewServeMux()
	(&ContentHandler{Content: store}).Mount(mux)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status, data := dockerInstallSearchPage(t, mux, ctx, map[string]any{"package": "curl"})
	if status == http.StatusOK || data["error"] == nil || store.calls != 1 {
		t.Fatalf("canceled status=%d data=%#v calls=%d", status, data, store.calls)
	}
}
