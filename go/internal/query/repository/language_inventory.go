// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository/readmodel"
)

const (
	repositoryLanguageDefaultLimit = 100
	repositoryLanguageMaxLimit     = 500
)

// listRepositoriesByLanguage serves GET /api/v0/repositories/by-language.
//
// Access scoping (#5167 Group B): content_files aggregates over the whole
// corpus with no grant of its own, so a scoped caller with no granted
// repository or ingestion scope never reaches the store at all -- the
// response renders as a bounded empty page without a query, mirroring the
// #5137 LiveActivityStore precedent. A scoped caller WITH grants only ever
// sees repositories/counts intersected with AllowedRepositoryIDs/
// AllowedScopeIDs (language_inventory.go passes the grant through
// to querycontract.ContentStore; see its interface doc comment in ports.go).
// ListRepositoriesByLanguage lists repositories by language. It forwards to
// listRepositoriesByLanguage; exported for #6060 so root tests in package
// query can name it from outside this package.
func (h *RepositoryHandler) ListRepositoriesByLanguage(w http.ResponseWriter, r *http.Request) {
	h.listRepositoriesByLanguage(w, r)
}

func (h *RepositoryHandler) listRepositoriesByLanguage(w http.ResponseWriter, r *http.Request) {
	language := strings.ToLower(strings.TrimSpace(querycontract.QueryParam(r, "language")))
	if language == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "language is required")
		return
	}
	if h == nil || h.Content == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "repository language content store is unavailable")
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	languages := repositoryLanguageFamily(language)
	page := repositoryLanguagePageFromRequest(r, true)

	if access.Empty() {
		h.writeEmptyRepositoryLanguagePage(w, r, language, languages, page)
		return
	}

	allScopes := !access.Scoped()
	allowedRepositoryIDs := access.GrantedRepositoryIDs()
	allowedScopeIDs := access.GrantedScopeIDs()

	aggregate, err := h.Content.CountRepositoriesByLanguage(r.Context(), languages, allScopes, allowedRepositoryIDs, allowedScopeIDs)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("count repositories by language: %v", err))
		return
	}

	rows := []querycontract.RepositoryLanguageRepository{}
	truncated := false
	if page.Limit > 0 {
		rows, err = h.Content.ListRepositoriesByLanguage(
			r.Context(), languages, page.Limit+1, page.Offset, allScopes, allowedRepositoryIDs, allowedScopeIDs,
		)
		if err != nil {
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list repositories by language: %v", err))
			return
		}
		truncated = len(rows) > page.Limit
		if truncated {
			rows = rows[:page.Limit]
		}
	}

	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"language":             language,
		"normalized_languages": languages,
		"repository_count":     aggregate.RepositoryCount,
		"file_count":           aggregate.FileCount,
		"last_indexed_at":      formatCoverageTimestamp(aggregate.LastIndexedAt),
		"repositories":         repositoryLanguageRepositoryMaps(rows),
		"limit":                page.Limit,
		"offset":               page.Offset,
		"truncated":            truncated,
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		"platform_impact.catalog",
		querycontract.TruthBasisContentIndex,
		"resolved from indexed repository language coverage",
	))
}

// getRepositoryLanguageInventory serves GET /api/v0/repositories/language-inventory.
// Access scoping mirrors listRepositoriesByLanguage: content_files carries no
// grant of its own, so a scoped caller with no grants never reaches the store
// (#5167 Group B, #5137 LiveActivityStore precedent), and a granted scoped
// caller only sees per-language counts intersected with its grant.
// GetRepositoryLanguageInventory serves the repository language inventory.
// It forwards to getRepositoryLanguageInventory; exported for #6060 so root
// tests in package query can name it from outside this package.
func (h *RepositoryHandler) GetRepositoryLanguageInventory(w http.ResponseWriter, r *http.Request) {
	h.getRepositoryLanguageInventory(w, r)
}

func (h *RepositoryHandler) getRepositoryLanguageInventory(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Content == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "repository language content store is unavailable")
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	page := repositoryLanguagePageFromRequest(r, false)

	if access.Empty() {
		h.writeEmptyRepositoryLanguageInventoryPage(w, r, page)
		return
	}

	rows, err := h.Content.RepositoryLanguageInventory(
		r.Context(), page.Limit+1, page.Offset, !access.Scoped(), access.GrantedRepositoryIDs(), access.GrantedScopeIDs(),
	)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("repository language inventory: %v", err))
		return
	}
	truncated := len(rows) > page.Limit
	if truncated {
		rows = rows[:page.Limit]
	}

	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"languages": repositoryLanguageInventoryMaps(rows),
		"limit":     page.Limit,
		"offset":    page.Offset,
		"truncated": truncated,
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		"platform_impact.catalog",
		querycontract.TruthBasisContentIndex,
		"resolved from indexed repository language inventory",
	))
}

