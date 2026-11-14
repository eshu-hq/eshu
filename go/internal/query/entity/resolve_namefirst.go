// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	entitycontract "github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

// resolveNameFirstMinNameLength bounds the omit-type name-first fan-out: a
// shorter exact name is refused with 400 instead of probing four indexes.
// The typed resolve paths keep their existing contracts; only the new
// omit-type path enforces this floor (#10659).
const resolveNameFirstMinNameLength = 3

// Name-first branch kinds carried on every row so a caller can tell what a
// match is and which selector to pass next without guessing the kind first.
const (
	nameFirstKindRepository = "repository"
	nameFirstKindWorkload   = "workload"
	nameFirstKindPackage    = "package"
	nameFirstKindEntity     = "entity"
)

// BuildResolveNameFirstRepositoryQuery renders the exact-name repository
// branch: repositories whose name or slug equals the trimmed name, with the
// caller's grant pushed into the WHERE before the LIMIT so the page is drawn
// from the granted set. Exported for the staying queryplan production-binding
// tests that pin builder bytes; see #6060.
//
// Cost note: Repository has no name index, so this is an exact-equality label
// scan bounded by LIMIT (see #10659). No index is added here: the arbiter
// ruling requires per-branch EXPLAIN/PROFILE on representative data before
// any Repository.name index decision.
func BuildResolveNameFirstRepositoryQuery(
	name string,
	limit int,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := access.GraphParams(map[string]any{"name": name})
	cypher := `MATCH (r:Repository)
		WHERE (
		   r.name = $name
		   OR r.repo_slug = $name
		)` + access.GraphPredicate("r") + `
		RETURN r.id as id, r.name as name
		ORDER BY r.id
		LIMIT $limit
	`
	params["limit"] = limit + 1
	return cypher, params
}

// BuildResolveNameFirstPackageQuery renders the exact-name package branch:
// packages whose normalized name equals the lowercased name. A scoped caller
// sees only public packages, the same literal the ecosystem browse and the
// bundle search apply; an all-scopes caller is unfiltered. Exported for the
// staying queryplan production-binding tests that pin builder bytes.
func BuildResolveNameFirstPackageQuery(normalized string, publicOnly bool, limit int) (string, map[string]any) {
	params := map[string]any{"name": normalized, "limit": limit + 1}
	cypher := `MATCH (p:Package) WHERE p.normalized_name = $name`
	if publicOnly {
		cypher += ` AND p.visibility = 'public'`
	}
	cypher += `
		RETURN p.uid AS package_id,
		       p.ecosystem AS ecosystem,
		       p.normalized_name AS normalized_name
		ORDER BY p.ecosystem, p.normalized_name, p.uid
		LIMIT $limit`
	return cypher, params
}

// nameFirstEntityResolveTruthEnvelope answers the hybrid basis: one name
// resolved by exact match across the repository, workload, package, and
// content entity indexes, so no single-index basis describes the answer.
func nameFirstEntityResolveTruthEnvelope(profile querycontract.QueryProfile) *querycontract.TruthEnvelope {
	return querycontract.BuildTruthEnvelope(
		profile,
		"code_search.exact_symbol",
		querycontract.TruthBasisHybrid,
		"resolved by exact name across repository, workload, package, and content entity indexes",
	)
}

