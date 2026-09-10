// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/routes"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file holds the *CodeHandler surface of the route-to-caller family.
// Request validation, route selection, traversal, and shaping live in the
// routes leaf; the HTTP handler, the impact assembler, and four
// grandfathered graph-read methods stay here because Go requires methods
// to live in their type's package. Those four carry queryplan
// source_sha256 pins (see go/internal/queryplan/grandfathered_non_hot.go):
// their bodies are byte-identical to the pre-move text, and the helpers
// they name resolve through the same-named forwarders at the bottom of
// this file. Edit a pinned body only with a manifest update in the same
// change; re-freezing a digest to match an edit is not a fix.

// RouteToCallerCapability aliases the routes leaf's capability so the
// seam, the mux wiring, and every existing spelling keep resolving to
// one definition.
const RouteToCallerCapability = routes.Capability

// routeToCallerRequest aliases the routes leaf's request type so every
// existing spelling (including the digest-pinned method signatures)
// keeps resolving to one definition.
type routeToCallerRequest = routes.Request

// routeToCallerRoute aliases the routes leaf's resolved-route type for
// the same reason.
type routeToCallerRoute = routes.Route

// selectRouteToCallerRoute forwards to the routes leaf; the live test
// names the pre-move spelling.
var selectRouteToCallerRoute = routes.SelectRoute

// splitRouteToCallerRelationships forwards to the routes leaf; the live
// test names the pre-move spelling.
func splitRouteToCallerRelationships(rows []map[string]any, limit int) ([]map[string]any, []map[string]any, bool) {
	return routes.SplitRelationships(rows, limit)
}