// writeEmptyRepositoryLanguagePage returns the bounded empty by-language page
// for a scoped caller with no granted repository or ingestion scope, without
// querying Postgres (#5167 Group B, #5137 LiveActivityStore precedent). The
// page reads nothing, so it reports TruthBasisNoBackendRead rather than the
// content_index it claimed before #6544; no content store was consulted to
// produce it.
func (h *RepositoryHandler) writeEmptyRepositoryLanguagePage(
	w http.ResponseWriter,
	r *http.Request,
	language string,
	languages []string,
	page readmodel.ListPage,
) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"language":             language,
		"normalized_languages": languages,
		"repository_count":     0,
		"file_count":           0,
		"last_indexed_at":      "",
		"repositories":         []map[string]any{},
		"limit":                page.Limit,
		"offset":               page.Offset,
		"truncated":            false,
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		"platform_impact.catalog",
		querycontract.TruthBasisNoBackendRead,
		"scoped token grants authorize no repositories; repository language coverage is empty",
	))
}

// writeEmptyRepositoryLanguageInventoryPage is the language-inventory
// counterpart of writeEmptyRepositoryLanguagePage.
func (h *RepositoryHandler) writeEmptyRepositoryLanguageInventoryPage(
	w http.ResponseWriter,
	r *http.Request,
	page readmodel.ListPage,
) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"languages": []map[string]any{},
		"limit":     page.Limit,
		"offset":    page.Offset,
		"truncated": false,
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		"platform_impact.catalog",
		querycontract.TruthBasisNoBackendRead,
		"scoped token grants authorize no repositories; repository language inventory is empty",
	))
}

func repositoryLanguagePageFromRequest(r *http.Request, allowZeroLimit bool) readmodel.ListPage {
	limit := querycontract.QueryParamInt(r, "limit", repositoryLanguageDefaultLimit)
	if limit < 0 {
		limit = repositoryLanguageDefaultLimit
	}
	if limit == 0 && !allowZeroLimit {
		limit = repositoryLanguageDefaultLimit
	}
	if limit > repositoryLanguageMaxLimit {
		limit = repositoryLanguageMaxLimit
	}
	offset := querycontract.QueryParamInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	return readmodel.ListPage{Limit: limit, Offset: offset}
}

func RepositoryLanguageFamily(language string) []string {
	normalized := strings.ToLower(strings.TrimSpace(language))
	switch normalized {
	case "ts", "typescript":
		return []string{"typescript", "tsx"}
	case "js", "javascript":
		return []string{"javascript", "jsx"}
	case "terraform":
		return []string{"terraform", "hcl", "tfvars"}
	default:
		// A blank or whitespace-only selector means "no language filter". Test the
		// trimmed value so "?language=%20" behaves like an absent param rather than
		// matching files with an empty language.
		if normalized == "" {
			return nil
		}
		return []string{normalized}
	}
}

func repositoryLanguageRepositoryMaps(rows []querycontract.RepositoryLanguageRepository) []map[string]any {
	mapped := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		repo := repositoryCatalogMap(row.Repository)
		repo["file_count"] = row.FileCount
		repo["languages"] = coverageLanguageMaps(row.Languages)
		repo["last_indexed_at"] = formatCoverageTimestamp(row.IndexedAt)
		mapped = append(mapped, repo)
	}
	return mapped
}

func repositoryLanguageInventoryMaps(rows []querycontract.RepositoryLanguageInventoryRow) []map[string]any {
	mapped := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		mapped = append(mapped, map[string]any{
			"language":         row.Language,
			"repository_count": row.RepositoryCount,
			"file_count":       row.FileCount,
			"last_indexed_at":  formatCoverageTimestamp(row.LastIndexedAt),
		})
	}
	return mapped
}

// repositoryLanguageFamily keeps the in-package spelling after the #6060
// export; root tests name RepositoryLanguageFamily.
func repositoryLanguageFamily(language string) []string {
	return RepositoryLanguageFamily(language)
}
