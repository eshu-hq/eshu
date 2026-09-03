// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestSemanticSearchHandlerGeneratedTokenRequiresAskSearchFeature(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	handler := &SemanticSearchHandler{Index: index, Profile: querycontract.ProfileProduction}
	req := querytestutil.SemanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                         queryauth.AuthModeScoped,
		TenantID:                     "tenant-a",
		WorkspaceID:                  "workspace-a",
		PermissionCatalogEnforced:    true,
		AllowedRepositoryIDs:         []string{"repo-payments"},
		AllowedPermissionFeatures:    []string{"repository_content"},
		AllowedPermissionDataClasses: []string{"source"},
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if index.calls != 0 {
		t.Fatalf("index calls = %d, want 0 before feature authorization", index.calls)
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Error == nil || envelope.Error.Code != querycontract.ErrorCodePermissionDenied {
		t.Fatalf("error = %#v, want permission_denied", envelope.Error)
	}
}

func TestSemanticSearchHandlerGeneratedTokenRequiresAskSearchDataClasses(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	handler := &SemanticSearchHandler{Index: index, Profile: querycontract.ProfileProduction}
	req := querytestutil.SemanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                         queryauth.AuthModeScoped,
		TenantID:                     "tenant-a",
		WorkspaceID:                  "workspace-a",
		PermissionCatalogEnforced:    true,
		AllowedRepositoryIDs:         []string{"repo-payments"},
		AllowedPermissionFeatures:    []string{"ask_search"},
		AllowedPermissionDataClasses: []string{"ask_reasoning"},
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if index.calls != 0 {
		t.Fatalf("index calls = %d, want 0 before data-class authorization", index.calls)
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Error == nil || envelope.Error.Code != querycontract.ErrorCodePermissionDenied {
		t.Fatalf("error = %#v, want permission_denied", envelope.Error)
	}
}
