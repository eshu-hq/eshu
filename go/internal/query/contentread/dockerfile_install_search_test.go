// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type dockerInstallEntityStore struct {
	querycontract.ContentStore
	rows []querycontract.EntityContent
}

func (s dockerInstallEntityStore) SearchDockerfileInstallEntities(_ context.Context, _ string, _ []string, _, _ string, _ int, _ string, limit int) ([]querycontract.EntityContent, error) {
	if len(s.rows) > limit {
		return s.rows[:limit], nil
	}
	return s.rows, nil
}

func TestSearchDockerfileInstallsReturnsPersistedEntities(t *testing.T) {
	store := dockerInstallEntityStore{rows: []querycontract.EntityContent{
		{EntityID: "e1", RepoID: "repo-a", RelativePath: "Dockerfile", EntityType: "DockerfileInstall", EntityName: "curl", StartLine: 2, Metadata: map[string]any{"manager": "apk", "category": "package", "stage": "runtime"}},
	}}
	handler := &ContentHandler{Content: store}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/dockerfile-installs/search", strings.NewReader(`{"package":"curl","limit":10}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"entity_id":"e1"`) || !strings.Contains(res.Body.String(), `"manager":"apk"`) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