// writeNameFirstEntityResolution answers an omit-type, repo-unscoped resolve
// by fanning out to the four exact-name branches and returning one ranked
// list. Every row carries its kind, its canonical id, and the next selector
// to pass on a follow-up call; a name matching several kinds lists every
// kind instead of picking one. Grant scoping rides each branch: the
// repository predicate, the workload resolver, the package visibility gate,
// and the content search scope all read the request's access filter, and a
// grantless caller never reaches this writer (ResolveEntity answers the
// empty list first). Each branch is bounded by limit+1 rows under the request
// context, the same bound every other resolve call carries.
func (h *Handler) writeNameFirstEntityResolution(
	w http.ResponseWriter,
	r *http.Request,
	req ResolveRequest,
	limit int,
) {
	ctx := r.Context()
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	merged := make([]map[string]any, 0, limit+1)

	if h.Neo4j != nil {
		repositories, err := h.resolveNameFirstRepositories(ctx, req.Name, limit, access)
		if err != nil {
			writeReadFailure(w, r, err, "code_search.exact_symbol", resolveEntityQueryFailedMessage)
			return
		}
		merged = append(merged, repositories...)

		workloads, err := h.resolveNameFirstWorkloads(ctx, req.Name, limit)
		if err != nil {
			h.writeNameFirstWorkloadFailure(w, r, ctx, err)
			return
		}
		merged = append(merged, workloads...)

		packages, err := h.resolveNameFirstPackages(ctx, req.Name, limit, access)
		if err != nil {
			writeReadFailure(w, r, err, "code_search.exact_symbol", resolveEntityQueryFailedMessage)
			return
		}
		merged = append(merged, packages...)
	}
	entities, err := h.resolveNameFirstEntities(ctx, req.Name, limit)
	if err != nil {
		h.writeNameFirstContentFailure(w, r, ctx, err)
		return
	}
	merged = append(merged, entities...)

	merged = normalizeResolvedEntities(merged, limit+1)
	merged, truncated := trimResolvedEntityPage(merged, limit)
	if merged == nil {
		merged = []map[string]any{}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, resolvedEntityResponse(merged, limit, truncated), nameFirstEntityResolveTruthEnvelope(h.profile()))
}

// writeNameFirstWorkloadFailure maps the workload branch failure the way the
// typed workload path does: reader fence first, then the unavailable-graph
// sentinel, then the fixed 500.
func (h *Handler) writeNameFirstWorkloadFailure(w http.ResponseWriter, r *http.Request, ctx context.Context, err error) {
	if querycontract.WriteGraphReadError(w, r, err, "code_search.exact_symbol") {
		return
	}
	if unavailableSentinel(ctx, err, errWorkloadGraphUnavailable) {
		querycontract.WriteError(w, http.StatusServiceUnavailable, errWorkloadGraphUnavailable.Error())
		return
	}
	writeReadFailure(w, r, err, "code_search.exact_symbol", resolveWorkloadEntitiesFailedMessage)
}

// writeNameFirstContentFailure maps the all-types content branch failure the
// way the typed global path does: reader fence, the two 503 sentinels, then
// the fixed 500.
func (h *Handler) writeNameFirstContentFailure(w http.ResponseWriter, r *http.Request, ctx context.Context, err error) {
	if querycontract.WriteGraphReadError(w, r, err, "code_search.exact_symbol") {
		return
	}
	if unavailableSentinel(ctx, err, entitycontract.ErrEntityNameSearchUnavailable) {
		querycontract.WriteError(w, http.StatusServiceUnavailable, entitycontract.ErrEntityNameSearchUnavailable.Error())
		return
	}
	if unavailableSentinel(ctx, err, querycontract.ErrContentSubstringIndexesNotReady) {
		querycontract.WriteError(w, http.StatusServiceUnavailable, querycontract.ErrContentSubstringIndexesNotReady.Error())
		return
	}
	writeReadFailure(w, r, err, "code_search.exact_symbol", resolveGlobalContentEntitiesFailedMessage)
}

// resolveNameFirstRepositories runs the repository branch and annotates each
// row with its kind and the repo_id follow-up selector.
func (h *Handler) resolveNameFirstRepositories(
	ctx context.Context,
	name string,
	limit int,
	access querycontract.RepositoryAccessFilter,
) ([]map[string]any, error) {
	timer := service.StartServiceQueryStage(ctx, h.Logger, "resolve_entity", name, "", "name_first_repository_lookup")
	cypher, params := BuildResolveNameFirstRepositoryQuery(name, limit, access)
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		timer.Done(ctx, slog.Int("row_count", 0))
		return nil, err
	}
	entities := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		id := querycontract.StringVal(row, "id")
		if id == "" {
			continue
		}
		repoName := querycontract.StringVal(row, "name")
		entities = append(entities, map[string]any{
			"id":        id,
			"labels":    []string{"Repository"},
			"name":      repoName,
			"repo_id":   id,
			"repo_name": repoName,
			"kind":      nameFirstKindRepository,
			"next_selector": map[string]any{
				"argument": "repo_id",
				"value":    id,
			},
		})
	}
	timer.Done(ctx, slog.Int("row_count", len(entities)))
	return entities, nil
}

