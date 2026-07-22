// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubCloudGraphQuery returns a fixed row set and records the last Cypher and
// params so tests can assert the bounded query shape.
type stubCloudGraphQuery struct {
	rows       []map[string]any
	err        error
	lastCypher string
	lastParams map[string]any
}

func (s *stubCloudGraphQuery) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	s.lastCypher = cypher
	s.lastParams = params
	if s.err != nil {
		return nil, s.err
	}
	rows := append([]map[string]any(nil), s.rows...)
	uids, bounded := params["uids"].([]string)
	if !bounded {
		return rows, nil
	}
	allowed := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		allowed[uid] = struct{}{}
	}
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if _, ok := allowed[StringVal(row, "uid")]; ok {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

func (s *stubCloudGraphQuery) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, errors.New("RunSingle not used by cloud resources")
}

func cloudRow(id, resourceType, name string) map[string]any {
	return map[string]any{
		"uid":           id,
		"id":            id,
		"resource_type": resourceType,
		"name":          name,
		"provider":      "aws",
		"region":        "us-east-1",
		"account_id":    "123456789012",
		"arn":           "arn:aws:iam::123456789012:role/" + name,
		"service_name":  "row.service_name",
		"state":         "",
	}
}

func newCloudHandlerWith(graph GraphQuery) http.Handler {
	store := &stubCloudResourceListStore{}
	if stub, ok := graph.(*stubCloudGraphQuery); ok {
		for _, row := range stub.rows {
			store.rows = append(store.rows, CloudResourceListIdentity{
				UID: StringVal(row, "uid"), ResourceType: StringVal(row, "resource_type"),
			})
		}
	}
	handler := &InfraHandler{Neo4j: graph, CloudResources: store}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func decodeCloudBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; raw=%s", err, w.Body.String())
	}
	return body
}

func TestListCloudResourcesHappyPath(t *testing.T) {
	t.Parallel()

	graph := &stubCloudGraphQuery{rows: []map[string]any{
		cloudRow("a1", "aws_iam_role", "role-a"),
		cloudRow("b2", "aws_s3_bucket", "bucket-b"),
	}}
	mux := newCloudHandlerWith(graph)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources?limit=50", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	body := decodeCloudBody(t, w)
	if got, want := int(body["count"].(float64)), 2; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if body["truncated"].(bool) {
		t.Fatalf("truncated = true, want false")
	}
	resources, ok := body["resources"].([]any)
	if !ok || len(resources) != 2 {
		t.Fatalf("resources = %#v, want 2 rows", body["resources"])
	}
	first := resources[0].(map[string]any)
	if got := first["provider"]; got != "aws" {
		t.Fatalf("provider = %v, want aws", got)
	}
	// service_name placeholder must be scrubbed from the wire payload.
	if _, present := first["service_name"]; present {
		t.Fatalf("service_name placeholder leaked: %#v", first)
	}
	if !strings.Contains(graph.lastCypher, "WHERE n.uid IN $uids") {
		t.Fatalf("query missing bounded uid hydration: %s", graph.lastCypher)
	}
}

func TestListCloudResourcesEmpty(t *testing.T) {
	t.Parallel()

	mux := newCloudHandlerWith(&stubCloudGraphQuery{rows: nil})
	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	body := decodeCloudBody(t, w)
	if got, want := int(body["count"].(float64)), 0; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if body["truncated"].(bool) {
		t.Fatalf("truncated = true, want false")
	}
	if resources := body["resources"].([]any); len(resources) != 0 {
		t.Fatalf("resources = %#v, want empty", resources)
	}
}

func TestListCloudResourcesLimitValidation(t *testing.T) {
	t.Parallel()

	mux := newCloudHandlerWith(&stubCloudGraphQuery{})
	for _, raw := range []string{"0", "201", "-1", "abc"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources?limit="+raw, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("limit=%q status = %d, want %d; body = %s", raw, got, want, w.Body.String())
			}
		})
	}
}

func TestListCloudResourcesTruncationAndCursor(t *testing.T) {
	t.Parallel()

	// limit=2 -> fetch 3; returning 3 rows means truncated and the cursor
	// anchors on the 2nd (last returned) row.
	graph := &stubCloudGraphQuery{rows: []map[string]any{
		cloudRow("a1", "aws_iam_role", "role-a"),
		cloudRow("b2", "aws_iam_role", "role-b"),
		cloudRow("c3", "aws_s3_bucket", "bucket-c"),
	}}
	mux := newCloudHandlerWith(graph)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources?limit=2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	body := decodeCloudBody(t, w)
	if got, want := int(body["count"].(float64)), 2; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if !body["truncated"].(bool) {
		t.Fatalf("truncated = false, want true")
	}
	cursor, ok := body["next_cursor"].(map[string]any)
	if !ok {
		t.Fatalf("next_cursor missing: %#v", body)
	}
	if got, want := cursor["after_resource_type"], "aws_iam_role"; got != want {
		t.Fatalf("after_resource_type = %v, want %v", got, want)
	}
	if got, want := cursor["after_id"], "b2"; got != want {
		t.Fatalf("after_id = %v, want %v", got, want)
	}
}

func TestListCloudResourcesCursorAppliesKeysetPredicate(t *testing.T) {
	t.Parallel()

	graph := &stubCloudGraphQuery{rows: nil}
	store := &stubCloudResourceListStore{}
	mux := newPagedCloudHandler(graph, store)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/cloud/resources?after_resource_type=aws_iam_role&after_id=b2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.filter.AfterResourceType, "aws_iam_role"; got != want {
		t.Fatalf("after_resource_type param = %v, want %v", got, want)
	}
	if got, want := store.filter.AfterID, "b2"; got != want {
		t.Fatalf("after_id param = %v, want %v", got, want)
	}
}

func TestListCloudResourcesRejectsIncompleteCursor(t *testing.T) {
	t.Parallel()

	graph := &stubCloudGraphQuery{rows: nil}
	mux := newCloudHandlerWith(graph)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources?after_id=b2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestListCloudResourcesProviderFilter(t *testing.T) {
	t.Parallel()

	graph := &stubCloudGraphQuery{rows: []map[string]any{cloudRow("a1", "aws_iam_role", "role-a")}}
	store := &stubCloudResourceListStore{rows: []CloudResourceListIdentity{{UID: "a1", ResourceType: "aws_iam_role"}}}
	mux := newPagedCloudHandler(graph, store)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/cloud/resources?provider=aws&resource_type=aws_iam_role&region=us-east-1&account_id=123456789012", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.filter.Provider, "aws"; got != want {
		t.Fatalf("provider param = %v, want %v", got, want)
	}
	if got, want := store.filter.ResourceType, "aws_iam_role"; got != want {
		t.Fatalf("resource type param = %v, want %v", got, want)
	}
	body := decodeCloudBody(t, w)
	scope := body["scope"].(map[string]any)
	if got, want := scope["region"], "us-east-1"; got != want {
		t.Fatalf("scope.region = %v, want %v", got, want)
	}
}

func TestListCloudResourcesBackendUnavailable(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestListCloudResourcesQueryError(t *testing.T) {
	t.Parallel()

	graph := &stubCloudGraphQuery{err: errors.New("boom")}
	store := &stubCloudResourceListStore{rows: []CloudResourceListIdentity{{
		UID: "a1", ResourceType: "aws_iam_role",
	}}}
	mux := newPagedCloudHandler(graph, store)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}
