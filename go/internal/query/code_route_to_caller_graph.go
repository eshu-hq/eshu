// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// codeEntityLabelSet indexes the code-entity labels a route handler may carry
// (mirrors codeCallChainAnchorLabelDisjunction). It gates any label interpolated
// into a Cypher traversal so the label text is never attacker-influenced.
var codeEntityLabelSet = func() map[string]struct{} {
	set := map[string]struct{}{}
	for _, label := range strings.Split(codeCallChainAnchorLabelDisjunction, "|") {
		set[label] = struct{}{}
	}
	return set
}()

// codeEntityLabelAllowed reports whether label is a known code-entity label.
func codeEntityLabelAllowed(label string) bool {
	_, ok := codeEntityLabelSet[label]
	return ok
}

// routeToCallerRouteRows resolves the endpoint(s) matching the route selector and
// their HANDLES_ROUTE handlers. The prior single query chained an endpoint MATCH
// with an OPTIONAL MATCH handler into a computed RETURN, which the pinned
// NornicDB build corrupts to literal expression text (#5287). It is split into a
// single-clause endpoint read and a single-clause handler read, left-joined in
// Go on the endpoint id so endpoints with no handler still surface (matching the
// prior OPTIONAL MATCH semantics).
func (h *CodeHandler) routeToCallerRouteRows(r *http.Request, req routeToCallerRequest) ([]map[string]any, error) {
	endpointRows, err := h.routeToCallerEndpointRows(r, req)
	if err != nil {
		return nil, err
	}
	handlerRows, err := h.routeToCallerHandlerRows(r, req)
	if err != nil {
		return nil, err
	}
	return joinRouteToCallerRouteRows(endpointRows, handlerRows, req.Limit+1), nil
}

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

