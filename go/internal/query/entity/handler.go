// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/graph/rows"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Handler exposes HTTP routes for entity queries.
type Handler struct {
	Neo4j                    querycontract.GraphQuery
	Content                  querycontract.ContentStore
	CICDRunCorrelations      querycontract.CICDRunCorrelationStore
	ContainerImageIdentities supplychain.ContainerImageIdentityStore
	SBOMAttachments          supplychain.SBOMAttestationAttachmentStore
	Profile                  querycontract.QueryProfile
	Logger                   *slog.Logger
	// Instruments backs operator-facing metrics for degraded-but-successful
	// entity-context reads, e.g. QueryK8sSelectCandidateScanTruncated. Nil is
	// tolerated (metric emission is skipped) so tests can construct
	// Handler without wiring the full telemetry stack.
	Instruments *telemetry.Instruments
	// ContentRelationships builds an entity's content-derived relationships
	// for the GET /api/v0/entities/{entity_id}/context content fallback
	// (getEntityContextFromContent, context_content.go). It is
	// interface-typed rather than a concrete type or func value because
	// assertRouterFieldsWired (cmd/api, cmd/mcp-server wiring completeness
	// tests) only inspects reflect.Interface-kind fields; a nil value here
	// is refused with a 503 rather than silently returning empty
	// relationship lists (#6060).
	ContentRelationships querycontract.ContentRelationshipBuilder
}

// errContentRelationshipBuilderNotConfigured is returned when the entity
// context content fallback runs without a ContentRelationships builder. It
// mirrors the code family's sentinel: the route answers 503 so a missing
// wire-in fails loudly instead of silently returning empty relationships.
var errContentRelationshipBuilderNotConfigured = errors.New("content relationship builder not configured")

// Mount registers all entity routes on the given mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/entities/resolve", h.ResolveEntity)
	mux.HandleFunc("GET /api/v0/entities/{entity_id}/context", h.GetEntityContext)
	mux.HandleFunc("GET /api/v0/workloads/{workload_id}/context", h.GetWorkloadContext)
	mux.HandleFunc("GET /api/v0/workloads/{workload_id}/story", h.GetWorkloadStory)
	mux.HandleFunc("GET /api/v0/services/{service_name}/context", h.GetServiceContext)
	mux.HandleFunc("GET /api/v0/services/{service_name}/story", h.GetServiceStory)
	mux.HandleFunc("GET /api/v0/investigations/services/{service_name}", h.InvestigateService)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil {
		return querycontract.ProfileProduction
	}
	return querycontract.NormalizeQueryProfile(string(h.Profile))
}

// ResolveRequest is the request body for entity resolution.
type ResolveRequest struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	RepoID string `json:"repo_id"`
	Limit  int    `json:"limit"`
}

const serviceLookupWhereClause = "w.name = $service_name OR w.id = $service_name" // #nosec G101 -- Cypher parameterised query template, not a hardcoded credential

