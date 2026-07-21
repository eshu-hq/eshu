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

// stubIaCResourceGraph records the Cypher and params sent to the graph and
// returns a canned page of rows. It lets the IaC resource list tests assert
// both the wire contract and the bounded query shape.
type stubIaCResourceGraph struct {
	rows       []map[string]any
	err        error
	lastCypher string
	lastParams map[string]any
	calls      int
}

func (s *stubIaCResourceGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	s.calls++
	s.lastCypher = cypher
	s.lastParams = params
	if s.err != nil {
		return nil, s.err
	}
	return append([]map[string]any(nil), s.rows...), nil
}

func (s *stubIaCResourceGraph) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, errors.New("RunSingle not used by IaC resource list")
}

func iacResourceNode(id, name, resourceType, provider string) map[string]any {
	return map[string]any{
		"id":            id,
		"name":          name,
		"resource_name": name,
		"type":          resourceType,
		"provider":      provider,
		"line_number":   int64(1),
	}
}

type iacResourceListResponse struct {
	Kind       string           `json:"kind"`
	Count      int              `json:"count"`
	Limit      int              `json:"limit"`
	Truncated  bool             `json:"truncated"`
	NextCursor map[string]any   `json:"next_cursor"`
	Resources  []iacResourceRow `json:"resources"`
}

func decodeIaCResourceList(t *testing.T, w *httptest.ResponseRecorder) iacResourceListResponse {
	t.Helper()
	var body iacResourceListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; raw = %s", err, w.Body.String())
	}
	return body
}

func TestIaCResourcesReturns503WhenGraphMissing(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestIaCResourcesHappyPath(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: []map[string]any{
		iacResourceNode("a1", "aws_s3_bucket.logs", "aws_s3_bucket", "aws"),
		iacResourceNode("b2", "aws_iam_role.app", "aws_iam_role", "aws"),
	}}
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	body := decodeIaCResourceList(t, w)
	if body.Kind != "resource" {
		t.Fatalf("kind = %q, want resource", body.Kind)
	}
	if body.Count != 2 || len(body.Resources) != 2 {
		t.Fatalf("count = %d, resources = %d, want 2", body.Count, len(body.Resources))
	}
	if body.Truncated {
		t.Fatalf("truncated = true, want false")
	}
	if body.Limit != 10 {
		t.Fatalf("limit = %d, want 10", body.Limit)
	}
	// limit+1 truncation: the handler must request limit+1 rows.
	if got := graph.lastParams["limit"]; got != 11 {
		t.Fatalf("graph limit param = %v, want 11", got)
	}
	if !strings.Contains(graph.lastCypher, "MATCH (n:TerraformResource)") {
		t.Fatalf("cypher missing TerraformResource anchor: %s", graph.lastCypher)
	}
	if !strings.Contains(graph.lastCypher, "ORDER BY n.name, n.id") {
		t.Fatalf("cypher missing deterministic order: %s", graph.lastCypher)
	}
	if first := body.Resources[0]; first.ID != "a1" || first.Type != "aws_s3_bucket" {
		t.Fatalf("first row = %+v, want id a1 type aws_s3_bucket", first)
	}
}

func TestIaCResourcesEmpty(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: nil}
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	body := decodeIaCResourceList(t, w)
	if body.Count != 0 || len(body.Resources) != 0 || body.Truncated {
		t.Fatalf("empty result mismatch: %+v", body)
	}
	if body.NextCursor != nil {
		t.Fatalf("next_cursor should be absent on empty page: %+v", body.NextCursor)
	}
}

