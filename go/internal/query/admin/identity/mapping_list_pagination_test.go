// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListGroupMappingsPagesPastLimit(t *testing.T) {
	t.Parallel()

	items := make([]IdPGroupMappingListItem, identityListLimit+1)
	for i := range items {
		items[i] = IdPGroupMappingListItem{
			MappingRef:  fmt.Sprintf("%064x", i+1),
			TenantID:    "tenant_a",
			WorkspaceID: "workspace_a",
		}
	}
	store := &fakeAdminIdentityReadStore{
		groupMappings: map[string][]IdPGroupMappingListItem{"tenant_a": items},
	}
	handler := &ReadHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	requestPage := func(path string) (int, struct {
		GroupMappings []struct {
			MappingRef string `json:"mapping_ref"`
		} `json:"group_mappings"`
		Truncated    bool   `json:"truncated"`
		NextAfterRef string `json:"next_after_ref"`
	},
	) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, path,
			allScopeAdminAuth("tenant_a", "workspace_a")))
		var body struct {
			GroupMappings []struct {
				MappingRef string `json:"mapping_ref"`
			} `json:"group_mappings"`
			Truncated    bool   `json:"truncated"`
			NextAfterRef string `json:"next_after_ref"`
		}
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode mapping page: %v", err)
			}
		}
		return rec.Code, body
	}
	const path = "/api/v0/auth/admin/idp-group-mappings"
	status, first := requestPage(path)
	if status != http.StatusOK || len(first.GroupMappings) != identityListLimit ||
		!first.Truncated || first.NextAfterRef != items[identityListLimit-1].MappingRef {
		t.Fatalf("first page: status=%d count=%d truncated=%t cursor=%q",
			status, len(first.GroupMappings), first.Truncated, first.NextAfterRef)
	}
	status, second := requestPage(path + "?after_ref=" + first.NextAfterRef)
	if status != http.StatusOK || len(second.GroupMappings) != 1 ||
		second.GroupMappings[0].MappingRef != items[identityListLimit].MappingRef ||
		second.Truncated || second.NextAfterRef != "" {
		t.Fatalf("second page: status=%d body=%+v", status, second)
	}
	status, _ = requestPage(path + "?after_ref=invalid")
	if status != http.StatusBadRequest {
		t.Fatalf("invalid cursor status=%d, want 400", status)
	}
}

// TestListGroupMappingsRejectsNonCanonicalCursors pins the documented cursor
// contract: after_ref is read from the raw query without normalization, so a
// present-but-empty, padded, whitespace-only, repeated, or unescapable value is
// a 400 rather than a silently accepted first page, matching the OpenAPI
// pattern ^[0-9a-f]{64}$. Only an absent after_ref is the first page.
func TestListGroupMappingsRejectsNonCanonicalCursors(t *testing.T) {
	t.Parallel()

	valid := fmt.Sprintf("%064x", 0xabcdef)
	newMux := func() *http.ServeMux {
		// Each parallel subtest owns its store: the fake records the
		// tenant/workspace of every call, so sharing one store across
		// parallel subtests races (reads_test.go got* fields).
		store := &fakeAdminIdentityReadStore{
			groupMappings: map[string][]IdPGroupMappingListItem{"tenant_a": {{
				MappingRef: valid, TenantID: "tenant_a", WorkspaceID: "workspace_a",
			}}},
		}
		handler := &ReadHandler{Store: store}
		mux := http.NewServeMux()
		handler.Mount(mux)
		return mux
	}

	const path = "/api/v0/auth/admin/idp-group-mappings"
	cases := []struct {
		name  string
		query string
		want  int
	}{
		{name: "absent", query: "", want: http.StatusOK},
		{name: "present but empty", query: "?after_ref=", want: http.StatusBadRequest},
		{name: "valid", query: "?after_ref=" + valid, want: http.StatusOK},
		{name: "whitespace only", query: "?after_ref=%20", want: http.StatusBadRequest},
		{name: "padded valid", query: "?after_ref=%20" + valid + "%20", want: http.StatusBadRequest},
		{name: "duplicate", query: "?after_ref=" + valid + "&after_ref=" + valid, want: http.StatusBadRequest},
		{name: "malformed escape", query: "?after_ref=%zz", want: http.StatusBadRequest},
		{name: "uppercase", query: "?after_ref=" + strings.ToUpper(valid), want: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			newMux().ServeHTTP(rec, adminRequest(t, http.MethodGet, path+tc.query,
				allScopeAdminAuth("tenant_a", "workspace_a")))
			if rec.Code != tc.want {
				t.Fatalf("after_ref %q: status=%d, want %d (body %s)", tc.query, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
