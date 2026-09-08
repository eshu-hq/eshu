// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// These route pins live in package repository (not the root
// repository_language_inventory_test.go) because the route-coverage gate
// requires a matching Test function in the handler's own directory. The
// root file keeps the deep ContentReader-backed coverage; these pin the
// moved routes' wiring: required params, status codes, and the admin-page
// shape against the promoted querytestutil fakes.

func languageInventoryAdminRequest(t *testing.T, target string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	return req.WithContext(queryauth.ContextWithAuthContext(
		req.Context(), queryauth.AuthContext{AllScopes: true},
	))
}

func TestListRepositoriesByLanguageRequiresLanguage(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/by-language", nil)
	w := httptest.NewRecorder()
	handler.ListRepositoriesByLanguage(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestListRepositoriesByLanguageRendersAdminPage(t *testing.T) {
	t.Parallel()

	family := repositoryLanguageFamily("go")
	handler := &RepositoryHandler{
		Content: querytestutil.FakePortContentStore{
			LanguageCounts: map[string]querycontract.RepositoryLanguageAggregate{
				strings.Join(family, ","): {RepositoryCount: 2, FileCount: 42},
			},
			LanguageRepos: []querycontract.RepositoryLanguageRepository{
				{
					Repository: querytestutil.RepositoryStatsCatalogEntry(),
					Languages:  []querycontract.RepositoryLanguageCount{{Language: "go", FileCount: 42}},
					FileCount:  42,
				},
			},
		},
	}
	req := languageInventoryAdminRequest(t, "/api/v0/repositories/by-language?language=go")
	w := httptest.NewRecorder()
	handler.ListRepositoriesByLanguage(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	resp := querytestutil.DecodeResponseBody(t, w)
	if got, want := resp["language"], "go"; got != want {
		t.Fatalf("language = %#v, want %#v", got, want)
	}
	if got, want := resp["repository_count"], float64(2); got != want {
		t.Fatalf("repository_count = %#v, want %#v", got, want)
	}
	if got, want := resp["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	rows, ok := resp["repositories"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("repositories = %#v, want 1 row", resp["repositories"])
	}
}

func TestGetRepositoryLanguageInventoryRendersAdminRows(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: querytestutil.FakePortContentStore{
			LanguageInventory: []querycontract.RepositoryLanguageInventoryRow{
				{Language: "go", RepositoryCount: 2, FileCount: 42},
			},
		},
	}
	req := languageInventoryAdminRequest(t, "/api/v0/repositories/language-inventory")
	w := httptest.NewRecorder()
	handler.GetRepositoryLanguageInventory(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	resp := querytestutil.DecodeResponseBody(t, w)
	rows, ok := resp["languages"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("languages = %#v, want 1 row", resp["languages"])
	}
}
