// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"log/slog"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// languageQueryGraphRows runs the graph half of one language query and returns
// its rows.
//
// The Directory branch is separated from the other three labels because its
// statement is no longer self-sufficient (#6541). buildDirectoryCypher seeks
// directories by an indexed `repo_id` instead of walking to a Repository, which
// is what makes it fast, and that costs it two things the other builders get
// from the backend: the repository NAME, which no longer appears in the
// statement at all, and a whole-result row bound, which the pinned NornicDB
// build applies once per UNWOUND id rather than once per result. Both are
// finished here, on at most `limit` surviving rows. Every other label keeps the
// single build-and-run it always had.
func (h *Handler) languageQueryGraphRows(
	ctx context.Context,
	language, label, query, repoID string,
	limit int,
	semanticFilterKey string,
	semanticFilterValue string,
	grant languageQueryGrant,
) ([]map[string]any, error) {
	if label == "Directory" {
		return h.directoryRowsByLanguage(ctx, language, query, repoID, limit, grant)
	}
	cypher, params := BuildCypherWithSemanticFilter(
		language,
		label,
		query,
		repoID,
		limit,
		semanticFilterKey,
		semanticFilterValue,
		grant.Access,
		nil,
	)
	return h.Neo4j.Run(ctx, cypher, params)
}

// directoryRowsByLanguage serves the `entity_type: "directory"` branch.
//
// The order of the three steps is the contract. The statement is bounded by the
// caller's resolved repository-id list, so it can never read outside the grant;
// the re-sort and truncate then cut the answer to `limit` rows; and only those
// surviving rows' repositories are named. A caller granted fifty repositories
// therefore pays one name lookup per repository actually present in its page,
// not one per granted repository.
//
// An empty id list short-circuits without touching the backend. That is the
// right answer for a grant that allows nothing, and for an unscoped caller on a
// graph that holds no repositories; neither case can be told apart from the
// other by the rows, and neither is an error.
func (h *Handler) directoryRowsByLanguage(
	ctx context.Context,
	language, query, repoID string,
	limit int,
	grant languageQueryGrant,
) ([]map[string]any, error) {
	repoIDs, everyRepository := directoryRepositoryIDsForGrant(grant.Access, repoID)
	if everyRepository {
		resolved, err := h.allRepositoryIDs(ctx)
		if err != nil {
			return nil, err
		}
		repoIDs = resolved
	}
	if len(repoIDs) == 0 {
		return nil, nil
	}

	cypher, params := buildDirectoryCypher(language, query, repoIDs, map[string]any{"limit": limit})
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	returned := len(rows)
	rows = sortAndTruncateDirectoryRows(rows, limit)

	names, err := h.directoryRepositoryNames(ctx, directoryRowRepositoryIDs(rows))
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if name, ok := names[querycontract.StringVal(row, "repo_id")]; ok {
			row["repo_name"] = name
		}
	}

	h.logDirectoryRead(ctx, len(repoIDs), returned, len(rows), len(names))
	return rows, nil
}

// logDirectoryRead records what the directory read actually cost, so an
// operator can tell the two shapes of a slow page apart at 3am: a wide grant
// (repositories) versus a backend that returned far more rows than the page
// needed (returned against kept, which is how the per-UNWOUND-id row bound
// shows up in production). named is the number of repositories the second read
// resolved, so a page whose repo_name is missing can be traced to that read
// rather than to the statement.
func (h *Handler) logDirectoryRead(ctx context.Context, repositories, returned, kept, named int) {
	if h == nil || h.Logger == nil {
		return
	}
	h.Logger.DebugContext(ctx, "language query directory read",
		slog.Int("repositories", repositories),
		slog.Int("rows_returned", returned),
		slog.Int("rows_kept", kept),
		slog.Int("repositories_named", named),
	)
}

// directoryRepositoryIDsForGrant resolves which repositories the Directory
// statement may read, and reports whether the caller is entitled to every
// repository in the graph.
//
// It reproduces exactly what the replaced statement's grant predicate decided.
// That predicate was `r.id IN $allowed_repository_ids OR r.id IN
// $allowed_scope_ids`, and RepositorySearchIDs is the sorted, deduplicated
// union of both lists, so the same repositories are admitted. Deduplication is
// not cosmetic here: UNWIND visits a repeated id twice, which on both pinned
// builds returns every one of that repository's directories twice (measured),
// and on a backend aggregating the whole result at once would double their
// file_counts instead.
//
// A scope id that names no repository is inert -- no Directory carries it as
// repo_id -- exactly as it was inert in the predicate.
//
// The repository-anchored case is checked against the grant again even though
// the route resolved the selector through it already, for the same
// defence-in-depth reason codeContentGrantScope states: a caller that reached
// this read on a selector-free path must not widen its own grant. A disallowed
// repository yields an empty list, which is zero rows.
func directoryRepositoryIDsForGrant(access querycontract.RepositoryAccessFilter, repoID string) (repoIDs []string, everyRepository bool) {
	if repoID != "" {
		if access.Scoped() && !access.AllowsRepositoryID(repoID) {
			return nil, false
		}
		return []string{repoID}, false
	}
	if access.Scoped() {
		return access.RepositorySearchIDs(), false
	}
	return nil, true
}

