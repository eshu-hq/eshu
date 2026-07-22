// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type stubCloudResourceListStore struct {
	rows   []CloudResourceListIdentity
	err    error
	calls  int
	filter CloudResourceListPageFilter
}

func (s *stubCloudResourceListStore) ListCloudResourceIdentities(
	_ context.Context,
	filter CloudResourceListPageFilter,
) ([]CloudResourceListIdentity, error) {
	s.calls++
	s.filter = filter
	if s.err != nil {
		return nil, s.err
	}
	return append([]CloudResourceListIdentity(nil), s.rows...), nil
}

func newPagedCloudHandler(graph GraphQuery, store CloudResourceListStore) http.Handler {
	handler := &InfraHandler{Neo4j: graph, CloudResources: store}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func TestListCloudResourcesSelectsAuthorizedPageBeforeGraphHydration(t *testing.T) {
	t.Parallel()

	store := &stubCloudResourceListStore{rows: []CloudResourceListIdentity{
		{UID: "a1", ResourceType: "aws_iam_role"},
		{UID: "b2", ResourceType: "aws_s3_bucket"},
		{UID: "c3", ResourceType: "aws_sqs_queue"},
	}}
	graph := &stubCloudGraphQuery{rows: []map[string]any{
		cloudRowWithUID("b2", "aws_s3_bucket", "bucket-b"),
		cloudRowWithUID("a1", "aws_iam_role", "role-a"),
	}}
	mux := newPagedCloudHandler(graph, store)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/cloud/resources?provider=aws&region=us-east-1&limit=2", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repository:allowed"},
		AllowedScopeIDs:      []string{"scope:allowed"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.filter.Limit, 3; got != want {
		t.Fatalf("store limit = %d, want %d (page limit+1)", got, want)
	}
	if store.filter.AllScopes {
		t.Fatal("store AllScopes = true, want false")
	}
	if got, want := store.filter.AllowedRepositoryIDs, []string{"repository:allowed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := store.filter.AllowedScopeIDs, []string{"scope:allowed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedScopeIDs = %#v, want %#v", got, want)
	}
	if got, want := store.filter.Provider, "aws"; got != want {
		t.Fatalf("store provider = %q, want %q", got, want)
	}
	if got, want := graph.lastParams["uids"], []string{"a1", "b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("graph uids = %#v, want %#v", got, want)
	}
	if !strings.Contains(graph.lastCypher, "WHERE n.uid IN $uids") {
		t.Fatalf("graph hydration is not uid-bounded: %s", graph.lastCypher)
	}
	if strings.Contains(graph.lastCypher, "ORDER BY n.resource_type") {
		t.Fatalf("graph hydration must not rescan/order the corpus: %s", graph.lastCypher)
	}

	body := decodeCloudBody(t, w)
	resources := body["resources"].([]any)
	if got, want := resources[0].(map[string]any)["id"], "a1"; got != want {
		t.Fatalf("first resource id = %v, want %v (store page order)", got, want)
	}
	if got, want := resources[1].(map[string]any)["id"], "b2"; got != want {
		t.Fatalf("second resource id = %v, want %v (store page order)", got, want)
	}
	if got := body["truncated"]; got != true {
		t.Fatalf("truncated = %v, want true", got)
	}
	cursor := body["next_cursor"].(map[string]any)
	if got, want := cursor["after_resource_type"], "aws_s3_bucket"; got != want {
		t.Fatalf("cursor resource type = %v, want %v", got, want)
	}
	if got, want := cursor["after_id"], "b2"; got != want {
		t.Fatalf("cursor id = %v, want %v", got, want)
	}
}

func TestListCloudResourcesRejectsMalformedCursorBeforeReads(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/cloud/resources?after_id=b2",
		"/api/v0/cloud/resources?after_resource_type=aws_iam_role",
	} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			store := &stubCloudResourceListStore{}
			graph := &stubCloudGraphQuery{}
			w := httptest.NewRecorder()
			newPagedCloudHandler(graph, store).ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if store.calls != 0 || graph.lastCypher != "" {
				t.Fatalf("malformed cursor reached stores: store calls=%d graph=%q", store.calls, graph.lastCypher)
			}
		})
	}
}

