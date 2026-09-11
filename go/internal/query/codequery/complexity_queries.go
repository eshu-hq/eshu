// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/graph/rows"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	// complexityNameCandidateLimit is the pre-move spelling of
	// codemodel.ComplexityNameCandidateLimit, kept for the digest-pinned
	// lookupComplexityRowByName body below.
	complexityNameCandidateLimit = codemodel.ComplexityNameCandidateLimit
)

// complexityAmbiguousError is the pre-move spelling of
// codemodel.ComplexityAmbiguousError, kept for the same digest-pinned body.
type complexityAmbiguousError = codemodel.ComplexityAmbiguousError

// normalizeComplexityListLimit forwards to
// codemodel.NormalizeComplexityListLimit so listMostComplexFunctions keeps
// its pre-move declaration bytes (Go has no function aliases).
func normalizeComplexityListLimit(limit int) int {
	return codemodel.NormalizeComplexityListLimit(limit)
}

// trimComplexityResults forwards to codemodel.TrimComplexityResults for the
// same digest-pinned body.
func trimComplexityResults(results []map[string]any, limit int) ([]map[string]any, bool) {
	return codemodel.TrimComplexityResults(results, limit)
}

// complexityCandidateMaps forwards to codemodel.ComplexityCandidateMaps for
// the digest-pinned lookupComplexityRowByName body.
func complexityCandidateMaps(rows []map[string]any) []map[string]any {
	return codemodel.ComplexityCandidateMaps(rows)
}

// lookupComplexityRowByName resolves a complexity row by function name. access
// appends the caller's grant to the Repository anchor's WHERE, so a scoped
// caller can neither read nor be told about a function in an ungranted
// repository -- including through the ambiguity candidate list this returns
// when the name matches more than one entity.
func (h *CodeHandler) lookupComplexityRowByName(
	ctx context.Context,
	functionName, repoID string,
	access querycontract.RepositoryAccessFilter,
) (map[string]any, error) {
	params := map[string]any{"entity_name": functionName, "limit": complexityNameCandidateLimit + 1}
	cypher := "\n\t\tMATCH (repo:Repository)-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e)\n\t\tWHERE e.name = $entity_name"
	if repoID != "" {
		cypher = "\n\t\tMATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e)\n\t\tWHERE e.name = $entity_name AND repo.id = $repo_id"
		params["repo_id"] = repoID
	}
	cypher += access.GraphPredicate("repo") + "\n"
	params = access.GraphParams(params)
	cypher += complexityCandidateProjection() + `
		ORDER BY file_path, start_line, id
		LIMIT $limit
	`
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		if err == nil && rows == nil {
			return h.runComplexityQuery(ctx, cypher, params)
		}
		return nil, err
	}
	if len(rows) > 1 {
		truncated := len(rows) > complexityNameCandidateLimit
		if truncated {
			rows = rows[:complexityNameCandidateLimit]
		}
		return nil, complexityAmbiguousError{
			FunctionName: functionName,
			RepoID:       repoID,
			Candidates:   complexityCandidateMaps(rows),
			Truncated:    truncated,
		}
	}
	return rows[0], nil
}

// lookupComplexityRowByID resolves a complexity row by entity id.
//
// This branch previously carried no repository predicate at all: it ignored
// even a repo_id the caller supplied, so an entity id alone returned that
// entity's repository, path, and metrics from any repository in the index. It
// now anchors on the supplied repository when there is one, and always appends
// the caller's grant (#5167).
func (h *CodeHandler) lookupComplexityRowByID(
	ctx context.Context,
	entityID, repoID string,
	access querycontract.RepositoryAccessFilter,
) (map[string]any, error) {
	params := map[string]any{"entity_id": entityID}
	where := ""
	if repoID != "" {
		where = "\n\t\tWHERE repo.id = $repo_id"
		params["repo_id"] = repoID
	}
	if predicate := access.GraphCondition("repo"); access.Scoped() {
		if where == "" {
			where = "\n\t\tWHERE " + predicate
		} else {
			where += " AND " + predicate
		}
	}
	row, err := h.runComplexityQuery(ctx, `
		MATCH (repo:Repository)-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e {id: $entity_id})`+where+`
`+complexityCandidateProjection()+`
		LIMIT 1
	`, access.GraphParams(params))
	return row, err
}

// complexityIDLookupIsRepositoryBound reports whether the entity-id lookup was
// restricted to a set of repositories, which decides whether an empty result
// may fall back to the function name.
//
// The fallback exists for a stale id: a caller holding an id the graph no
// longer has still gets the function it named. An unrestricted lookup proves
// that case, because it searched the whole index and found nothing. A lookup
// bound to a repository -- by a supplied repo_id, or by a scoped caller's grant
// -- proves nothing of the sort: the id may be live in a repository the lookup
// excluded, and answering with a same-named function from the requested
// repository would hand an exact-id caller another entity's metrics. That
// request is not-found, which is what the route's OpenAPI description and
// [HTTP API — Code] promise for an entity id held by another repository.
//
// [HTTP API — Code]: https://github.com/eshu-hq/eshu/blob/main/docs/public/reference/http-api/code.md
func complexityIDLookupIsRepositoryBound(repoID string, access querycontract.RepositoryAccessFilter) bool {
	return strings.TrimSpace(repoID) != "" || access.Scoped()
}

