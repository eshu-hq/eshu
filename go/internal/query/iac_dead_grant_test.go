// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// #5167 W4 two-tenant grant proof for find_dead_iac. handleDeadIaC resolves
// every repo_id/repo_ids selector through resolveRepositoryScope ->
// resolveRepositorySelectorExactForAccess bound to
// repositoryAccessFilterFromContext (iac.go) before either the
// reducer-materialized IaCReachabilityStore read or the content-derived
// fallback runs. All three tests below are mutation-sensitive: reverting
// resolveRepositoryScope to the unscoped resolveRepositorySelectorExact it
// used before #5167 W4 makes every one of these go red (the 400 becomes a
// 200 that reaches the reachability store with the out-of-grant repo id).
// Split out of iac_dead_test.go to keep that file under the repository's
// file-size cap.
func deadIaCGrantTestReachabilityStore(observedRepoIDs *[]string) fakeIaCReachabilityStore {
	return fakeIaCReachabilityStore{
		observedRepoIDs: observedRepoIDs,
		rows: []IaCReachabilityFindingRow{
			{
				ID:           "terraform:repository:r_tenant_a:modules/orphan-cache",
				Family:       "terraform",
				RepoID:       "repository:r_tenant_a",
				ArtifactPath: "modules/orphan-cache",
				Reachability: "unused",
				Finding:      "candidate_dead_iac",
				Confidence:   0.75,
				Evidence:     []string{"modules/orphan-cache/main.tf: module directory exists"},
			},
		},
	}
}

func TestHandleDeadIaCScopedGrantRejectsOutOfGrantRepository(t *testing.T) {
	t.Parallel()

	var observedRepoIDs []string
	handler := &IaCHandler{
		Profile:      ProfileLocalAuthoritative,
		Content:      fakeIaCDeadContentStore{},
		Reachability: deadIaCGrantTestReachabilityStore(&observedRepoIDs),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{
		"repo_ids": ["repository:r_tenant_b"]
	}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repository:r_tenant_a"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if observedRepoIDs != nil {
		t.Fatalf("reachability store observed repoIDs = %#v, want nil (never reached)", observedRepoIDs)
	}
}

func TestHandleDeadIaCScopedGrantAllowsInGrantRepository(t *testing.T) {
	t.Parallel()

	var observedRepoIDs []string
	handler := &IaCHandler{
		Profile:      ProfileLocalAuthoritative,
		Content:      fakeIaCDeadContentStore{},
		Reachability: deadIaCGrantTestReachabilityStore(&observedRepoIDs),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{
		"repo_ids": ["repository:r_tenant_a"]
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repository:r_tenant_a"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := observedRepoIDs, []string{"repository:r_tenant_a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("reachability repoIDs = %#v, want %#v", got, want)
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got, want := int(data["findings_count"].(float64)), 1; got != want {
		t.Fatalf("findings_count = %d, want %d", got, want)
	}
}

func TestHandleDeadIaCScopedEmptyGrantRejectsAnyRepository(t *testing.T) {
	t.Parallel()

	var observedRepoIDs []string
	handler := &IaCHandler{
		Profile:      ProfileLocalAuthoritative,
		Content:      fakeIaCDeadContentStore{},
		Reachability: deadIaCGrantTestReachabilityStore(&observedRepoIDs),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{
		"repo_ids": ["repository:r_tenant_a"]
	}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if observedRepoIDs != nil {
		t.Fatalf("reachability store observed repoIDs = %#v, want nil (never reached)", observedRepoIDs)
	}
}