func (h *CodeHandler) handleRouteToCaller(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(h.profile(), RouteToCallerCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"route-to-caller tracing requires authoritative graph mode",
			ErrorCodeUnsupportedCapability,
			RouteToCallerCapability,
			h.profile(),
			querycontract.RequiredProfile(RouteToCallerCapability),
		)
		return
	}

	var req routeToCallerRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Normalize()
	if err := req.Validate(); err != nil {
		WriteErrorEnvelope(w, r, http.StatusBadRequest, &ErrorEnvelope{
			Code:       ErrorCodeInvalidArgument,
			Message:    err.Error(),
			Capability: RouteToCallerCapability,
		})
		return
	}
	if h.Neo4j == nil {
		WriteErrorEnvelope(w, r, http.StatusServiceUnavailable, &ErrorEnvelope{
			Code:       ErrorCodeBackendUnavailable,
			Message:    "route-to-caller tracing requires a configured graph backend",
			Capability: RouteToCallerCapability,
		})
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if !routes.AllowedByScope(access, req) {
		h.writeRouteToCallerNotFound(w, r, "route not found")
		return
	}

	routeRows, err := h.routeToCallerRouteRows(r, req)
	if err != nil {
		if WriteGraphReadError(w, r, err, RouteToCallerCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	route, status, ok := routes.SelectRoute(routeRows)
	if !ok {
		switch status {
		case "not_found":
			h.writeRouteToCallerNotFound(w, r, "route not found")
		case "ambiguous":
			WriteErrorEnvelope(w, r, http.StatusConflict, &ErrorEnvelope{
				Code:       ErrorCodeAmbiguous,
				Message:    "route selector matched multiple endpoints or handlers",
				Capability: RouteToCallerCapability,
			})
		default:
			h.writeRouteToCallerNotFound(w, r, "route not found")
		}
		return
	}

	if route.HandlerID == "" {
		WriteSuccess(w, r, http.StatusOK, map[string]any{
			"status":       "unsupported",
			"partial":      true,
			"truncated":    false,
			"unsupported":  []string{"no exact HANDLES_ROUTE edge exists for this endpoint selector"},
			"route":        route.RouteMap(),
			"handler":      nil,
			"callers":      []map[string]any{},
			"callees":      []map[string]any{},
			"impact":       routes.EmptyImpact(),
			"max_depth":    req.MaxDepth,
			"limit":        req.Limit,
			"truth_source": "HANDLES_ROUTE",
		}, BuildTruthEnvelope(h.profile(), RouteToCallerCapability, TruthBasisAuthoritativeGraph, "resolved exact route endpoint; no HANDLES_ROUTE handler edge was present"))
		return
	}

	relationshipRows, err := h.routeToCallerRelationshipRows(r, route.HandlerID, req)
	if err != nil {
		if WriteGraphReadError(w, r, err, RouteToCallerCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	callers, callees, truncated := routes.SplitRelationships(relationshipRows, req.Limit)
	impact, err := h.routeToCallerImpact(r, route, req.Limit)
	if err != nil {
		if WriteGraphReadError(w, r, err, RouteToCallerCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	responseStatus := "complete"
	if truncated {
		responseStatus = "partial"
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"status":       responseStatus,
		"partial":      truncated,
		"truncated":    truncated,
		"unsupported":  []string{},
		"route":        route.RouteMap(),
		"handler":      route.HandlerMap(),
		"callers":      callers,
		"callees":      callees,
		"impact":       impact,
		"max_depth":    req.MaxDepth,
		"limit":        req.Limit,
		"truth_source": "HANDLES_ROUTE",
	}, BuildTruthEnvelope(h.profile(), RouteToCallerCapability, TruthBasisAuthoritativeGraph, "resolved from exact HANDLES_ROUTE edge and bounded CALLS traversal"))
}

func (h *CodeHandler) writeRouteToCallerNotFound(w http.ResponseWriter, r *http.Request, message string) {
	WriteErrorEnvelope(w, r, http.StatusNotFound, &ErrorEnvelope{
		Code:       ErrorCodeNotFound,
		Message:    message,
		Capability: RouteToCallerCapability,
	})
}

// routeToCallerRouteRows resolves the joined endpoint/handler rows through
// the pinned endpoint and handler reads below, so the grandfathered
// digests keep covering the live path. Tests name this spelling.
func (h *CodeHandler) routeToCallerRouteRows(r *http.Request, req routeToCallerRequest) ([]map[string]any, error) {
	endpointRows, err := h.routeToCallerEndpointRows(r, req)
	if err != nil {
		return nil, err
	}
	handlerRows, err := h.routeToCallerHandlerRows(r, req)
	if err != nil {
		return nil, err
	}
	return routes.JoinRouteRows(endpointRows, handlerRows, req.Limit+1), nil
}

// routeToCallerRelationshipRows resolves the handler label through the
// pinned read below and runs both directional traversals in the routes
// leaf. Tests name this spelling.
func (h *CodeHandler) routeToCallerRelationshipRows(
	r *http.Request,
	handlerID string,
	req routeToCallerRequest,
) ([]map[string]any, error) {
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	label, err := h.routeToCallerHandlerLabel(r, handlerID)
	if err != nil {
		return nil, err
	}
	return routes.RelationshipRows(r.Context(), h.Neo4j, handlerID, req, access, label)
}

// The four methods below carry queryplan source_sha256 pins
// (grandfathered_non_hot.go): their bodies are byte-identical to the
// pre-move text. Do not edit them; re-freezing a digest to match an edit
// is not a fix.

// routeToCallerEndpointRows reads the endpoints matching the route selector
// (optionally anchored on the exposing workload when a service filter is set).
func (h *CodeHandler) routeToCallerEndpointRows(r *http.Request, req routeToCallerRequest) ([]map[string]any, error) {
	params := map[string]any{"path": req.Path, "route_limit": req.Limit + 1}
	predicates := []string{"endpoint.path = $path"}
	if req.RepoID != "" {
		params["repo_id"] = req.RepoID
		predicates = append(predicates, "endpoint.repo_id = $repo_id")
	}
	if req.ServiceID != "" {
		params["service_id"] = req.ServiceID
		predicates = append(predicates, "coalesce(workload.id, workload.uid) = $service_id")
	}
	if req.ServiceName != "" {
		params["service_name"] = req.ServiceName
		predicates = append(predicates, "workload.name = $service_name")
	}
	params = routeToCallerAccessParams(r, params)
	if accessPredicate := routeToCallerEndpointAccessPredicate(r); accessPredicate != "" {
		predicates = append(predicates, accessPredicate)
	}
	match := "MATCH (endpoint:Endpoint)"
	if req.ServiceID != "" || req.ServiceName != "" {
		match = "MATCH (workload:Workload)-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)"
	}
	cypher := match + `
		WHERE ` + strings.Join(predicates, " AND ") + `
		RETURN DISTINCT coalesce(endpoint.id, endpoint.uid) as endpoint_id,
		       endpoint.path as path,
		       endpoint.repo_id as repo_id,
		       endpoint.framework as endpoint_framework
		ORDER BY repo_id, path, endpoint_id
		LIMIT $route_limit`
	return h.Neo4j.Run(r.Context(), cypher, params)
}

// routeToCallerHandlerRows reads the HANDLES_ROUTE handlers for the endpoints
// matching the route selector path (and optional method).
func (h *CodeHandler) routeToCallerHandlerRows(r *http.Request, req routeToCallerRequest) ([]map[string]any, error) {
	params := map[string]any{"path": req.Path, "method": req.Method, "route_limit": req.Limit + 1}
	predicates := []string{"endpoint.path = $path", "($method = '' OR coalesce(route.http_method, '') = $method)"}
	// Scope the handler half by the same route selectors as the endpoint half so
	// it does not read handlers of same-path endpoints in other repos/services
	// (the endpoint-id join drops those, but scope the read directly too).
	if req.RepoID != "" {
		params["repo_id"] = req.RepoID
		predicates = append(predicates, "endpoint.repo_id = $repo_id")
	}
	if req.ServiceID != "" {
		params["service_id"] = req.ServiceID
		predicates = append(predicates, "coalesce(workload.id, workload.uid) = $service_id")
	}
	if req.ServiceName != "" {
		params["service_name"] = req.ServiceName
		predicates = append(predicates, "workload.name = $service_name")
	}
	params = routeToCallerAccessParams(r, params)
	if accessPredicate := routeToCallerEndpointAccessPredicate(r); accessPredicate != "" {
		predicates = append(predicates, accessPredicate)
	}
	// A service selector requires the endpoint to be exposed by the matching
	// workload; the WORKLOAD -> ENDPOINT <- HANDLES_ROUTE linear pattern is a
	// single MATCH clause (NornicDB-safe, verified live).
	match := "MATCH (handler)-[route:HANDLES_ROUTE]->(endpoint:Endpoint)"
	if req.ServiceID != "" || req.ServiceName != "" {
		match = "MATCH (workload:Workload)-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)<-[route:HANDLES_ROUTE]-(handler)"
	}
	cypher := match + `
		WHERE ` + strings.Join(predicates, " AND ") + `
		RETURN DISTINCT coalesce(endpoint.id, endpoint.uid) as endpoint_id,
		       route.http_method as http_method,
		       route.framework as route_framework,
		       coalesce(handler.id, handler.uid) as handler_id,
		       handler.name as handler_name,
		       coalesce(handler.file_path, handler.relative_path) as handler_file_path,
		       handler.language as handler_language,
		       handler.start_line as handler_start_line,
		       handler.end_line as handler_end_line
		ORDER BY handler_name, handler_id
		LIMIT $route_limit`
	return h.Neo4j.Run(r.Context(), cypher, params)
}

// routeToCallerHandlerLabel resolves the handler node's primary label so the
// relationship traversal can anchor a labelled path start (required by NornicDB
// for a variable-length pattern). Only a known code-entity label is returned, so
// the interpolated label is never attacker-influenced; an unknown/missing label
// yields "" and the caller returns no relationships.
func (h *CodeHandler) routeToCallerHandlerLabel(r *http.Request, handlerID string) (string, error) {
	row, err := h.Neo4j.RunSingle(r.Context(),
		`MATCH (handler) WHERE coalesce(handler.id, handler.uid) = $handler_id RETURN head(labels(handler)) AS label`,
		map[string]any{"handler_id": handlerID})
	if err != nil {
		return "", err
	}
	label := StringVal(row, "label")
	if !codeEntityLabelAllowed(label) {
		return "", nil
	}
	return label, nil
}

// routeToCallerImpactRows runs one single-clause impact set read and returns its
// rows as id/name(/repo_id) maps, dropping rows with an empty id.
func (h *CodeHandler) routeToCallerImpactRows(r *http.Request, cypher string, params map[string]any) ([]map[string]any, error) {
	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		id := StringVal(row, "id")
		if id == "" {
			continue
		}
		entry := map[string]any{"id": id, "name": StringVal(row, "name")}
		if _, ok := row["repo_id"]; ok {
			entry["repo_id"] = StringVal(row, "repo_id")
		}
		out = append(out, entry)
	}
	return out, nil
}

// routeToCallerImpact returns the workloads and repositories exposing/running
// the route's handler. The prior single query chained four OPTIONAL MATCH
// clauses into map-valued `collect(DISTINCT {…})` aggregations, which the pinned
// NornicDB build corrupts to literal expression text (#5287). It is split into
// three single-clause set reads whose scalar id/name/repo_id columns are
// assembled into maps in Go.
func (h *CodeHandler) routeToCallerImpact(
	r *http.Request,
	route routeToCallerRoute,
	limit int,
) (map[string]any, error) {
	params := routeToCallerAccessParams(r, map[string]any{
		"handler_id":  route.HandlerID,
		"endpoint_id": route.EndpointID,
		"limit":       limit,
	})
	endpointWorkloads, err := h.routeToCallerImpactRows(r, `MATCH (endpointWorkload:Workload)-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)
		WHERE coalesce(endpoint.id, endpoint.uid) = $endpoint_id`+routeToCallerRequiredNodeAccessClause(r, "endpointWorkload")+`
		RETURN DISTINCT endpointWorkload.id AS id, endpointWorkload.name AS name, endpointWorkload.repo_id AS repo_id
		ORDER BY id LIMIT $limit`, params)
	if err != nil {
		return nil, err
	}
	runtimeWorkloads, err := h.routeToCallerImpactRows(r, `MATCH (handler)-[:RUNS_IN]->(runtimeWorkload:Workload)
		WHERE coalesce(handler.id, handler.uid) = $handler_id`+routeToCallerRequiredNodeAccessClause(r, "runtimeWorkload")+`
		RETURN DISTINCT runtimeWorkload.id AS id, runtimeWorkload.name AS name, runtimeWorkload.repo_id AS repo_id
		ORDER BY id LIMIT $limit`, params)
	if err != nil {
		return nil, err
	}
	repositories, err := h.routeToCallerImpactRows(r, `MATCH (repo:Repository)-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)
		WHERE coalesce(endpoint.id, endpoint.uid) = $endpoint_id`+routeToCallerRequiredRepositoryAccessClause(r, "repo")+`
		RETURN DISTINCT repo.id AS id, repo.name AS name
		ORDER BY id LIMIT $limit`, params)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"workloads":    routes.MergeMaps(endpointWorkloads, runtimeWorkloads, limit),
		"repositories": repositories,
	}, nil
}

// The helpers below keep the pre-move spellings the pinned bodies above
// resolve, forwarding to the routes leaf. They are the only new
// definitions this file adds besides the handler surface.

// codeEntityLabelAllowed reports whether label is a known code-entity label.
func codeEntityLabelAllowed(label string) bool {
	return routes.LabelAllowed(label)
}

func routeToCallerAccessParams(r *http.Request, params map[string]any) map[string]any {
	return routes.AccessParams(querycontract.RepositoryAccessFilterFromContext(r.Context()), params)
}

func routeToCallerEndpointAccessPredicate(r *http.Request) string {
	return routes.EndpointAccessPredicate(querycontract.RepositoryAccessFilterFromContext(r.Context()))
}

// routeToCallerRequiredNodeAccessClause renders the scoped-access predicate for
// a required (non-OPTIONAL) node match, ANDed onto the branch WHERE. Unlike the
// OPTIONAL form it has no `alias IS NULL OR` escape: an out-of-grant node is
// excluded (fail-closed), which is the correct scoped behavior for the split
// impact set reads.
func routeToCallerRequiredNodeAccessClause(r *http.Request, alias string) string {
	return routes.RequiredNodeAccessClause(querycontract.RepositoryAccessFilterFromContext(r.Context()), alias)
}

// routeToCallerRequiredRepositoryAccessClause is the required-match access
// predicate for a Repository node, whose grant identity is its own id.
func routeToCallerRequiredRepositoryAccessClause(r *http.Request, alias string) string {
	return routes.RequiredRepositoryAccessClause(querycontract.RepositoryAccessFilterFromContext(r.Context()), alias)
}