func complexityCandidateProjection() string {
	return `
		OPTIONAL MATCH (e)-[outgoingRel]->()
		OPTIONAL MATCH ()-[incomingRel]->(e)
		RETURN e.id as id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       repo.id as repo_id, repo.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
		       coalesce(e.cyclomatic_complexity, 0) as complexity,
		       count(DISTINCT outgoingRel) as outgoing_count,
		       count(DISTINCT incomingRel) as incoming_count,
		       count(DISTINCT outgoingRel) + count(DISTINCT incomingRel) as total_relationships,
` + rows.GraphSemanticMetadataProjection()
}

// complexityListAnchor picks the clause that binds the Repository side of the
// complexity list scan.
//
// Every caller whose answer is restricted to some set of repositories -- a
// scoped caller, and any caller that supplied a repo_id -- gets the
// File/Repository hops as a required MATCH, because the alternative does not
// filter. In Cypher a WHERE attached to an OPTIONAL MATCH constrains the
// optional pattern, not the driving row set, so a grant predicate or a
// repo.id equality appended there returns every Function in the corpus with
// only the repository columns nulled -- name, language, line span, complexity
// and the semantic metadata all still reach the caller. That was measured
// against NornicDB v1.2.3, where the same shape additionally projected the
// literal text "repo.id" into the repo_id column; both results are recorded in
// docs/internal/evidence/5167-code-family-batch-1.md. With a required MATCH the
// restriction sits in a MATCH-attached WHERE ahead of the ORDER BY and LIMIT,
// so the page is drawn from the requested set, and a function with no
// repository path at all is dropped -- the fail-closed answer for a row whose
// repository cannot be determined.
//
// Only the unscoped caller that names no repository keeps the long-standing
// shape: a bare MATCH (e:Function) with the hops in an OPTIONAL MATCH, so a
// function the graph has not attributed to a repository still ranks in a
// corpus-wide list. That text is unchanged by #5167.
func complexityListAnchor(access querycontract.RepositoryAccessFilter, repoID string) string {
	if access.Scoped() || repoID != "" {
		return `
		MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		WHERE coalesce(e.cyclomatic_complexity, 0) > 0
	`
	}
	return `
		MATCH (e:Function)
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		WHERE coalesce(e.cyclomatic_complexity, 0) > 0
	`
}