// resolveNameFirstWorkloads runs the workload branch over the authoritative
// graph exact-name resolver, so service names match through the same w.name
// equality the service routes use. Each row carries the type follow-up
// selector plus the row's repo_id for narrowing, and a legacy name-only
// twin is marked legacy_id exactly like the service ambiguity candidates.
func (h *Handler) resolveNameFirstWorkloads(ctx context.Context, name string, limit int) ([]map[string]any, error) {
	timer := service.StartServiceQueryStage(ctx, h.Logger, "resolve_entity", name, "", "name_first_workload_lookup")
	workloads, err := h.ResolveWorkloadEntities(ctx, name, "", limit+1)
	if err != nil {
		timer.Done(ctx, slog.Int("row_count", 0))
		return nil, err
	}
	for _, workload := range workloads {
		attachSemanticSummary(workload)
		workload["kind"] = nameFirstKindWorkload
		workload["next_selector"] = map[string]any{
			"argument": "type",
			"value":    "workload",
		}
		if id := stringField(workload, "id"); legacyworkload.IsLegacyID(id, stringField(workload, "name")) {
			workload["legacy_id"] = true
		}
	}
	timer.Done(ctx, slog.Int("row_count", len(workloads)))
	return workloads, nil
}

// resolveNameFirstPackages runs the package branch: one exact normalized-name
// read across ecosystems. A scoped caller sees only public packages.
func (h *Handler) resolveNameFirstPackages(
	ctx context.Context,
	name string,
	limit int,
	access querycontract.RepositoryAccessFilter,
) ([]map[string]any, error) {
	timer := service.StartServiceQueryStage(ctx, h.Logger, "resolve_entity", name, "", "name_first_package_lookup")
	cypher, params := BuildResolveNameFirstPackageQuery(strings.ToLower(name), access.Scoped(), limit)
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		timer.Done(ctx, slog.Int("row_count", 0))
		return nil, err
	}
	entities := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		id := querycontract.StringVal(row, "package_id")
		if id == "" {
			continue
		}
		entities = append(entities, map[string]any{
			"id":        id,
			"labels":    []string{"Package"},
			"name":      querycontract.StringVal(row, "normalized_name"),
			"ecosystem": querycontract.StringVal(row, "ecosystem"),
			"kind":      nameFirstKindPackage,
			"next_selector": map[string]any{
				"argument": "package_id",
				"value":    id,
			},
		})
	}
	timer.Done(ctx, slog.Int("row_count", len(entities)))
	return entities, nil
}

// resolveNameFirstEntities runs the content branch: one exact-name probe
// across all content entity types. Unlike resolveGlobalContentEntities, no
// per-type filter is applied, so a single bounded query covers every type
// instead of one query per type.
func (h *Handler) resolveNameFirstEntities(ctx context.Context, name string, limit int) ([]map[string]any, error) {
	timer := service.StartServiceQueryStage(ctx, h.Logger, "resolve_entity", name, "", "name_first_entity_lookup")
	searcher, ok := h.Content.(entitycontract.EntityNameSearcher)
	if !ok {
		timer.Done(ctx, slog.Int("row_count", 0))
		return nil, entitycontract.ErrEntityNameSearchUnavailable
	}
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	search := entitycontract.EntityNameSearch{
		Name: name, Match: entitycontract.EntityNameMatchExact,
		Scope: entitycontract.EntityNameScopeAll, Limit: limit + 1,
	}
	if access.Scoped() {
		search.Scope = entitycontract.EntityNameScopeRepositories
		search.RepositoryIDs = access.RepositorySearchIDs()
	}
	rows, err := searcher.SearchEntityNames(ctx, search)
	if err != nil {
		timer.Done(ctx, slog.Int("row_count", 0))
		return nil, err
	}
	entities := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entity := contentEntityToMap(row)
		entity["kind"] = nameFirstKindEntity
		entity["next_selector"] = map[string]any{
			"argument": "entity_id",
			"value":    row.EntityID,
		}
		entities = append(entities, entity)
	}
	timer.Done(ctx, slog.Int("row_count", len(entities)))
	return entities, nil
}
