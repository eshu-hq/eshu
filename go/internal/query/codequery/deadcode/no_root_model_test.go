// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func TestHandleDeadCodeReportsFrameworksWithoutRootModel(t *testing.T) {
	t.Parallel()

	handler := &codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j: graph.FakeGraphReader{
			RunFn: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "php-cake", "name": "display", "labels": []any{"Function"},
						"file_path": "src/Controller/PagesController.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"framework": "cakephp",
					},
					{
						"entity_id": "php-symfony", "name": "show", "labels": []any{"Function"},
						"file_path": "src/Controller/ReportController.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"framework": "symfony",
					},
					{
						"entity_id": "php-zf1", "name": "indexAction", "labels": []any{"Function"},
						"file_path": "application/controllers/IndexController.php", "repo_id": "repo-1", "repo_name": "php-app", "language": "php",
						"framework": "zend_framework_1",
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"php","limit":20}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp["data"].(map[string]any)
	analysis := data["analysis"].(map[string]any)

	unmodeled, ok := analysis["frameworks_without_root_model"].(map[string]any)
	if !ok {
		t.Fatalf("analysis[frameworks_without_root_model] type = %T, want map[string]any", analysis["frameworks_without_root_model"])
	}
	php, ok := unmodeled["php"].([]any)
	if !ok {
		t.Fatalf("frameworks_without_root_model[php] type = %T, want []any", unmodeled["php"])
	}
	if got, want := len(php), 1; got != want {
		t.Fatalf("len(frameworks_without_root_model[php]) = %d, want %d (%#v)", got, want, php)
	}
	if got, want := php[0], "cakephp"; got != want {
		t.Fatalf("frameworks_without_root_model[php][0] = %#v, want %#v", got, want)
	}

	notes, ok := analysis["notes"].([]any)
	if !ok {
		t.Fatalf("analysis[notes] type = %T, want []any", analysis["notes"])
	}
	if !queryTestNotesContain(notes, "cakephp") {
		t.Fatalf("analysis[notes] missing cakephp no-root-model notice in %#v", notes)
	}
}
