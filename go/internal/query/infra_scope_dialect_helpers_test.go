// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// dialectRecordingGraph records every Run / RunSingle call in order and lets a
// test script the RunSingle answer per statement, so the #7215 dialect tests
// can see the whole relationships probe/scoped sequence, not just the last
// statement.
type dialectRecordingGraph struct {
	runRows []map[string]any
	single  func(cypher string, params map[string]any) map[string]any
	calls   []recordedInfraCall
}

func (g *dialectRecordingGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.calls = append(g.calls, recordedInfraCall{Cypher: cypher, Params: params})
	return g.runRows, nil
}

func (g *dialectRecordingGraph) RunSingle(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
	g.calls = append(g.calls, recordedInfraCall{Cypher: cypher, Params: params})
	if g.single == nil {
		return nil, nil
	}
	return g.single(cypher, params), nil
}

// dialectGrant is one scoped-token grant shape used across the dialect tests.
type dialectGrant struct {
	name   string
	repos  []string
	scopes []string
}

// dialectGrants returns g1 (1 repo + 1 scope), g5 (5 + 5) and cap (70 + 70 =
// 140 scalars, past the 128 inline-term cap).
func dialectGrants() []dialectGrant {
	build := func(name string, n int) dialectGrant {
		g := dialectGrant{name: name}
		for i := 0; i < n; i++ {
			g.repos = append(g.repos, fmt.Sprintf("repo-%03d", i))
			g.scopes = append(g.scopes, fmt.Sprintf("scope-%03d", i))
		}
		return g
	}
	return []dialectGrant{build("g1", 1), build("g5", 5), build("cap", 70)}
}

func (g dialectGrant) auth() AuthContext {
	return AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: g.repos,
		AllowedScopeIDs:      g.scopes,
	}
}

func (g dialectGrant) filter() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilterFromContext(ContextWithAuthContext(context.Background(), g.auth()))
}

// serveInfraDialect drives one POST through the real mounted handler with the
// given backend, auth and graph, returning the recorder.
func serveInfraDialect(
	t *testing.T,
	backend querycontract.GraphBackend,
	auth *AuthContext,
	graph GraphQuery,
	path, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	handler := &InfraHandler{GraphBackend: backend, Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if auth != nil {
		req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// statementDigest hashes the exact Cypher bytes plus the canonical JSON of
// the params (encoding/json sorts map keys), so a digest match is byte
// identity of what the backend receives.
func statementDigest(t *testing.T, calls []recordedInfraCall) string {
	t.Helper()
	h := sha256.New()
	for _, call := range calls {
		params, err := json.Marshal(call.Params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		h.Write([]byte(call.Cypher))
		h.Write([]byte{0})
		h.Write(params)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// countScopeGrantIndexParams counts params bound under the SHAPE-A per-grant
// prefix (scope_grant_<i>).
func countScopeGrantIndexParams(params map[string]any) int {
	n := 0
	for key := range params {
		if strings.HasPrefix(key, querycontract.ScopeGrantInlineParamPrefix) {
			n++
		}
	}
	return n
}
