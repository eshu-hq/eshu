// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strings"
)

func (h *CodeHandler) routeToCallerRouteRows(r *http.Request, req routeToCallerRequest) ([]map[string]any, error) {
	params := map[string]any{
		"path":        req.Path,
		"method":      req.Method,
		"route_limit": req.Limit + 1,
	}
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
	accessPredicate := routeToCallerEndpointAccessPredicate(r)
	if accessPredicate != "" {
		predicates = append(predicates, accessPredicate)
	}

	var cypher strings.Builder
	if req.ServiceID != "" || req.ServiceName != "" {
		cypher.WriteString(`
		MATCH (workload:Workload)-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)
		`)
	} else {
		cypher.WriteString(`
		MATCH (endpoint:Endpoint)
		`)
	}
	cypher.WriteString("WHERE ")
	cypher.WriteString(strings.Join(predicates, " AND "))
	cypher.WriteString(`
		OPTIONAL MATCH (handler)-[route:HANDLES_ROUTE]->(endpoint:Endpoint)
		WHERE $method = '' OR coalesce(route.http_method, '') = $method
		RETURN coalesce(endpoint.id, endpoint.uid) as endpoint_id,
		       endpoint.path as path,
		       endpoint.repo_id as repo_id,
		       route.http_method as http_method,
		       coalesce(route.framework, endpoint.framework) as framework,
		       coalesce(handler.id, handler.uid) as handler_id,
		       handler.name as handler_name,
		       coalesce(handler.file_path, handler.relative_path) as handler_file_path,
		       handler.language as handler_language,
		       handler.start_line as handler_start_line,
		       handler.end_line as handler_end_line
		ORDER BY repo_id, path, http_method, handler_name, handler_id
		LIMIT $route_limit
	`)
	return h.Neo4j.Run(r.Context(), cypher.String(), params)
}

func (h *CodeHandler) routeToCallerRelationshipRows(
	r *http.Request,
	handlerID string,
	req routeToCallerRequest,
) ([]map[string]any, error) {
	params := routeToCallerAccessParams(r, map[string]any{
		"handler_id": handlerID,
		"limit":      req.Limit + 1,
	})
	cypher := `
		MATCH (handler)
		WHERE coalesce(handler.id, handler.uid) = $handler_id
		CALL {
			WITH handler
			MATCH path = (caller)-[:CALLS*1..` + intLiteral(req.MaxDepth) + `]->(handler)
			WHERE caller <> handler` + routeToCallerEntityAccessPredicate(r, "caller") + routeToCallerPathAccessPredicate(r, "path") + `
			RETURN 'incoming' as direction, length(path) as depth, caller as entity
			UNION ALL
			WITH handler
			MATCH path = (handler)-[:CALLS*1..` + intLiteral(req.MaxDepth) + `]->(callee)
			WHERE callee <> handler` + routeToCallerEntityAccessPredicate(r, "callee") + routeToCallerPathAccessPredicate(r, "path") + `
			RETURN 'outgoing' as direction, length(path) as depth, callee as entity
		}
		RETURN direction,
		       depth,
		       coalesce(entity.id, entity.uid) as entity_id,
		       entity.name as name,
		       coalesce(entity.file_path, entity.relative_path) as file_path,
		       entity.repo_id as repo_id,
		       entity.language as language,
		       entity.start_line as start_line,
		       entity.end_line as end_line
		ORDER BY direction, depth, name, entity_id
		LIMIT $limit
	`
	return h.Neo4j.Run(r.Context(), cypher, params)
}

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
	cypher := `
		MATCH (handler)
		WHERE coalesce(handler.id, handler.uid) = $handler_id
		OPTIONAL MATCH (handler)-[:HANDLES_ROUTE]->(endpoint:Endpoint)
		WHERE coalesce(endpoint.id, endpoint.uid) = $endpoint_id
		OPTIONAL MATCH (endpointWorkload:Workload)-[:EXPOSES_ENDPOINT]->(endpoint)` + routeToCallerOptionalNodeAccessClause(r, "endpointWorkload") + `
		OPTIONAL MATCH (handler)-[:RUNS_IN]->(runtimeWorkload:Workload)` + routeToCallerOptionalNodeAccessClause(r, "runtimeWorkload") + `
		OPTIONAL MATCH (repo:Repository)-[:EXPOSES_ENDPOINT]->(endpoint)` + routeToCallerOptionalRepositoryAccessClause(r, "repo") + `
		RETURN collect(DISTINCT {id: endpointWorkload.id, name: endpointWorkload.name, repo_id: endpointWorkload.repo_id})[0..$limit] as endpoint_workloads,
		       collect(DISTINCT {id: runtimeWorkload.id, name: runtimeWorkload.name, repo_id: runtimeWorkload.repo_id})[0..$limit] as runtime_workloads,
		       collect(DISTINCT {id: repo.id, name: repo.name})[0..$limit] as repositories
	`
	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return emptyRouteToCallerImpact(), nil
	}
	workloads := mergeRouteToCallerMaps(
		cleanRouteToCallerMaps(rows[0]["endpoint_workloads"]),
		cleanRouteToCallerMaps(rows[0]["runtime_workloads"]),
		limit,
	)
	return map[string]any{
		"workloads":    workloads,
		"repositories": cleanRouteToCallerMaps(rows[0]["repositories"]),
	}, nil
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

func routeToCallerOptionalNodeAccessClause(r *http.Request, alias string) string {
	if !repositoryAccessFilterFromContext(r.Context()).scoped() {
		return ""
	}
	return "\n\t\tWHERE " + alias + " IS NULL OR (" +
		alias + ".repo_id IN $allowed_repository_ids OR " + alias + ".scope_id IN $allowed_scope_ids)"
}

func routeToCallerOptionalRepositoryAccessClause(r *http.Request, alias string) string {
	if !repositoryAccessFilterFromContext(r.Context()).scoped() {
		return ""
	}
	return "\n\t\tWHERE " + alias + " IS NULL OR (" +
		alias + ".id IN $allowed_repository_ids OR " + alias + ".scope_id IN $allowed_scope_ids)"
}

func emptyRouteToCallerImpact() map[string]any {
	return map[string]any{
		"workloads":    []map[string]any{},
		"repositories": []map[string]any{},
	}
}

func cleanRouteToCallerMaps(value any) []map[string]any {
	var items []any
	switch values := value.(type) {
	case []any:
		items = values
	case []map[string]any:
		items = make([]any, 0, len(values))
		for _, item := range values {
			items = append(items, item)
		}
	default:
		return []map[string]any{}
	}
	cleaned := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok || StringVal(row, "id") == "" {
			continue
		}
		cleaned = append(cleaned, row)
	}
	return cleaned
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
