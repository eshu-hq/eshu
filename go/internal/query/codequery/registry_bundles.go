// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/package/registry"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	searchBundlesDefaultLimit = 50
	searchBundlesMaxLimit     = 200
)

// searchBundlesCapability names the registry bundle search surface on both the
// success truth envelope and the error envelope, so envelope-accepting callers
// (the MCP dispatch path sends Accept: application/eshu.envelope+json) get a
// capability-tagged structured result for failures, not a bare error.
const searchBundlesCapability = "platform_impact.context_overview"

// writeSearchBundlesError emits a canonical ResponseEnvelope error when the
// caller accepts the envelope MIME type and a plain error otherwise. #3506:
// the MCP dispatch path recognizes only canonical envelopes; a non-envelope
// 400 there becomes a transport error instead of a structured IsError tool
// result, so bundle validation failures must ride the envelope.
func writeSearchBundlesError(w http.ResponseWriter, r *http.Request, status int, code ErrorCode, message string) {
	if querycontract.AcceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{
			Data: nil,
			Error: &ErrorEnvelope{
				Code:       code,
				Message:    message,
				Capability: searchBundlesCapability,
			},
		})
		return
	}
	WriteError(w, status, message)
}

// handleSearchBundles searches the pre-indexed package registry catalog as
// shareable bundle candidates.
//
// #3493: this handler previously ran `MATCH (r:Repository) WHERE r.name
// CONTAINS $query`, which is a repository-name search wearing a
// registry/SBOM-bundle name. A "registry bundle" is pre-indexed dependency or
// library graph content, and the only such pre-indexed, queryable registry
// catalog in the graph is the package registry (`:Package` /
// `:PackageRegistryPackage` identities materialized by the reducer). The
// handler now searches that catalog by package identity (normalized name,
// namespace, or PURL) and optionally scopes to one ecosystem, aligning with the
// list_package_registry_* read surface instead of inventing repo-name truth.
//
// #3506: a search scope is required. The request must supply a non-empty
// `query` or `ecosystem`; an unscoped request is rejected with 400 before any
// graph read. Without this guard a catalog-head request anchored on
// `MATCH (p:Package)` would scan every package and run the version
// `OPTIONAL MATCH`/`count(v)` aggregation across the whole registry before
// applying `LIMIT`, which violates the bounded read contract on large
// registries. Requiring a scope keeps the read bounded by construction.
//
// #5167: a scoped token is admitted and sees only `visibility = 'public'`
// packages (a package with no visibility stays hidden); an empty grant is an
// empty page with no graph call. The read is two statements, an anchor-only
// ordered page and a page-bound version count, because the pinned NornicDB
// ignores the ORDER BY/LIMIT that follow `WITH p, count(v)` (see
// searchRegistryBundlesCypher).
func (h *CodeHandler) handleSearchBundles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string `json:"query"`
		Ecosystem  string `json:"ecosystem"`
		UniqueOnly bool   `json:"unique_only"`
		Limit      int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		writeSearchBundlesError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}

	query := strings.TrimSpace(req.Query)
	ecosystem := strings.TrimSpace(req.Ecosystem)
	if query == "" && ecosystem == "" {
		writeSearchBundlesError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, "a non-empty query or ecosystem scope is required")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = searchBundlesDefaultLimit
	}
	if limit > searchBundlesMaxLimit {
		limit = searchBundlesMaxLimit
	}

	// #5167: a scoped caller sees only visibility = 'public' packages, the
	// same gate the package-registry ecosystem browse applies. Package nodes
	// carry no repository key a grant could bind to, so an empty grant is a
	// zero-row page with no graph call and every other grant reads the public
	// subset. An unscoped or all-scope caller reads the whole catalog.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		writeSearchBundlesPage(w, r, h, nil, limit, false)
		return
	}
	publicOnly := access.Scoped()

	cypher, params := searchRegistryBundlesCypher(query, ecosystem, req.UniqueOnly, publicOnly, limit+1)

	ctx, cancel := context.WithTimeout(r.Context(), cypherQueryTimeout)
	defer cancel()

	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		if WriteGraphReadError(w, r, err, searchBundlesCapability) {
			return
		}
		writeSearchBundlesError(w, r, http.StatusInternalServerError, ErrorCodeInternalError, err.Error())
		return
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}

	// The anchor statement carries no version count: the pinned NornicDB
	// ignores ORDER BY/LIMIT after `WITH p, count(v)`, so the count is read
	// for the bounded page only, by the registry family's MATCH-only
	// statement, and zero-filled here.
	packageIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		packageIDs = append(packageIDs, StringVal(row, "package_id"))
	}
	counts, err := registry.VersionCountsByPackageID(ctx, h.Neo4j, packageIDs)
	if err != nil {
		if WriteGraphReadError(w, r, err, searchBundlesCapability) {
			return
		}
		writeSearchBundlesError(w, r, http.StatusInternalServerError, ErrorCodeInternalError, err.Error())
		return
	}
	for _, row := range rows {
		row["version_count"] = counts[StringVal(row, "package_id")]
	}

	writeSearchBundlesPage(w, r, h, rows, limit, truncated)
}

