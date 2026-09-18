// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestDeleteIdPGroupMappingRejectsLegacyMD5Ref(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{mappingDeleteResult: IdPGroupMappingDeleteResult{Found: true, Deleted: true}}
	audit := &querytestutil.FakeGovernanceAuditAppender{}
	mux := newMutationMux(store, audit)
	legacyRef := strings.Repeat("a", 32)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodDelete,
		"/api/v0/auth/admin/idp-group-mappings/"+legacyRef, "", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusConflict {
		t.Fatalf("legacy ref status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if store.gotMappingDelete.MappingRef != "" {
		t.Fatal("legacy ref reached the store and could be reported as deleted")
	}
	if !strings.Contains(rec.Body.String(), "list") {
		t.Fatalf("legacy ref response did not tell caller to list mappings again: %s", rec.Body.String())
	}
	if !hasAuditReason(audit, "idp_group_mapping_ref_stale") {
		t.Fatal("legacy ref rejection was not audited")
	}
}

func TestDeleteIdPGroupMappingAcceptsSHA256Ref(t *testing.T) {
	t.Parallel()

	store := &fakeAdminMutationStore{mappingDeleteResult: IdPGroupMappingDeleteResult{Found: true, Deleted: true}}
	mux := newMutationMux(store, &querytestutil.FakeGovernanceAuditAppender{})
	newRef := strings.Repeat("b", 64)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mutationRequest(http.MethodDelete,
		"/api/v0/auth/admin/idp-group-mappings/"+newRef, "", allScopeAdminAuth("tenant_a", "workspace_a")))
	if rec.Code != http.StatusOK {
		t.Fatalf("SHA-256 ref status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotMappingDelete.MappingRef != newRef {
		t.Fatalf("store ref = %q, want new ref", store.gotMappingDelete.MappingRef)
	}
}
