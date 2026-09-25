// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestOpenAPIAdminReplayDocuments422RefusedWorkItems pins the #7120 contract:
// the replay 422 body documents the per-id refused_work_items list.
func TestOpenAPIAdminReplayDocuments422RefusedWorkItems(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	ServeOpenAPI(w, httptest.NewRequest(http.MethodGet, "/api/v0/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	post := testutil.MustMapField(t, testutil.MustMapField(t, paths, "/api/v0/admin/replay"), "post")
	refused := testutil.MustMapField(t, testutil.MustMapField(t, post, "responses"), "422")
	schema := testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, refused, "content"), "application/json"), "schema")
	props := testutil.MustMapField(t, schema, "properties")
	items := testutil.MustMapField(t, testutil.MustMapField(t, props, "refused_work_items"), "items")
	itemProps := testutil.MustMapField(t, items, "properties")
	for _, field := range []string{"work_item_id", "failure_class", "reason"} {
		if _, ok := itemProps[field]; !ok {
			t.Fatalf("refused_work_items item schema missing %q: %v", field, itemProps)
		}
	}
}
