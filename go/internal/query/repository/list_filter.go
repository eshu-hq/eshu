// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository/readmodel"
)

// Name filter and compact projection for the repository list route (#10603).
// The handler keeps the route wiring and the COUNT/page queries; this file
// holds the filter parsing, predicate composition, and compact shaping so the
// handler stays near its size cap.

// repositoryListMaxNameLength bounds the list name filter input: a longer
// value answers 400 instead of scanning unbounded text.
const repositoryListMaxNameLength = 200

// Repository list row projections. The HTTP route defaults to full (today's
// bytes); the MCP route builders default to compact (the discovery set).
// The handler default is full; unknown values answer 400.
const (
	repositoryListProjectionFull    = "full"
	repositoryListProjectionCompact = "compact"
)

// parseRepositoryListFilter reads the optional name substring filter and the
// row projection off r. Empty or whitespace-only input means absent for both
// the filter (no filtering) and, on the HTTP route, the projection (full).
// An over-long name or an unknown projection answers 400 and reports false.
func parseRepositoryListFilter(w http.ResponseWriter, r *http.Request) (name, projection string, ok bool) {
	name = querycontract.QueryParam(r, "name")
	if len(name) > repositoryListMaxNameLength {
		querycontract.WriteError(w, http.StatusBadRequest, repositoryListNameFailedMessage)
		return "", "", false
	}
	switch projection = querycontract.QueryParam(r, "projection"); projection {
	case "":
		return name, repositoryListProjectionFull, true
	case repositoryListProjectionFull, repositoryListProjectionCompact:
		return name, projection, true
	default:
		querycontract.WriteError(w, http.StatusBadRequest, repositoryListProjectionFailedMessage)
		return "", "", false
	}
}

// repositoryListNameCondition is the Cypher substring predicate matching the
// pre-lowercased $name_contains parameter against the repository name or
// repo slug.
const repositoryListNameCondition = `(toLower(r.name) CONTAINS $name_contains OR toLower(coalesce(r.repo_slug, '')) CONTAINS $name_contains)`

// repositoryListWhereClause composes the caller's access filter with the
// optional name substring predicate so both bind before the COUNT and the
// page query. An empty filter keeps today's clause byte-for-byte.
func repositoryListWhereClause(access querycontract.RepositoryAccessFilter, name string) string {
	where := access.GraphWhereClause("r")
	if name == "" {
		return where
	}
	if where == "" {
		return "WHERE " + repositoryListNameCondition
	}
	return where + " AND " + repositoryListNameCondition
}

// repositoryListGraphParams binds the page window, the caller's grant arrays,
// and the pre-lowercased name substring when a filter is present.
func repositoryListGraphParams(access querycontract.RepositoryAccessFilter, page readmodel.ListPage, name string) map[string]any {
	params := access.GraphParams(map[string]any{"offset": page.Offset, "limit": page.Limit + 1})
	if name != "" {
		params["name_contains"] = strings.ToLower(name)
	}
	return params
}

// filterRepositoryMapsByName keeps the rows whose name or repo_slug contains
// the substring, case-insensitively. It composes with (never replaces) the
// access filter the caller applies separately. An empty filter is a
// passthrough.
func filterRepositoryMapsByName(repos []map[string]any, name string) []map[string]any {
	if name == "" {
		return repos
	}
	want := strings.ToLower(name)
	kept := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		if strings.Contains(strings.ToLower(querycontract.StringVal(repo, "name")), want) ||
			strings.Contains(strings.ToLower(querycontract.StringVal(repo, "repo_slug")), want) {
			kept = append(kept, repo)
		}
	}
	return kept
}

// projectCompactRepositoryRow reduces an annotated row to the discovery set:
// identity plus index_state. Everything else is drilldown data served by the
// per-repository tools (get_repository_stats names the drilldown).
func projectCompactRepositoryRow(repo map[string]any) map[string]any {
	return map[string]any{
		"id":          repo["id"],
		"name":        repo["name"],
		"repo_slug":   repo["repo_slug"],
		"index_state": repo["index_state"],
	}
}

