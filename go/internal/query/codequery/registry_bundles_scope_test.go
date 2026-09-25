// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// bundlesCall is one graph statement a fake reader saw.
type bundlesCall struct {
	cypher string
	params map[string]any
}

// bundlesFake serves an anchor page for the catalog read and version counts
// for the version-count read, recording every statement.
func bundlesFake(anchor []map[string]any, counts []map[string]any, calls *[]bundlesCall) fakeGraphReader {
	return fakeGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			*calls = append(*calls, bundlesCall{cypher: cypher, params: params})
			if strings.Contains(cypher, "v.package_id IN $package_ids") {
				return counts, nil
			}
			return anchor, nil
		},
	}
}

func runBundles(t *testing.T, reader fakeGraphReader, body string, auth *AuthContext) (map[string]any, int) {
	t.Helper()
	h := &CodeHandler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/bundles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	if auth != nil {
		req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
	}
	w := httptest.NewRecorder()
	h.handleSearchBundles(w, req)
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, w.Body.String())
	}
	data, _ := envelope.Data.(map[string]any)
	return data, w.Code
}

// TestSearchBundlesScopedCallerBindsPublicVisibility proves the scoped caller
// reaches the anchor read with the public-visibility term and the shared /
// all-scope caller does not (the shared key's statement is otherwise
// unchanged).
func TestSearchBundlesScopedCallerBindsPublicVisibility(t *testing.T) {
	scoped := testutil.CodeGrantScopedAuthContext([]string{"repo://tenant-a/svc"})
	scopeOnly := AuthContext{Mode: AuthModeScoped, TenantID: "t", AllowedScopeIDs: []string{"scope-a"}}
	allScopes := AuthContext{Mode: AuthModeScoped, AllScopes: true}
	shared := AuthContext{Mode: "shared"}
	cases := []struct {
		name       string
		auth       *AuthContext
		wantPublic bool
	}{
		{"scoped repository grant", &scoped, true},
		{"scoped scope-only grant", &scopeOnly, true},
		{"all-scope token", &allScopes, false},
		{"shared key", &shared, false},
		{"no auth context", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []bundlesCall
			reader := bundlesFake(nil, nil, &calls)
			if _, code := runBundles(t, reader, `{"query": "react"}`, tc.auth); code != http.StatusOK {
				t.Fatalf("status = %d, want 200", code)
			}
			if len(calls) != 1 {
				t.Fatalf("graph calls = %d, want 1 (empty page skips the version-count read)", len(calls))
			}
			if got := strings.Contains(calls[0].cypher, "p.visibility = 'public'"); got != tc.wantPublic {
				t.Fatalf("public-visibility term present = %v, want %v; cypher:\n%s", got, tc.wantPublic, calls[0].cypher)
			}
			for _, id := range []string{"allowed_repository_ids", "allowed_scope_ids"} {
				if _, bound := calls[0].params[id]; bound {
					t.Fatalf("params bind %s; Package nodes have no repo key, the grant must not leak into the statement", id)
				}
			}
		})
	}
}

// TestSearchBundlesEmptyGrantMakesNoGraphCall proves a scoped token with no
// grant fails closed to an empty, well-formed page without touching the graph.
func TestSearchBundlesEmptyGrantMakesNoGraphCall(t *testing.T) {
	empty := AuthContext{Mode: AuthModeScoped, TenantID: "t", WorkspaceID: "w"}
	var calls []bundlesCall
	reader := bundlesFake([]map[string]any{{"package_id": "pkg-1"}}, nil, &calls)
	data, code := runBundles(t, reader, `{"query": "react", "limit": 7}`, &empty)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(calls) != 0 {
		t.Fatalf("graph calls = %d (%v), want 0 for an empty grant", len(calls), calls)
	}
	bundles, ok := data["bundles"].([]any)
	if !ok || len(bundles) != 0 {
		t.Fatalf("bundles = %#v, want an empty array (not null)", data["bundles"])
	}
	if data["count"] != float64(0) || data["limit"] != float64(7) || data["truncated"] != false {
		t.Fatalf("page = %#v, want count 0, limit 7, truncated false", data)
	}
}

// TestSearchBundlesResolvesVersionCountsForThePageOnly proves the two-statement
// shape: the anchor read carries no aggregate, the count read is bound to the
// returned page (not the limit+1 probe row), and a package absent from the
// count result is zero-filled instead of dropped.
func TestSearchBundlesResolvesVersionCountsForThePageOnly(t *testing.T) {
	anchor := []map[string]any{
		{"package_id": "pkg-a", "name": "a", "ecosystem": "npm"},
		{"package_id": "pkg-b", "name": "b", "ecosystem": "npm"},
		{"package_id": "pkg-c", "name": "c", "ecosystem": "npm"},
	}
	counts := []map[string]any{{"package_id": "pkg-a", "version_count": int64(4)}}
	var calls []bundlesCall
	data, _ := runBundles(t, bundlesFake(anchor, counts, &calls), `{"ecosystem": "npm", "limit": 2}`, nil)

	if len(calls) != 2 {
		t.Fatalf("graph calls = %d, want 2 (anchor, counts)", len(calls))
	}
	if strings.Contains(calls[0].cypher, "count(") || strings.Contains(calls[0].cypher, "OPTIONAL MATCH") {
		t.Fatalf("anchor statement carries an aggregate; the pinned NornicDB drops its ORDER BY/LIMIT:\n%s", calls[0].cypher)
	}
	if got := calls[0].params["limit"]; got != 3 {
		t.Fatalf("anchor limit = %#v, want limit+1 = 3", got)
	}
	ids, _ := calls[1].params["package_ids"].([]string)
	if len(ids) != 2 || ids[0] != "pkg-a" || ids[1] != "pkg-b" {
		t.Fatalf("count read package_ids = %#v, want the returned page [pkg-a pkg-b] without the probe row", ids)
	}
	bundles, _ := data["bundles"].([]any)
	if len(bundles) != 2 || data["truncated"] != true {
		t.Fatalf("bundles = %d truncated = %v, want 2 and true", len(bundles), data["truncated"])
	}
	want := map[string]float64{"pkg-a": 4, "pkg-b": 0}
	for _, item := range bundles {
		row := item.(map[string]any)
		if got := row["version_count"]; got != want[row["package_id"].(string)] {
			t.Fatalf("%v version_count = %v, want %v (zero-filled when absent)", row["package_id"], got, want[row["package_id"].(string)])
		}
	}
}
