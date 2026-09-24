// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// This file proves the four generated API-token routes end to end through
// Mount + ServeHTTP (#6642 route-coverage; see handler_test.go's file
// comment for why these tests are new rather than moved verbatim).

func TestListAPITokens(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/local/api-tokens", nil)
	req = allScopeContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestListAPITokensRequiresAuthentication(t *testing.T) {
	t.Parallel()

	handler := &IdentityHandler{Store: &fakeStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/local/api-tokens", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestCreateAPIToken(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store, NewSecret: sequenceSecrets("token-1", "raw-generated-token")}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/api-tokens", bytes.NewBufferString(
		`{"token_class":"personal","user_id":"user-1","display_label":"laptop"}`,
	))
	req = allScopeContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if store.createdAPIToken.TokenID != "token-1" {
		t.Fatalf("created token = %#v, want token-1", store.createdAPIToken)
	}
	if store.createdAPIToken.TokenHash != IdentityHash("raw-generated-token") {
		t.Fatalf("token hash = %q, want hash of raw token, never the raw value stored", store.createdAPIToken.TokenHash)
	}
}

func TestRevokeAPIToken(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/api-tokens/token-1/revoke", bytes.NewBufferString(`{}`))
	req = allScopeContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if store.revokedAPIToken.TokenID != "token-1" {
		t.Fatalf("revoked token = %#v, want token-1", store.revokedAPIToken)
	}
}

func TestRevokeAPITokenNotOwnedReturnsNotFound(t *testing.T) {
	t.Parallel()

	store := &fakeStore{revokeAPITokenError: ErrIdentityAPITokenNotFound}
	handler := &IdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/authCtx/local/api-tokens/token-1/revoke", bytes.NewBufferString(`{}`))
	authCtx := auth.AuthContext{Mode: auth.AuthModeBrowserSession, SubjectIDHash: "sha256:non-owner"}
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), authCtx))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestRotateAPIToken(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store, NewSecret: sequenceSecrets("token-2", "new-raw-token")}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/api-tokens/token-1/rotate", bytes.NewBufferString(`{}`))
	req = allScopeContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if store.rotatedAPIToken.OldTokenID != "token-1" || store.rotatedAPIToken.NewTokenID != "token-2" {
		t.Fatalf("rotated token = %#v, want old token-1 replaced by token-2", store.rotatedAPIToken)
	}
}