// annotateListPageCompact annotates one repository list page for the compact
// projection: retirement only (index_state derives there), without the
// selection aggregate. The retirement detail block and the
// derivation-only evidence source are dropped; the caller projects each row
// to the discovery set. Entries never serve with evidence silently missing:
// a failing tombstone read stops the page with its stage, as in full rows.
func annotateListPageCompact(
	ctx context.Context,
	retirements PageRetirementReader,
	repos []map[string]any,
) ([]map[string]any, listAnnotationStage, error) {
	annotated, err := annotateListRepositoriesWithRetirement(ctx, retirements, repos)
	if err != nil {
		return nil, listAnnotationRetirement, err
	}
	compact := make([]map[string]any, 0, len(annotated))
	for _, repo := range annotated {
		delete(repo, "evidence_source")
		delete(repo, "retirement")
		compact = append(compact, projectCompactRepositoryRow(repo))
	}
	return compact, 0, nil
}

// repositoryInventoryResponseWithProjection wraps the bounded repository list
// page and echoes the served projection so callers can tell a compact
// discovery page from a full one. The projection key is additive: rows and
// every other envelope field keep today's shape under full.
func repositoryInventoryResponseWithProjection(repos []map[string]any, page readmodel.ListPage, truncated bool, total int, projection string, extraPartialReasons ...string) map[string]any {
	response := repositoryInventoryResponse(repos, page, truncated, total, extraPartialReasons...)
	response["projection"] = projection
	return response
}

// repositoryCountCypher is the bounded COUNT query used to populate the total
// field on the repository list response independently of the page size. It runs
// in a few milliseconds on production-scale graphs because it uses the same
// Repository node scan that the backing store optimises for label counts.
const repositoryCountCypher = `MATCH (r:Repository) RETURN count(r) AS total`

// isRepositoryCountCypher reports whether a Cypher string is the bounded count
// query emitted by queryRepositoryTotal. Test fakes use this to dispatch the
// count response separately from the page response.
func isRepositoryCountCypher(cypher string) bool {
	return cypher == repositoryCountCypher
}

// queryRepositoryTotal runs a bounded COUNT query and returns the true number
// of Repository nodes visible to the caller. A failed or malformed count is an
// error rather than an exact zero because zero is a valid, materially different
// inventory result. With a name filter present the COUNT carries the same
// predicate as the page query, so total stays exact for the filtered set.
func queryRepositoryTotal(ctx context.Context, graph querycontract.GraphQuery, access querycontract.RepositoryAccessFilter, name string) (int, error) {
	var cypher string
	var params map[string]any
	if where := repositoryListWhereClause(access, name); where != "" {
		cypher = fmt.Sprintf(
			`MATCH (r:Repository) %s RETURN count(r) AS total`,
			where,
		)
		params = access.GraphParams(nil)
		if name != "" {
			params["name_contains"] = strings.ToLower(name)
		}
	} else {
		cypher = repositoryCountCypher
	}
	row, err := graph.RunSingle(ctx, cypher, params)
	if err != nil {
		return 0, fmt.Errorf("repository total: %w", err)
	}
	if row == nil {
		return 0, fmt.Errorf("repository total: count query returned no row")
	}
	raw, ok := row["total"]
	if !ok || raw == nil {
		return 0, fmt.Errorf("repository total: count query omitted total")
	}
	var total int
	switch value := raw.(type) {
	case int:
		total = value
	case int64:
		if value > int64(math.MaxInt) {
			return 0, fmt.Errorf("repository total: count query total exceeds platform integer range")
		}
		total = int(value)
	case float64:
		if value != math.Trunc(value) || value > float64(math.MaxInt) {
			return 0, fmt.Errorf("repository total: count query returned non-integer total")
		}
		total = int(value)
	default:
		return 0, fmt.Errorf("repository total: count query returned unsupported total type %T", raw)
	}
	if total < 0 {
		return 0, fmt.Errorf("repository total: count query returned negative total")
	}
	return total, nil
}