// listMostComplexFunctions ranks the most complex functions in scope. The
// caller's grant, and any repo_id it supplied, land in the WHERE attached to
// the clause complexityListAnchor chose -- required whenever either restricts
// the answer, optional only for an unscoped corpus-wide ranking.
func (h *CodeHandler) listMostComplexFunctions(
	ctx context.Context,
	repoID string,
	limit int,
	access querycontract.RepositoryAccessFilter,
) ([]map[string]any, int, bool, error) {
	limit = normalizeComplexityListLimit(limit)
	cypher := complexityListAnchor(access, repoID)
	params := map[string]any{"limit": limit + 1}
	if repoID != "" {
		cypher += " AND repo.id = $repo_id"
		params["repo_id"] = repoID
	}
	cypher += access.GraphPredicate("repo")
	params = access.GraphParams(params)
	cypher += `
		RETURN e.id as id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       repo.id as repo_id, repo.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + rows.GraphSemanticMetadataProjection() + `,
		       coalesce(e.cyclomatic_complexity, 0) as complexity
		ORDER BY complexity DESC, e.name, e.id
		LIMIT $limit
	`
	records, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, 0, false, err
	}
	results := make([]map[string]any, 0, len(records))
	for _, row := range records {
		result := map[string]any{
			"entity_id":  StringVal(row, "id"),
			"name":       StringVal(row, "name"),
			"labels":     StringSliceVal(row, "labels"),
			"file_path":  StringVal(row, "file_path"),
			"repo_id":    StringVal(row, "repo_id"),
			"repo_name":  StringVal(row, "repo_name"),
			"language":   StringVal(row, "language"),
			"start_line": IntVal(row, "start_line"),
			"end_line":   IntVal(row, "end_line"),
			"complexity": IntVal(row, "complexity"),
		}
		if metadata := querycontract.GraphResultMetadata(row); len(metadata) > 0 {
			result["metadata"] = metadata
			entitysemantics.AttachSemanticSummary(result)
		}
		results = append(results, result)
	}
	results, truncated := trimComplexityResults(results, limit)
	return results, limit, truncated, nil
}

// handleComplexity returns relationship-based complexity metrics for an entity.
func (h *CodeHandler) handleComplexity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepoID       string `json:"repo_id"`
		EntityID     string `json:"entity_id"`
		FunctionName string `json:"function_name"`
		Limit        int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, "code_quality.complexity") {
		return
	}
	// #5167 code family: the list branch's Repository anchor is optional and
	// the entity_id branch had no repository predicate at all, so the caller's
	// grant is resolved once here and pushed into every complexity builder.
	access := codeGrantAccessFilter(ctx)
	if access.Empty() {
		writeEmptyComplexityAnswer(w, r, h.profile(), req.RepoID, req.EntityID == "" && req.FunctionName == "", req.Limit)
		return
	}
	if req.EntityID == "" && req.FunctionName == "" {
		results, limit, truncated, err := h.listMostComplexFunctions(ctx, req.RepoID, req.Limit, access)
		if err != nil {
			if WriteGraphReadError(w, r, err, "code_quality.complexity") {
				return
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteSuccess(w, r, http.StatusOK, map[string]any{
			"repo_id":    req.RepoID,
			"results":    results,
			"limit":      limit,
			"truncated":  truncated,
			"result_key": "entity_id",
		}, BuildTruthEnvelope(h.profile(), "code_quality.complexity", TruthBasisHybrid, "resolved from graph-derived complexity metrics"))
		return
	}

	row, err := h.lookupComplexityRow(ctx, req.EntityID, req.FunctionName, req.RepoID, access)
	if err != nil {
		var ambiguous codemodel.ComplexityAmbiguousError
		if errors.As(err, &ambiguous) {
			codemodel.WriteComplexityAmbiguousError(w, r, ambiguous, h.profile())
			return
		}
		if WriteGraphReadError(w, r, err, "code_quality.complexity") {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		WriteError(w, http.StatusNotFound, "entity not found")
		return
	}

	response := map[string]any{
		"entity_id":           StringVal(row, "id"),
		"name":                StringVal(row, "name"),
		"labels":              StringSliceVal(row, "labels"),
		"file_path":           StringVal(row, "file_path"),
		"repo_id":             StringVal(row, "repo_id"),
		"repo_name":           StringVal(row, "repo_name"),
		"language":            StringVal(row, "language"),
		"start_line":          IntVal(row, "start_line"),
		"end_line":            IntVal(row, "end_line"),
		"complexity":          IntVal(row, "complexity"),
		"outgoing_count":      IntVal(row, "outgoing_count"),
		"incoming_count":      IntVal(row, "incoming_count"),
		"total_relationships": IntVal(row, "total_relationships"),
	}
	if metadata := querycontract.GraphResultMetadata(row); len(metadata) > 0 {
		response["metadata"] = metadata
	}
	enriched, err := h.enrichGraphSearchResultsWithContentMetadata(
		ctx,
		[]map[string]any{response},
		StringVal(row, "repo_id"),
		StringVal(row, "name"),
		1,
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteSuccess(w, r, http.StatusOK, enriched[0], BuildTruthEnvelope(h.profile(), "code_quality.complexity", TruthBasisHybrid, "resolved from graph-derived complexity metrics"))
}

func (h *CodeHandler) lookupComplexityRow(
	ctx context.Context,
	entityID, functionName, repoID string,
	access querycontract.RepositoryAccessFilter,
) (map[string]any, error) {
	if strings.TrimSpace(entityID) == "" {
		return h.lookupComplexityRowByName(ctx, functionName, repoID, access)
	}
	row, err := h.lookupComplexityRowByID(ctx, entityID, repoID, access)
	if err != nil {
		return nil, err
	}
	// The name fallback answers a stale id, and only a lookup that searched
	// every repository can prove the id is stale. See
	// complexityIDLookupIsRepositoryBound.
	if row == nil && strings.TrimSpace(functionName) != "" && !complexityIDLookupIsRepositoryBound(repoID, access) {
		return h.lookupComplexityRowByName(ctx, functionName, repoID, access)
	}
	return row, nil
}

// writeEmptyComplexityAnswer is the fail-closed response for a scoped caller
// with no grants: the same shape each branch returns when nothing matched, so
// an ungranted caller cannot tell an empty grant from an empty index.
func writeEmptyComplexityAnswer(
	w http.ResponseWriter,
	r *http.Request,
	profile QueryProfile,
	repoID string,
	listBranch bool,
	limit int,
) {
	if !listBranch {
		WriteError(w, http.StatusNotFound, "entity not found")
		return
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"repo_id":    repoID,
		"results":    []map[string]any{},
		"limit":      codemodel.NormalizeComplexityListLimit(limit),
		"truncated":  false,
		"result_key": "entity_id",
	}, BuildTruthEnvelope(profile, "code_quality.complexity", TruthBasisHybrid, "resolved from graph-derived complexity metrics"))
}