func TestListCloudResourcesEmptyScopedGrantShortCircuits(t *testing.T) {
	t.Parallel()

	store := &stubCloudResourceListStore{}
	graph := &stubCloudGraphQuery{}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:     AuthModeScoped,
		TenantID: "tenant-empty",
	}))
	w := httptest.NewRecorder()
	newPagedCloudHandler(graph, store).ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.calls != 0 || graph.lastCypher != "" {
		t.Fatalf("empty grant reached stores: store calls=%d graph=%q", store.calls, graph.lastCypher)
	}
	body := decodeCloudBody(t, w)
	if got := int(body["count"].(float64)); got != 0 {
		t.Fatalf("count = %d, want 0", got)
	}
}

func TestListCloudResourcesFailsClosedOnHydrationDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rows []map[string]any
	}{
		{name: "missing", rows: nil},
		{name: "duplicate", rows: []map[string]any{
			cloudRowWithUID("a1", "aws_iam_role", "role-a"),
			cloudRowWithUID("a1", "aws_iam_role", "role-a"),
		}},
		{name: "unexpected", rows: []map[string]any{cloudRowWithUID("z9", "aws_iam_role", "role-z")}},
		{name: "type mismatch", rows: []map[string]any{cloudRowWithUID("a1", "aws_s3_bucket", "bucket-a")}},
		{name: "id mismatch", rows: []map[string]any{func() map[string]any {
			row := cloudRowWithUID("a1", "aws_iam_role", "role-a")
			row["id"] = "different"
			return row
		}()}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &stubCloudResourceListStore{rows: []CloudResourceListIdentity{{
				UID: "a1", ResourceType: "aws_iam_role",
			}}}
			graph := &stubCloudGraphQuery{rows: tt.rows}
			w := httptest.NewRecorder()
			newPagedCloudHandler(graph, store).ServeHTTP(
				w, httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources", nil))
			if got, want := w.Code, http.StatusInternalServerError; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestListCloudResourcesStoreErrorSkipsGraph(t *testing.T) {
	t.Parallel()

	store := &stubCloudResourceListStore{err: errors.New("postgres unavailable")}
	graph := &stubCloudGraphQuery{}
	w := httptest.NewRecorder()
	newPagedCloudHandler(graph, store).ServeHTTP(
		w, httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources", nil))
	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if graph.lastCypher != "" {
		t.Fatalf("store error reached graph: %s", graph.lastCypher)
	}
}

func TestListCloudResourcesPageCardinalities(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		rows      []CloudResourceListIdentity
		wantCount int
		truncated bool
	}{
		{name: "zero", wantCount: 0},
		{name: "one", rows: cloudResourceIdentities(1), wantCount: 1},
		{name: "exact limit", rows: cloudResourceIdentities(2), wantCount: 2},
		{name: "limit plus one", rows: cloudResourceIdentities(3), wantCount: 2, truncated: true},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			graphRows := make([]map[string]any, 0, len(tt.rows))
			for _, identity := range tt.rows {
				graphRows = append(graphRows, cloudRowWithUID(identity.UID, identity.ResourceType, identity.UID))
			}
			w := httptest.NewRecorder()
			newPagedCloudHandler(
				&stubCloudGraphQuery{rows: graphRows},
				&stubCloudResourceListStore{rows: tt.rows},
			).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v0/cloud/resources?limit=2", nil))
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			body := decodeCloudBody(t, w)
			if got := int(body["count"].(float64)); got != tt.wantCount {
				t.Fatalf("count = %d, want %d", got, tt.wantCount)
			}
			if got := body["truncated"].(bool); got != tt.truncated {
				t.Fatalf("truncated = %t, want %t", got, tt.truncated)
			}
		})
	}
}

func cloudResourceIdentities(count int) []CloudResourceListIdentity {
	rows := make([]CloudResourceListIdentity, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, CloudResourceListIdentity{
			UID: "uid-" + string(rune('a'+i)), ResourceType: "aws_resource",
		})
	}
	return rows
}

func cloudRowWithUID(uid, resourceType, name string) map[string]any {
	row := cloudRow(uid, resourceType, name)
	row["uid"] = uid
	return row
}