// BuildResolveGraphQuery renders the repository-anchored entity
// resolution Cypher for req, or ("", nil) when req carries no RepoID: global
// resolution never touches the graph. Exported for the staying queryplan
// production-binding tests that pin builder bytes; see #6060.
//
// access is accepted but unused: the function returns before it would ever
// matter. It used to also render a global (non-repository-anchored) query
// with a scoped grant, but that whole path -- guarded by
// `!repositoryAnchored`, which is unreachable past the early return two lines
// below -- was dead code (#6786 review follow-up), including the last
// tab-indented `AND EXISTS {...}` block in this file: the same
// newline/tab-before-AND shape that made NornicDB v1.3.3 drop a live WHERE
// clause elsewhere in this file, just never executed here. Removed rather
// than left as inert bait for a future edit to "fix" the early return and
// revive it.
func BuildResolveGraphQuery(
	req ResolveRequest,
	limit int,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	if req.RepoID == "" {
		return "", nil
	}
	cypher := `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e) WHERE e.name = $name`
	params := map[string]any{"name": req.Name, "repo_id": req.RepoID}

	if req.Type != "" {
		graphLabel, semanticKey, semanticValue, ok := resolveGraphEntityType(req.Type)
		if ok {
			cypher += " AND $type IN labels(e)"
			params["type"] = graphLabel
			if semanticKey != "" {
				cypher += fmt.Sprintf(" AND coalesce(e.%s, '') = $semantic_filter", semanticKey)
				params["semantic_filter"] = semanticValue
			}
		}
	}

	cypher += `
		RETURN e.id as id, labels(e) as labels, e.name as name,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + rows.GraphSemanticMetadataProjection() + `
		ORDER BY e.name
		LIMIT $limit
	`
	params["limit"] = limit + 1
	return cypher, params
}

// ResolveEntity resolves an entity by name and optional type/repo filters. Exported so the staying root resolve tests keep driving the handler; see #6060.
func (h *Handler) ResolveEntity(w http.ResponseWriter, r *http.Request) {
	var req ResolveRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	canonicalContentHandle := strings.HasPrefix(strings.TrimSpace(req.Name), contentEntityIDPrefix)
	if req.Type != "" && req.Type != "workload" && !knownResolveEntityType(req.Type) {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("unknown entity type %q", req.Type))
		return
	}
	if req.RepoID == "" {
		if req.Type == "" && !canonicalContentHandle {
			querycontract.WriteError(w, http.StatusBadRequest, "global entity resolution requires type or repo_id")
			return
		}
		if _, graphOnly := globalGraphOnlyEntityTypes[req.Type]; graphOnly {
			querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("global entity type %q requires repo_id", req.Type))
			return
		}
	}
	limit := NormalizeResolveEntityLimit(req.Limit)
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if req.RepoID != "" {
		resolvedRepoID, err := selector.ResolveExactForAccess(r.Context(), h.Neo4j, h.Content, req.RepoID, access)
		if err != nil {
			if querycontract.WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
				return
			}
			status := http.StatusBadRequest
			if selector.IsNotFound(err) {
				status = http.StatusNotFound
			}
			querycontract.WriteError(w, status, err.Error())
			return
		}
		req.RepoID = resolvedRepoID
	}
	if access.Empty() {
		truth := resolveTruthEnvelope(h.profile())
		if req.RepoID == "" {
			truth = globalContentEntityResolveTruthEnvelope(h.profile())
		}
		if strings.EqualFold(strings.TrimSpace(req.Type), "workload") {
			truth = workloadEntityResolveTruthEnvelope(h.profile())
		}
		querycontract.WriteSuccess(w, r, http.StatusOK, resolvedEntityResponse([]map[string]any{}, limit, false), truth)
		return
	}
	if h.writeCanonicalContentEntityResolution(w, r, req, limit) {
		return
	}
	if h.writeWorkloadEntityResolution(w, r, req, limit) {
		return
	}
	if req.RepoID == "" {
		entities, err := h.resolveGlobalContentEntities(r.Context(), req.Name, req.Type, limit+1)
		if err != nil {
			if errors.Is(err, entity.ErrEntityNameSearchUnavailable) {
				querycontract.WriteError(w, http.StatusServiceUnavailable, err.Error())
				return
			}
			if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
				return
			}
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve content entities: %v", err))
			return
		}
		entities, truncated := trimResolvedEntityPage(normalizeResolvedEntities(entities, limit+1), limit)
		querycontract.WriteSuccess(w, r, http.StatusOK, resolvedEntityResponse(entities, limit, truncated), globalContentEntityResolveTruthEnvelope(h.profile()))
		return
	}

	cypher, params := BuildResolveGraphQuery(req, limit, access)

	var (
		rows []map[string]any
		err  error
	)
	if h.Neo4j != nil {
		rows, err = h.Neo4j.Run(r.Context(), cypher, params)
		if err != nil {
			if querycontract.WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
				return
			}
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
			return
		}
	}

	entities := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entity := map[string]any{
			"id":         querycontract.StringVal(row, "id"),
			"labels":     querycontract.StringSliceVal(row, "labels"),
			"name":       querycontract.StringVal(row, "name"),
			"file_path":  querycontract.StringVal(row, "file_path"),
			"repo_id":    querycontract.StringVal(row, "repo_id"),
			"repo_name":  querycontract.StringVal(row, "repo_name"),
			"language":   querycontract.StringVal(row, "language"),
			"start_line": querycontract.IntVal(row, "start_line"),
			"end_line":   querycontract.IntVal(row, "end_line"),
		}
		if metadata := taxonomy.GraphResultMetadata(row); len(metadata) > 0 {
			entity["metadata"] = metadata
		}
		entities = append(entities, entity)
	}
	entities, err = h.EnrichEntityResultsWithContentMetadata(r.Context(), entities, req.RepoID, req.Name, limit+1)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich entities: %v", err))
		return
	}
	for i := range entities {
		attachSemanticSummary(entities[i])
	}
	if _, err := hydrateResolvedEntityRepoIdentity(r.Context(), h.Neo4j, h.Content, entities); err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("hydrate entity repo identity: %v", err))
		return
	}
	entities = normalizeResolvedEntities(entities, limit+1)
	entities, truncated := trimResolvedEntityPage(entities, limit)
	if len(entities) == 0 {
		entities, err = h.resolveEntityFromContent(r.Context(), req.Name, req.Type, req.RepoID, limit+1)
		if err != nil {
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve content entities: %v", err))
			return
		}
		entities, truncated = trimResolvedEntityPage(entities, limit)
	}

	querycontract.WriteSuccess(w, r, http.StatusOK, resolvedEntityResponse(entities, limit, truncated), resolveTruthEnvelope(h.profile()))
}

// GetEntityContext retrieves the context for a specific entity. Exported so the staying graph-read-error tests keep driving the handler; see #6060.
func (h *Handler) GetEntityContext(w http.ResponseWriter, r *http.Request) {
	entityID := querycontract.PathParam(r, "entity_id")
	if entityID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "entity_id is required")
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		querycontract.WriteError(w, http.StatusNotFound, "entity not found")
		return
	}

	// Scoped mode used to add an `AND EXISTS { MATCH ... WHERE <grant> }`
	// block here to bound e to the caller's granted repositories. On the
	// pinned NornicDB v1.3.3 image that multi-line `AND EXISTS {...}` group
	// is unreliable: it can silently drop the WHOLE WHERE, including the
	// unrelated `e.id = $entity_id` anchor, so a scoped caller's request for
	// one entity could read back an arbitrary DIFFERENT entity (#6786). The
	// grant is now decided in Go instead, from the single-line-WHERE
	// OPTIONAL MATCH below (already proven safe) plus the id/repo_id checks
	// after RunSingle: the row-id equality check just below guards
	// e.id = $entity_id even if a future backend regresses that anchor, and
	// the access.AllowsRepositoryID check after hydration (unchanged) is
	// what actually fails a scoped, ungranted read closed to not-found.
	//
	// The bare, unlabeled `MATCH (e)` anchor scanned every node in the graph
	// for every call regardless of scope -- the classic all-node-scan shape
	// (docs/public/reference/cypher-performance.md, "unlabeled anchor"),
	// proven live on ops-qa (issue #7006) to blow the 10s bounded-read
	// deadline on every request.
	//
	// A single `MATCH (e:A|B|C)` label DISJUNCTION looks like the fix, and is
	// the shape codequery/chain.AnchorLabelDisjunction and
	// impacttrace.ImpactAnchorLabelDisjunction use -- but both of those only
	// ever render on the Neo4j-compat path (BuildCallChainCypher's own NornicDB
	// branch bypasses it entirely, and the impact family never uses the raw
	// disjunction in a MATCH at all, only Go-side via strings.Split for its
	// CALL{UNION} resolver below). Proven live on ops-qa (issue #7006): on
	// this NornicDB pin, `MATCH (n:A|B) WHERE n.id = $id` -- and the inline-map
	// form `MATCH (n:A|B {id: $id})` -- both silently return ZERO rows for an
	// id a single-label `MATCH (n:A) WHERE n.id = $id` resolves correctly,
	// reproduced for both a code-entity id and an infra-entity id. Shipping
	// that shape would have converted the timeout into an always-wrong
	// not-found. A many-branch `CALL{UNION}` resolver (impacttrace's own
	// pattern) is also live-proven unsafe here: 28 branches did not return
	// before a 15s timeout even though the target existed and an 8-branch
	// subset resolved it in 0.67s -- badly non-linear, not just slower.
	//
	// The only shape proven both correct and bounded is a single label per
	// MATCH. EntityContextAnchorLabels tries each candidate label in turn,
	// most-common-first, stopping at the first match; a genuinely absent
	// entity pays the full label count (14 reads, each individually proven
	// sub-second live), never a whole-graph or many-branch scan.
	//
	// The file/repo enrichment also changed shape, for a second, independent
	// reason: chaining a SECOND hop onto the (already fast, single-bound-node)
	// `OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)` -- reaching for `r:Repository`
	// via `<-[:REPO_CONTAINS]-` -- is itself live-proven catastrophic on this
	// NornicDB pin: it did not return before a 10s timeout even anchored on a
	// real Function id whose File parent exists and resolves instantly alone.
	// The reverse `(f)<-[:REPO_CONTAINS]-(r)` hop is unreliable even as a
	// REQUIRED (non-optional) single hop from an indexed, bound File node: it
	// returned zero rows for a File whose containing Repository is real and
	// resolves correctly via the forward direction
	// (`(r:Repository {id:...})-[:REPO_CONTAINS]->(f:File)`). So the fix
	// drops the graph-side Repository hop entirely: `e`/`f` already carry a
	// direct `repo_id` property (the canonical writer sets it on every
	// code-entity and File node), and the handler's existing
	// hydrateResolvedEntityRepoIdentity call below already backfills
	// repo_name from the content store once repo_id is set -- no second graph
	// round trip needed. A Repository entity's own id/name backfill the same
	// way, via that function's resolvedEntityIsRepository branch.
	buildCypher := func(label string) string {
		cypher := `
		MATCH (e:` + label + `) WHERE e.id = $entity_id
	`
		cypher += `
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)
		OPTIONAL MATCH (e)-[rel]->(target)
		RETURN e.id as id, labels(e) as labels, e.name as name,
		       f.relative_path as file_path,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + rows.GraphSemanticMetadataProjection() + `
		       ,coalesce(e.repo_id, f.repo_id) as repo_id,
		       collect(DISTINCT {type: type(rel), target_name: target.name, target_id: target.id}) as relationships
	`
		return cypher
	}

	params := access.GraphParams(map[string]any{"entity_id": entityID})
	var row map[string]any
	var err error
	if h.Neo4j != nil {
		ctx := querycontract.WithGraphQueryName(r.Context(), "code_search.fuzzy_symbol")
		for _, label := range EntityContextAnchorLabels {
			row, err = h.Neo4j.RunSingle(ctx, buildCypher(label), params)
			if err != nil || row != nil {
				break
			}
		}
		if err != nil {
			if querycontract.WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
				return
			}
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
			return
		}
	}

	// Defense-in-depth guard against a backend that stops honoring
	// `e.id = $entity_id` (the exact failure #6786 proved on NornicDB
	// v1.3.3): a row whose id does not match the requested entity is treated
	// as no row at all, the same not-found path a genuinely absent entity
	// takes, rather than trusted as an answer to this request.
	if row != nil {
		if gotID := querycontract.StringVal(row, "id"); gotID != entityID {
			// #6786 review follow-up (F5): this guard firing means the
			// backend returned a DIFFERENT node than the one anchored on --
			// backend anchor drift, not ordinary authorization, and an
			// operator needs to see it. Log outside the `if h.Logger != nil`
			// gate would panic on a nil Handler in tests that construct one
			// without a logger; every other Logger use in this package
			// checks the same way (context_content.go).
			if h.Logger != nil {
				h.Logger.WarnContext(r.Context(),
					"entity context graph row id did not match the requested entity id",
					"requested_entity_id", entityID,
					"returned_entity_id", gotID,
					"reason", "backend_anchor_mismatch",
				)
			}
			h.recordScopedGrantDenied(r.Context(), "entity_context", "backend_anchor_mismatch")
			row = nil
		}
	}

	if row == nil {
		response, fallbackErr := h.getEntityContextFromContent(r.Context(), entityID)
		if fallbackErr != nil {
			if errors.Is(fallbackErr, errContentRelationshipBuilderNotConfigured) {
				querycontract.WriteError(w, http.StatusServiceUnavailable, fallbackErr.Error())
				return
			}
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", fallbackErr))
			return
		}
		if response == nil {
			querycontract.WriteError(w, http.StatusNotFound, "entity not found")
			return
		}
		response["result_limits"] = contextResultLimits(response, entityID)
		response["partial_reasons"] = querycontract.ContextPartialReasons(response)
		querycontract.WriteSuccess(w, r, http.StatusOK, response, contextTruthEnvelope(h.profile()))
		return
	}

	response := map[string]any{
		"id":            querycontract.StringVal(row, "id"),
		"labels":        querycontract.StringSliceVal(row, "labels"),
		"name":          querycontract.StringVal(row, "name"),
		"file_path":     querycontract.StringVal(row, "file_path"),
		"repo_id":       querycontract.StringVal(row, "repo_id"),
		"repo_name":     querycontract.StringVal(row, "repo_name"),
		"language":      querycontract.StringVal(row, "language"),
		"start_line":    querycontract.IntVal(row, "start_line"),
		"end_line":      querycontract.IntVal(row, "end_line"),
		"relationships": extractRelationships(row),
	}
	if metadata := taxonomy.GraphResultMetadata(row); len(metadata) > 0 {
		response["metadata"] = metadata
	}
	if _, err := hydrateResolvedEntityRepoIdentity(r.Context(), h.Neo4j, h.Content, []map[string]any{response}); err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("hydrate entity repo identity: %v", err))
		return
	}
	if access.Scoped() && !access.AllowsRepositoryID(querycontract.StringVal(response, "repo_id")) {
		h.recordScopedGrantDenied(r.Context(), "entity_context", "grant_denied")
		querycontract.WriteError(w, http.StatusNotFound, "entity not found")
		return
	}
	enriched, err := h.EnrichEntityResultsWithContentMetadata(r.Context(), []map[string]any{response}, querycontract.StringVal(response, "repo_id"), querycontract.StringVal(row, "name"), 1)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich entity context: %v", err))
		return
	}
	response = enriched[0]
	attachSemanticSummary(response)

	response["result_limits"] = contextResultLimits(response, entityID)
	response["partial_reasons"] = querycontract.ContextPartialReasons(response)
	querycontract.WriteSuccess(w, r, http.StatusOK, response, contextTruthEnvelope(h.profile()))
}