// allRepositoryIDs reads every repository id in the graph, for an unscoped
// admin caller that named no repository.
//
// The Directory statement needs an explicit id list to seek on, and an unscoped
// caller supplies no grant to derive one from. The alternative was a
// Directory-label anchor, and it was rejected on measurement: expanding
// `(d:Directory)-[:CONTAINS]->(f:File)` across the issue's 50-repository corpus
// cost 8.658s for 200,000 rows, and a whole-label scan is what the query-plan
// gate exists to reject.
//
// The ids come from the graph rather than from the Postgres repository
// catalogue deliberately. The catalogue is keyed on ingestion_scopes, so a
// repository present in the graph but absent there would silently drop out of
// an admin's answer; reading the same store the rest of the statement reads
// cannot disagree with itself.
func (h *Handler) allRepositoryIDs(ctx context.Context) ([]string, error) {
	rows, err := h.Neo4j.Run(ctx, `
		MATCH (r:Repository)
		RETURN r.id as repo_id
		ORDER BY repo_id
	`, nil)
	if err != nil {
		return nil, err
	}
	return dedupedRepositoryIDs(rows), nil
}

// directoryRepositoryNames resolves repo_id to repo_name for the repositories
// on one page of directory rows.
//
// The UNWIND variable is `rid` and the returned columns are `repo_id` and
// `repo_name`, and those names must stay distinct from each other. On both the
// v1.2.1 pin and v1.3.1, `UNWIND $repo_ids AS id ... RETURN r.id AS id` comes
// back with that column named after the first bound literal instead of `id`;
// giving the loop variable and the projection different names avoids it. See
// docs/public/reference/nornicdb-path-predicate-pitfalls.md.
//
// The read is keyed on at most `limit` ids -- the page is already truncated
// when this runs -- so it is a bounded seek per repository, measured at 135ms
// for a single repository on the issue's corpus.
func (h *Handler) directoryRepositoryNames(ctx context.Context, repoIDs []string) (map[string]string, error) {
	if len(repoIDs) == 0 {
		return nil, nil
	}
	rows, err := h.Neo4j.Run(ctx, `
		UNWIND $repo_ids AS rid
		MATCH (r:Repository {id: rid})
		RETURN r.id as repo_id, r.name as repo_name
	`, map[string]any{"repo_ids": repoIDs})
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		id := querycontract.StringVal(row, "repo_id")
		name := querycontract.StringVal(row, "repo_name")
		if id == "" || name == "" {
			continue
		}
		names[id] = name
	}
	return names, nil
}

// sortAndTruncateDirectoryRows applies the route's total order
// `file_count DESC, repo_id ASC, name ASC` and its row bound to the whole
// result.
//
// The statement carries the same three keys and the same bound already, and on
// the pinned NornicDB build that is not enough: it applies them once per
// UNWOUND repository id, so a caller granted R repositories can receive up to
// R x limit rows, each group ordered within itself. Re-sorting here is correct
// under BOTH backends rather than a workaround for one, because the union of
// the per-group top-L sets contains the whole result's top-L under that order:
// a row inside the global top-L has at most L-1 rows before it globally, hence
// at most L-1 before it inside its own group, so no group can have dropped it.
// Neo4j returns the global top-L and this re-sort is a no-op on it, while
// NornicDB returns a superset of it and this cuts the superset down.
//
// That containment holds only because the statement orders on the SAME three
// keys (buildDirectoryCypher, and the measurement is on its doc comment). If
// the statement ordered on `file_count DESC` alone, each group's bound would
// break ties arbitrarily, and a tied row a group dropped cannot be recovered
// here -- no re-sort can return a row the backend never sent -- so page
// membership would be backend-arbitrary whenever ties straddle the bound. The
// replaced statement ordered on file_count alone and had exactly that defect.
//
// With both orders aligned the page is a function of the data: the rows are the
// total order's top-L and the order within the page is that same total order.
func sortAndTruncateDirectoryRows(rows []map[string]any, limit int) []map[string]any {
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if a, b := querycontract.IntVal(left, "file_count"), querycontract.IntVal(right, "file_count"); a != b {
			return a > b
		}
		if a, b := querycontract.StringVal(left, "repo_id"), querycontract.StringVal(right, "repo_id"); a != b {
			return a < b
		}
		return querycontract.StringVal(left, "name") < querycontract.StringVal(right, "name")
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

// directoryRowRepositoryIDs returns the distinct repository ids on one page of
// directory rows, in first-seen order.
func directoryRowRepositoryIDs(rows []map[string]any) []string {
	return dedupedRepositoryIDs(rows)
}

// dedupedRepositoryIDs collects distinct non-empty `repo_id` values from graph
// rows, preserving the order they arrived in.
func dedupedRepositoryIDs(rows []map[string]any) []string {
	seen := make(map[string]struct{}, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		id := querycontract.StringVal(row, "repo_id")
		if id == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