// joinRouteToCallerRouteRows left-joins endpoint rows with their handler rows on
// the endpoint id, emitting one row per (endpoint, handler) and a single
// null-handler row for endpoints with no handler (the prior OPTIONAL MATCH
// semantics). Ordering mirrors the prior
// `ORDER BY repo_id, path, http_method, handler_name, handler_id`.
func joinRouteToCallerRouteRows(endpointRows, handlerRows []map[string]any, limit int) []map[string]any {
	handlersByEndpoint := map[string][]map[string]any{}
	for _, hr := range handlerRows {
		eid := StringVal(hr, "endpoint_id")
		handlersByEndpoint[eid] = append(handlersByEndpoint[eid], hr)
	}
	out := make([]map[string]any, 0, len(endpointRows))
	for _, ep := range endpointRows {
		eid := StringVal(ep, "endpoint_id")
		epFramework := StringVal(ep, "endpoint_framework")
		newBase := func() map[string]any {
			return map[string]any{"endpoint_id": eid, "path": StringVal(ep, "path"), "repo_id": StringVal(ep, "repo_id")}
		}
		handlers := handlersByEndpoint[eid]
		if len(handlers) == 0 {
			row := newBase()
			row["framework"] = epFramework
			out = append(out, row)
			continue
		}
		for _, hr := range handlers {
			row := newBase()
			row["http_method"] = StringVal(hr, "http_method")
			row["framework"] = firstNonEmpty(StringVal(hr, "route_framework"), epFramework)
			row["handler_id"] = StringVal(hr, "handler_id")
			row["handler_name"] = StringVal(hr, "handler_name")
			row["handler_file_path"] = StringVal(hr, "handler_file_path")
			row["handler_language"] = StringVal(hr, "handler_language")
			row["handler_start_line"] = IntVal(hr, "handler_start_line")
			row["handler_end_line"] = IntVal(hr, "handler_end_line")
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return routeToCallerRouteRowSortKey(out[i]) < routeToCallerRouteRowSortKey(out[j])
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func routeToCallerRouteRowSortKey(row map[string]any) string {
	return StringVal(row, "repo_id") + "\x00" + StringVal(row, "path") + "\x00" +
		StringVal(row, "http_method") + "\x00" + StringVal(row, "handler_name") + "\x00" +
		StringVal(row, "handler_id")
}

// routeToCallerRelationshipRows returns the callers and callees of the route's
// handler. The prior query wrapped a UNION in a CALL subquery and computed the
// entity columns in the OUTER RETURN over the subquery node, which the pinned
// NornicDB build corrupts to literal expression text (#5287); a variable-length
// path with an unlabeled anchor also returns zero rows. It is rewritten to
// resolve the handler's label first, then run one single-clause directional path
// per direction with the KNOWN handler as the (labelled) path start, projecting
// raw nodes(path); the discovered caller/callee is the far endpoint, extracted in
// Go. Node identity inequality is expressed on id/uid because NornicDB
// mis-evaluates a `<>` between whole nodes.
func (h *CodeHandler) routeToCallerRelationshipRows(
	r *http.Request,
	handlerID string,
	req routeToCallerRequest,
) ([]map[string]any, error) {
	label, err := h.routeToCallerHandlerLabel(r, handlerID)
	if err != nil {
		return nil, err
	}
	if label == "" {
		return []map[string]any{}, nil
	}
	incoming, err := h.routeToCallerDirectionRows(r, handlerID, req, label, "incoming")
	if err != nil {
		return nil, err
	}
	outgoing, err := h.routeToCallerDirectionRows(r, handlerID, req, label, "outgoing")
	if err != nil {
		return nil, err
	}
	return append(incoming, outgoing...), nil
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

// routeToCallerDirectionRows runs one directional CALLS traversal anchored on the
// labelled handler start and returns the far-endpoint entities as relationship
// rows tagged with the direction.
func (h *CodeHandler) routeToCallerDirectionRows(
	r *http.Request,
	handlerID string,
	req routeToCallerRequest,
	label string,
	direction string,
) ([]map[string]any, error) {
	far := "callee"
	pattern := "(handler:" + label + ")-[:CALLS*1.." + intLiteral(req.MaxDepth) + "]->(callee)"
	if direction == "incoming" {
		far = "caller"
		pattern = "(handler:" + label + ")<-[:CALLS*1.." + intLiteral(req.MaxDepth) + "]-(caller)"
	}
	cypher := "MATCH path = " + pattern + `
		WHERE coalesce(handler.id, handler.uid) = $handler_id
		  AND coalesce(` + far + `.id, ` + far + `.uid) <> coalesce(handler.id, handler.uid)` +
		routeToCallerEntityAccessPredicate(r, far) + routeToCallerPathAccessPredicate(r, "path") + `
		RETURN length(path) as depth, nodes(path) as chain
		ORDER BY depth, coalesce(` + far + `.id, ` + far + `.uid)
		LIMIT $limit`
	params := routeToCallerAccessParams(r, map[string]any{"handler_id": handlerID, "limit": req.Limit + 1})
	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entity := routeToCallerEntityFromChain(row["chain"])
		if entity == nil || StringVal(entity, "entity_id") == "" {
			continue
		}
		entity["direction"] = direction
		entity["depth"] = IntVal(row, "depth")
		out = append(out, entity)
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
		"workloads":    mergeRouteToCallerMaps(endpointWorkloads, runtimeWorkloads, limit),
		"repositories": repositories,
	}, nil
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

func splitRouteToCallerRelationships(rows []map[string]any, limit int) ([]map[string]any, []map[string]any, bool) {
	callers := make([]map[string]any, 0)
	callees := make([]map[string]any, 0)
	truncated := false
	for _, row := range rows {
		if len(callers)+len(callees) >= limit {
			truncated = true
			break
		}
		item := map[string]any{
			"entity_id":  StringVal(row, "entity_id"),
			"name":       StringVal(row, "name"),
			"file_path":  StringVal(row, "file_path"),
			"repo_id":    StringVal(row, "repo_id"),
			"language":   StringVal(row, "language"),
			"start_line": IntVal(row, "start_line"),
			"end_line":   IntVal(row, "end_line"),
			"depth":      IntVal(row, "depth"),
		}
		if StringVal(row, "direction") == "incoming" {
			callers = append(callers, item)
		} else {
			callees = append(callees, item)
		}
	}
	return callers, callees, truncated
}

func routeToCallerAccessParams(r *http.Request, params map[string]any) map[string]any {
	access := repositoryAccessFilterFromContext(r.Context())
	if !access.scoped() {
		return params
	}
	params["allowed_repository_ids"] = access.grantedRepositoryIDs()
	params["allowed_scope_ids"] = access.grantedScopeIDs()
	return params
}

func routeToCallerEndpointAccessPredicate(r *http.Request) string {
	if !repositoryAccessFilterFromContext(r.Context()).scoped() {
		return ""
	}
	return "(endpoint.repo_id IN $allowed_repository_ids OR endpoint.scope_id IN $allowed_scope_ids)"
}

func routeToCallerEntityAccessPredicate(r *http.Request, alias string) string {
	if !repositoryAccessFilterFromContext(r.Context()).scoped() {
		return ""
	}
	return " AND (" + alias + ".repo_id IN $allowed_repository_ids OR " + alias + ".scope_id IN $allowed_scope_ids)"
}

func routeToCallerPathAccessPredicate(r *http.Request, pathAlias string) string {
	if !repositoryAccessFilterFromContext(r.Context()).scoped() {
		return ""
	}
	return " AND all(pathNode IN nodes(" + pathAlias + ") WHERE " +
		"(pathNode.repo_id IN $allowed_repository_ids OR pathNode.scope_id IN $allowed_scope_ids))"
}

// routeToCallerRequiredNodeAccessClause renders the scoped-access predicate for
// a required (non-OPTIONAL) node match, ANDed onto the branch WHERE. Unlike the
// OPTIONAL form it has no `alias IS NULL OR` escape: an out-of-grant node is
// excluded (fail-closed), which is the correct scoped behavior for the split
// impact set reads.
func routeToCallerRequiredNodeAccessClause(r *http.Request, alias string) string {
	if !repositoryAccessFilterFromContext(r.Context()).scoped() {
		return ""
	}
	return " AND (" + alias + ".repo_id IN $allowed_repository_ids OR " + alias + ".scope_id IN $allowed_scope_ids)"
}

// routeToCallerRequiredRepositoryAccessClause is the required-match access
// predicate for a Repository node, whose grant identity is its own id.
func routeToCallerRequiredRepositoryAccessClause(r *http.Request, alias string) string {
	if !repositoryAccessFilterFromContext(r.Context()).scoped() {
		return ""
	}
	return " AND (" + alias + ".id IN $allowed_repository_ids OR " + alias + ".scope_id IN $allowed_scope_ids)"
}

func emptyRouteToCallerImpact() map[string]any {
	return map[string]any{
		"workloads":    []map[string]any{},
		"repositories": []map[string]any{},
	}
}

func mergeRouteToCallerMaps(first []map[string]any, second []map[string]any, limit int) []map[string]any {
	capacity := len(first) + len(second)
	if capacity > limit {
		capacity = limit
	}
	merged := make([]map[string]any, 0, capacity)
	seen := make(map[string]struct{}, len(first)+len(second))
	for _, items := range [][]map[string]any{first, second} {
		for _, item := range items {
			id := StringVal(item, "id")
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			merged = append(merged, item)
			if len(merged) >= limit {
				return merged
			}
		}
	}
	return merged
}

func intLiteral(value int) string {
	return fmt.Sprintf("%d", value)
}