func TestIaCResourcesLimitValidation(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{}
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, raw := range []string{"0", "-1", "201", "abc"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit="+raw, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestIaCResourcesDefaultLimitWhenAbsent(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: nil}
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	body := decodeIaCResourceList(t, w)
	if body.Limit != iacResourcesDefaultLimit {
		t.Fatalf("limit = %d, want default %d", body.Limit, iacResourcesDefaultLimit)
	}
	inventory := handler.Inventory.(*stubIaCInventoryStore)
	if got, want := inventory.lastSearch.Limit, iacResourcesDefaultLimit+1; got != want {
		t.Fatalf("inventory search limit = %v, want %d", got, want)
	}
}

func TestIaCResourcesTruncationAndCursor(t *testing.T) {
	t.Parallel()

	// limit=2 means the handler asks for 3 rows; returning 3 signals more pages.
	graph := &stubIaCResourceGraph{rows: []map[string]any{
		iacResourceNode("a1", "aws_iam_role.a", "aws_iam_role", "aws"),
		iacResourceNode("b2", "aws_iam_role.b", "aws_iam_role", "aws"),
		iacResourceNode("c3", "aws_s3_bucket.c", "aws_s3_bucket", "aws"),
	}}
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body := decodeIaCResourceList(t, w)
	if !body.Truncated {
		t.Fatalf("truncated = false, want true")
	}
	if body.Count != 2 || len(body.Resources) != 2 {
		t.Fatalf("returned %d rows, want 2", len(body.Resources))
	}
	if body.NextCursor == nil {
		t.Fatalf("next_cursor missing on truncated page")
	}
	if got, want := body.NextCursor["after_name"], "aws_iam_role.b"; got != want {
		t.Fatalf("next_cursor after_name = %v, want %v", got, want)
	}
	if got, want := body.NextCursor["after_id"], "b2"; got != want {
		t.Fatalf("next_cursor after_id = %v, want %v", got, want)
	}
}

func TestIaCResourcesFilters(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: nil}
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=5&type=aws_iam_role&provider=aws&module=app", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	inventory := handler.Inventory.(*stubIaCInventoryStore)
	if got := inventory.lastSearch.Type; got != "aws_iam_role" {
		t.Fatalf("type filter = %v, want aws_iam_role", got)
	}
	if got := inventory.lastSearch.Provider; got != "aws" {
		t.Fatalf("provider filter = %v, want aws", got)
	}
	if got := inventory.lastSearch.Module; got != "app" {
		t.Fatalf("module filter = %v, want app", got)
	}
	if graph.calls != 0 {
		t.Fatalf("graph calls = %d, want 0 for an empty current candidate page", graph.calls)
	}
}

func TestIaCResourcesModuleFilterIncludesQuotedAddresses(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: nil}
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=5&module=api-node", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	inventory := handler.Inventory.(*stubIaCInventoryStore)
	if got := inventory.lastSearch.Module; got != "api-node" {
		t.Fatalf("module filter = %v, want api-node", got)
	}
}

func TestIaCResourcesModuleKind(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: []map[string]any{
		{"id": "m1", "name": "vpc", "line_number": int64(0)},
	}}
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?kind=module&module=vpc&limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(graph.lastCypher, "MATCH (n:TerraformModule)") {
		t.Fatalf("cypher missing TerraformModule anchor: %s", graph.lastCypher)
	}
	inventory := handler.Inventory.(*stubIaCInventoryStore)
	if got := inventory.lastSearch.Module; got != "vpc" {
		t.Fatalf("module filter = %v, want vpc", got)
	}
	body := decodeIaCResourceList(t, w)
	if body.Kind != "module" {
		t.Fatalf("kind = %q, want module", body.Kind)
	}
	if len(body.Resources) != 1 || body.Resources[0].Module != "vpc" {
		t.Fatalf("module row mismatch: %+v", body.Resources)
	}
}

func TestIaCResourcesDataSourceKind(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{rows: []map[string]any{
		iacResourceNode("d1", "data.aws_iam_policy_document.app", "aws_iam_policy_document", "aws"),
	}}
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?kind=data-source&type=aws_iam_policy_document&limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(graph.lastCypher, "MATCH (n:TerraformDataSource)") {
		t.Fatalf("cypher missing TerraformDataSource anchor: %s", graph.lastCypher)
	}
	inventory := handler.Inventory.(*stubIaCInventoryStore)
	if got := inventory.lastSearch.Type; got != "aws_iam_policy_document" {
		t.Fatalf("data-source type filter = %q, want aws_iam_policy_document", got)
	}
}

func TestIaCResourcesInvalidKind(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{}
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?kind=bogus&limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if graph.calls != 0 {
		t.Fatalf("graph called %d times for invalid kind, want 0", graph.calls)
	}
}

func TestIaCResourcesUnsupportedProfile(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{}
	handler := newIaCResourceTestHandler(graph)
	handler.Profile = ProfileLocalLightweight
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if graph.calls != 0 {
		t.Fatalf("graph called %d times for unsupported profile, want 0", graph.calls)
	}
}

func TestIaCResourcesGraphError(t *testing.T) {
	t.Parallel()

	graph := &stubIaCResourceGraph{
		rows: []map[string]any{iacResourceNode("a1", "aws_s3_bucket.logs", "aws_s3_bucket", "aws")},
		err:  errors.New("boom"),
	}
	handler := newIaCResourceTestHandler(graph)
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestModuleNameFromResourceName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`module."api-node".aws_iam_role.this`:      "api-node",
		"module.vpc.aws_subnet.private":            "vpc",
		`module.gha_role["api-node"].aws_iam.this`: "gha_role",
		"aws_s3_bucket.logs":                       "",
		"module.":                                  "",
		`module."unterminated`:                     "",
	}
	for in, want := range cases {
		if got := moduleNameFromResourceName(in); got != want {
			t.Fatalf("moduleNameFromResourceName(%q) = %q, want %q", in, got, want)
		}
	}
}
