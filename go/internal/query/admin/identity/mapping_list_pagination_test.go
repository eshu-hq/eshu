// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
