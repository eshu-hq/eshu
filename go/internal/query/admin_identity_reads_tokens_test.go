// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAdminListAPITokensIncludesDisplayLabel verifies the tenant admin token
// list surfaces each token's real display_label (issue #3708) alongside owner
// attribution, so an admin can tell which token is which without guessing
// from token_id alone.
//
// Split out of admin_identity_reads_test.go to keep that file under the
// repo's 500-line cap (issue #5164); the fakeAdminIdentityReadStore,
// adminRequest, and allScopeAdminAuth helpers it uses are defined there and
// shared across the package.
func TestAdminListAPITokensIncludesDisplayLabel(t *testing.T) {
	t.Parallel()

	store := &fakeAdminIdentityReadStore{
		apiTokens: map[string][]AdminAPITokenListItem{
			"tenant_a": {
				{
					TokenID:      "tok_1",
					TokenClass:   "personal",
					UserID:       "user_1",
					Status:       "active",
					DisplayLabel: "owner laptop",
					TenantID:     "tenant_a",
					WorkspaceID:  "workspace_a",
				},
			},
		},
	}
	handler := &AdminIdentityReadHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, "/api/v0/auth/admin/api-tokens", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tokens, _ := resp["tokens"].([]any)
	if len(tokens) != 1 {
		t.Fatalf("tokens len = %d, want 1: %s", len(tokens), rec.Body.String())
	}
	token, _ := tokens[0].(map[string]any)
	if got, want := token["display_label"], "owner laptop"; got != want {
		t.Fatalf("token display_label = %v, want %q", got, want)
	}
	if got, want := token["user_id"], "user_1"; got != want {
		t.Fatalf("token user_id = %v, want %q", got, want)
	}
}
