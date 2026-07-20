// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strings"
	"time"
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
// AllowedScopeIDs (repository_language_inventory.go passes the grant through
// to ContentStore; see its interface doc comment in ports.go).
func (h *RepositoryHandler) listRepositoriesByLanguage(w http.ResponseWriter, r *http.Request) {
	language := strings.ToLower(strings.TrimSpace(QueryParam(r, "language")))
	if language == "" {
		WriteError(w, http.StatusBadRequest, "language is required")
		return
	}
	if h == nil || h.Content == nil {
		WriteError(w, http.StatusServiceUnavailable, "repository language content store is unavailable")
		return
	}

	access := repositoryAccessFilterFromContext(r.Context())
	languages := repositoryLanguageFamily(language)
	page := repositoryLanguagePageFromRequest(r, true)

	if access.empty() {
		h.writeEmptyRepositoryLanguagePage(w, r, language, languages, page)
		return
	}

	allScopes := !access.scoped()
	allowedRepositoryIDs := access.grantedRepositoryIDs()
	allowedScopeIDs := access.grantedScopeIDs()

	aggregate, err := h.Content.CountRepositoriesByLanguage(r.Context(), languages, allScopes, allowedRepositoryIDs, allowedScopeIDs)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("count repositories by language: %v", err))
		return
	}

	rows := []RepositoryLanguageRepository{}
	truncated := false
	if page.Limit > 0 {
		rows, err = h.Content.ListRepositoriesByLanguage(
			r.Context(), languages, page.Limit+1, page.Offset, allScopes, allowedRepositoryIDs, allowedScopeIDs,
		)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list repositories by language: %v", err))
			return
		}
		truncated = len(rows) > page.Limit
		if truncated {
			rows = rows[:page.Limit]
		}
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"language":             language,
		"normalized_languages": languages,
		"repository_count":     aggregate.RepositoryCount,
		"file_count":           aggregate.FileCount,
		"last_indexed_at":      formatCoverageTimestamp(aggregate.LastIndexedAt),
		"repositories":         repositoryLanguageRepositoryMaps(rows),
		"limit":                page.Limit,
		"offset":               page.Offset,
		"truncated":            truncated,
	}, BuildTruthEnvelope(
		h.profile(),
		"platform_impact.catalog",
		TruthBasisContentIndex,
		"resolved from indexed repository language coverage",
	))
}

// getRepositoryLanguageInventory serves GET /api/v0/repositories/language-inventory.
// Access scoping mirrors listRepositoriesByLanguage: content_files carries no
// grant of its own, so a scoped caller with no grants never reaches the store
// (#5167 Group B, #5137 LiveActivityStore precedent), and a granted scoped
// caller only sees per-language counts intersected with its grant.
func (h *RepositoryHandler) getRepositoryLanguageInventory(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Content == nil {
		WriteError(w, http.StatusServiceUnavailable, "repository language content store is unavailable")
		return
	}

	access := repositoryAccessFilterFromContext(r.Context())
	page := repositoryLanguagePageFromRequest(r, false)

	if access.empty() {
		h.writeEmptyRepositoryLanguageInventoryPage(w, r, page)
		return
	}

	rows, err := h.Content.RepositoryLanguageInventory(
		r.Context(), page.Limit+1, page.Offset, !access.scoped(), access.grantedRepositoryIDs(), access.grantedScopeIDs(),
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("repository language inventory: %v", err))
		return
	}
	truncated := len(rows) > page.Limit
	if truncated {
		rows = rows[:page.Limit]
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"languages": repositoryLanguageInventoryMaps(rows),
		"limit":     page.Limit,
		"offset":    page.Offset,
		"truncated": truncated,
	}, BuildTruthEnvelope(
		h.profile(),
		"platform_impact.catalog",
		TruthBasisContentIndex,
		"resolved from indexed repository language inventory",
	))
}

// writeEmptyRepositoryLanguagePage returns the bounded empty by-language page
// for a scoped caller with no granted repository or ingestion scope, without
// querying Postgres (#5167 Group B, #5137 LiveActivityStore precedent).
func (h *RepositoryHandler) writeEmptyRepositoryLanguagePage(
	w http.ResponseWriter,
	r *http.Request,
	language string,
	languages []string,
	page repositoryListPage,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"language":             language,
		"normalized_languages": languages,
		"repository_count":     0,
		"file_count":           0,
		"last_indexed_at":      "",
		"repositories":         []map[string]any{},
		"limit":                page.Limit,
		"offset":               page.Offset,
		"truncated":            false,
	}, BuildTruthEnvelope(
		h.profile(),
		"platform_impact.catalog",
		TruthBasisContentIndex,
		"scoped token grants authorize no repositories; repository language coverage is empty",
	))
}

// writeEmptyRepositoryLanguageInventoryPage is the language-inventory
// counterpart of writeEmptyRepositoryLanguagePage.
func (h *RepositoryHandler) writeEmptyRepositoryLanguageInventoryPage(
	w http.ResponseWriter,
	r *http.Request,
	page repositoryListPage,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"languages": []map[string]any{},
		"limit":     page.Limit,
		"offset":    page.Offset,
		"truncated": false,
	}, BuildTruthEnvelope(
		h.profile(),
		"platform_impact.catalog",
		TruthBasisContentIndex,
		"scoped token grants authorize no repositories; repository language inventory is empty",
	))
}

func repositoryLanguagePageFromRequest(r *http.Request, allowZeroLimit bool) repositoryListPage {
	limit := QueryParamInt(r, "limit", repositoryLanguageDefaultLimit)
	if limit < 0 {
		limit = repositoryLanguageDefaultLimit
	}
	if limit == 0 && !allowZeroLimit {
		limit = repositoryLanguageDefaultLimit
	}
	if limit > repositoryLanguageMaxLimit {
		limit = repositoryLanguageMaxLimit
	}
	offset := QueryParamInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	return repositoryListPage{Limit: limit, Offset: offset}
}

func repositoryLanguageFamily(language string) []string {
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

func repositoryLanguageRepositoryMaps(rows []RepositoryLanguageRepository) []map[string]any {
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

func repositoryLanguageInventoryMaps(rows []RepositoryLanguageInventoryRow) []map[string]any {
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

func maxTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}
