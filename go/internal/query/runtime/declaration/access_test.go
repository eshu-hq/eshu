// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func scopedRequest(body string, granted []string) *http.Request {
	req := httpReq(body)
	return req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode: auth.AuthModeScoped, TenantID: "t", WorkspaceID: "t", SubjectClass: "team",
		SubjectIDHash: "sha256:t", PolicyRevisionHash: "sha256:p", AllowedRepositoryIDs: granted,
	}))
}

func TestScopedCallerReadsOnlyGrantedRepositoriesBeforeTheLimit(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{
		pageRows:     []map[string]any{decl("repository:r_a", ".nvmrc", 1, 0, "node", "18", "partial", intp(18))},
		excludedRows: []map[string]any{{"spec_kind": "alias", "excluded_count": int64(1)}},
	}
	w := serve(newMux(graph), scopedRequest(`{"runtime":"node","major_below":20}`, []string{"repository:r_a"}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	for _, marker := range []string{"ORDER BY d.repo_id", "AS excluded_count"} {
		call := graph.callContaining(t, marker)
		cypher := querycontract.NormalizeCypherWhitespace(call.cypher)
		if !strings.Contains(cypher, "d.repo_id IN $allowed_repository_ids") {
			t.Errorf("%s statement is not grant-bound:\n%s", marker, cypher)
		}
		if !querycontract.GraphParamContains(call.params, "allowed_repository_ids", "repository:r_a") {
			t.Errorf("%s statement params lack the grant: %v", marker, call.params)
		}
	}
}

func TestScopedCallerWithNoGrantsGetsAnEmptyPageWithoutReadingTheGraph(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{pageRows: []map[string]any{decl("repository:r_x", ".nvmrc", 1, 0, "node", "14", "partial", intp(14))}}
	w := serve(newMux(graph), scopedRequest(`{"runtime":"node","major_below":20}`, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	if len(graph.calls) != 0 {
		t.Fatalf("a grantless caller caused %d graph reads, want 0", len(graph.calls))
	}
	got := decodeList(t, w.Body.Bytes())
	if got.Count != 0 || len(got.Declarations) != 0 || got.Truncated {
		t.Errorf("page = %+v, want empty", got)
	}
}

func TestRequestedRepoIDsNarrowWithinTheGrantNotAroundIt(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{}
	serve(newMux(graph), scopedRequest(`{"repo_ids":["repository:r_a","repository:r_b"]}`, []string{"repository:r_a"}))
	call := graph.callContaining(t, "ORDER BY d.repo_id")
	cypher := querycontract.NormalizeCypherWhitespace(call.cypher)
	if !strings.Contains(cypher, "d.repo_id IN $repo_ids") || !strings.Contains(cypher, "d.repo_id IN $allowed_repository_ids") {
		t.Fatalf("both the requested ids and the grant must bind:\n%s", cypher)
	}
}

func TestUnsupportedProfileAnswers501BeforeReading(t *testing.T) {
	t.Parallel()

	graph := &fakeGraph{}
	mux := http.NewServeMux()
	(&Handler{Neo4j: graph, Profile: querycontract.ProfileLocalLightweight}).Mount(mux)
	w := post(mux, `{}`)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body = %s", w.Code, w.Body.String())
	}
	if len(graph.calls) != 0 {
		t.Fatalf("an unsupported profile read the graph %d times", len(graph.calls))
	}
}

func TestMissingGraphAnswers503(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	(&Handler{}).Mount(mux)
	if w := post(mux, `{}`); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", w.Code, w.Body.String())
	}
}