// writeSearchBundlesPage writes the bundle-search success envelope. rows may
// be nil (an empty page).
func writeSearchBundlesPage(
	w http.ResponseWriter,
	r *http.Request,
	h *CodeHandler,
	rows []map[string]any,
	limit int,
	truncated bool,
) {
	if rows == nil {
		rows = []map[string]any{}
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"bundles":   rows,
		"count":     len(rows),
		"limit":     limit,
		"truncated": truncated,
	}, BuildTruthEnvelope(h.profile(), searchBundlesCapability, TruthBasisAuthoritativeGraph, "resolved from bounded package registry bundle catalog"))
}

// searchRegistryBundlesCypher builds the bounded, deterministically ordered
// anchor read over the package registry catalog. The match anchors on
// `:Package` identities (which carry the dual `:PackageRegistryPackage` label
// written by the reducer) and filters by case-insensitive substring over the
// package's normalized name, namespace, or PURL. A non-empty ecosystem scopes
// the read to one ecosystem. The caller (handleSearchBundles) requires a
// non-empty query or ecosystem before calling this, so the produced query
// always carries a selective predicate and never scans the whole catalog. The
// query parameter is always bound, never interpolated, so the substring match
// stays injection-safe.
//
// publicOnly adds `p.visibility = 'public'` for a scoped caller, the literal
// the package-registry ecosystem browse
// (packageRegistryPackagesScopedEcosystemCypher) uses: a package whose fact
// carries no visibility is not public, so it stays hidden.
//
// The statement is deliberately anchor-only, with no version count and no
// OPTIONAL MATCH: on the pinned NornicDB, `WITH p, count(v)` makes the
// planner silently drop the trailing ORDER BY and LIMIT (measured: LIMIT 51
// returned all 3000 rows unordered), so version_count is resolved for the
// returned page by registry.VersionCountsByPackageID. ORDER BY and LIMIT sit
// directly on the anchor RETURN, where the pinned build honours them.
func searchRegistryBundlesCypher(query, ecosystem string, uniqueOnly, publicOnly bool, limit int) (string, map[string]any) {
	params := map[string]any{"limit": limit}

	cypher := `MATCH (p:Package) WHERE p.uid IS NOT NULL`
	if ecosystem != "" {
		cypher += ` AND p.ecosystem = $ecosystem`
		params["ecosystem"] = ecosystem
	}
	if publicOnly {
		cypher += ` AND p.visibility = 'public'`
	}
	if query != "" {
		cypher += ` AND (toLower(coalesce(p.normalized_name, '')) CONTAINS toLower($query)` +
			` OR toLower(coalesce(p.namespace, '')) CONTAINS toLower($query)` +
			` OR toLower(coalesce(p.purl, '')) CONTAINS toLower($query))`
		params["query"] = query
	}

	projection := `RETURN`
	if uniqueOnly {
		projection += ` DISTINCT`
	}
	// An ecosystem-pinned read has one ecosystem, so ordering by it changes no
	// row's position; dropping the key leaves the (ecosystem, name, uid)
	// contract intact and halves the sort cost on the pinned NornicDB
	// (measured 115 -> 69 ms at limit 51, 343 -> 178 ms at limit 201 over 3,000
	// packages, uncached).
	orderBy := `p.ecosystem, p.normalized_name, p.uid`
	if ecosystem != "" {
		orderBy = `p.normalized_name, p.uid`
	}
	cypher += `
` + projection + ` p.uid AS package_id,
       p.normalized_name AS name,
       p.ecosystem AS ecosystem,
       p.registry AS registry,
       p.namespace AS namespace,
       p.purl AS purl
ORDER BY ` + orderBy + `
LIMIT $limit`

	return cypher, params
}
