package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
)

// dispatchTool routes an MCP tool call to the appropriate internal HTTP endpoint.
func dispatchTool(ctx context.Context, handler http.Handler, toolName string, args map[string]any, authHeader string, logger *slog.Logger) (*dispatchResult, error) {
	route, err := resolveRoute(toolName, args)
	if err != nil {
		return nil, err
	}

	logger.Debug("dispatch tool", "tool", toolName, "method", route.method, "path", route.path)

	var body io.Reader
	if route.body != nil {
		encoded, err := json.Marshal(route.body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, route.method, route.path, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/eshu.envelope+json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	// Set query parameters
	if len(route.query) > 0 {
		q := req.URL.Query()
		for k, v := range route.query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if envelope, ok := parseCanonicalEnvelope(rec.Body.Bytes()); ok {
		return &dispatchResult{
			Value:    envelope,
			Envelope: envelope,
			IsError:  rec.Code >= 400,
		}, nil
	}

	if rec.Code >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", rec.Code, rec.Body.String())
	}

	var result any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		return &dispatchResult{Value: rec.Body.String()}, nil
	}
	return &dispatchResult{Value: result}, nil
}

type route struct {
	method string
	path   string
	body   any
	query  map[string]string
}

func str(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func intOr(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return def
	}
}

func boolOr(args map[string]any, key string, def bool) bool {
	v, ok := args[key].(bool)
	if !ok {
		return def
	}
	return v
}

func stringSlice(args map[string]any, key string) []any {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if ok {
		return values
	}
	stringValues, ok := raw.([]string)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(stringValues))
	for _, value := range stringValues {
		result = append(result, value)
	}
	return result
}

func firstString(values []any) string {
	if len(values) == 0 {
		return ""
	}
	value, _ := values[0].(string)
	return value
}

func normalizeQualifiedIdentifier(value string) string {
	if head, tail, ok := strings.Cut(value, ":"); ok && head != "" && tail != "" {
		return tail
	}
	return value
}

func resolveEntityBody(args map[string]any) map[string]any {
	body := map[string]any{}

	if name := str(args, "name"); name != "" {
		body["name"] = name
	} else if query := str(args, "query"); query != "" {
		body["name"] = query
	}
	if kind := str(args, "type"); kind != "" {
		body["type"] = kind
	} else if kinds := stringSlice(args, "types"); len(kinds) > 0 {
		body["type"] = firstString(kinds)
	}
	if repoID := str(args, "repo_id"); repoID != "" {
		body["repo_id"] = repoID
	}

	return body
}

func contentSearchBody(args map[string]any) map[string]any {
	body := map[string]any{
		"query":  str(args, "query"),
		"limit":  intOr(args, "limit", 10),
		"offset": intOr(args, "offset", 0),
	}
	if body["query"] == "" {
		body["query"] = str(args, "pattern")
	}

	repoIDs := stringSlice(args, "repo_ids")
	switch len(repoIDs) {
	case 0:
		if repoID := str(args, "repo_id"); repoID != "" {
			body["repo_id"] = repoID
		}
	case 1:
		if repoID := firstString(repoIDs); repoID != "" {
			body["repo_id"] = repoID
		}
	default:
		body["repo_ids"] = repoIDs
	}

	return body
}

func parseMaxDepth(args map[string]any, defaultDepth int) int {
	if depth, ok := args["max_depth"].(float64); ok {
		return int(depth)
	}
	contextValue := str(args, "context")
	if contextValue == "" {
		return defaultDepth
	}
	depth, err := strconv.Atoi(strings.TrimSpace(contextValue))
	if err != nil {
		return defaultDepth
	}
	return depth
}

// resolveRoute maps a tool name and its arguments to an internal HTTP route.
func resolveRoute(toolName string, args map[string]any) (*route, error) {
	switch toolName {
	// ── Code ──
	case "find_code":
		return &route{method: "POST", path: "/api/v0/code/search", body: map[string]any{
			"query": str(args, "query"), "repo_id": str(args, "repo_id"),
			"limit": intOr(args, "limit", 10), "exact": boolOr(args, "exact", false),
		}}, nil
	case "find_symbol":
		return &route{method: "POST", path: "/api/v0/code/symbols/search", body: map[string]any{
			"symbol":       str(args, "symbol"),
			"match_mode":   str(args, "match_mode"),
			"repo_id":      str(args, "repo_id"),
			"language":     str(args, "language"),
			"entity_type":  str(args, "entity_type"),
			"entity_types": stringSlice(args, "entity_types"),
			"limit":        intOr(args, "limit", 25),
			"offset":       intOr(args, "offset", 0),
		}}, nil
	case "get_code_relationship_story":
		return &route{method: "POST", path: "/api/v0/code/relationships/story", body: map[string]any{
			"target":             str(args, "target"),
			"entity_id":          str(args, "entity_id"),
			"repo_id":            str(args, "repo_id"),
			"language":           str(args, "language"),
			"relationship_type":  str(args, "relationship_type"),
			"direction":          str(args, "direction"),
			"include_transitive": boolOr(args, "include_transitive", false),
			"max_depth":          intOr(args, "max_depth", 5),
			"limit":              intOr(args, "limit", 25),
			"offset":             intOr(args, "offset", 0),
		}}, nil
	case "analyze_code_relationships":
		body := map[string]any{
			"entity_id":  str(args, "target"),
			"query_type": str(args, "query_type"),
		}
		switch str(args, "query_type") {
		case "find_callers":
			body = map[string]any{
				"name":              str(args, "target"),
				"direction":         "incoming",
				"relationship_type": "CALLS",
			}
		case "find_callees":
			body = map[string]any{
				"name":              str(args, "target"),
				"direction":         "outgoing",
				"relationship_type": "CALLS",
			}
		case "find_all_callers":
			body = map[string]any{
				"name":              str(args, "target"),
				"direction":         "incoming",
				"relationship_type": "CALLS",
				"transitive":        true,
				"max_depth":         parseMaxDepth(args, 5),
			}
		case "find_all_callees":
			body = map[string]any{
				"name":              str(args, "target"),
				"direction":         "outgoing",
				"relationship_type": "CALLS",
				"transitive":        true,
				"max_depth":         parseMaxDepth(args, 5),
			}
		case "find_importers":
			body = map[string]any{
				"name":              str(args, "target"),
				"direction":         "incoming",
				"relationship_type": "IMPORTS",
			}
		case "call_chain":
			start, end, ok := strings.Cut(str(args, "target"), "->")
			if !ok {
				return nil, fmt.Errorf("call_chain target must use start->end format")
			}
			return &route{method: "POST", path: "/api/v0/code/call-chain", body: map[string]any{
				"start":     strings.TrimSpace(start),
				"end":       strings.TrimSpace(end),
				"max_depth": parseMaxDepth(args, 5),
			}}, nil
		case "dead_code":
			return &route{method: "POST", path: "/api/v0/code/dead-code", body: map[string]any{
				"repo_id":                str(args, "repo_id"),
				"limit":                  intOr(args, "limit", 100),
				"exclude_decorated_with": stringSlice(args, "exclude_decorated_with"),
			}}, nil
		}
		return &route{method: "POST", path: "/api/v0/code/relationships", body: body}, nil
	case "find_dead_code":
		return &route{method: "POST", path: "/api/v0/code/dead-code", body: map[string]any{
			"repo_id":                str(args, "repo_id"),
			"limit":                  intOr(args, "limit", 100),
			"exclude_decorated_with": stringSlice(args, "exclude_decorated_with"),
		}}, nil
	case "find_dead_iac":
		return &route{method: "POST", path: "/api/v0/iac/dead", body: map[string]any{
			"repo_id":           str(args, "repo_id"),
			"repo_ids":          stringSlice(args, "repo_ids"),
			"families":          stringSlice(args, "families"),
			"include_ambiguous": boolOr(args, "include_ambiguous", false),
			"limit":             intOr(args, "limit", 100),
			"offset":            intOr(args, "offset", 0),
		}}, nil
	case "find_unmanaged_resources":
		return &route{method: "POST", path: "/api/v0/iac/unmanaged-resources", body: map[string]any{
			"scope_id":      str(args, "scope_id"),
			"account_id":    str(args, "account_id"),
			"region":        str(args, "region"),
			"finding_kinds": stringSlice(args, "finding_kinds"),
			"limit":         intOr(args, "limit", 100),
			"offset":        intOr(args, "offset", 0),
		}}, nil
	case "calculate_cyclomatic_complexity":
		return &route{method: "POST", path: "/api/v0/code/complexity", body: map[string]any{
			"function_name": str(args, "function_name"), "repo_id": str(args, "repo_id"),
		}}, nil
	case "find_most_complex_functions":
		return &route{method: "POST", path: "/api/v0/code/complexity", body: map[string]any{
			"repo_id": str(args, "repo_id"), "limit": intOr(args, "limit", 10),
		}}, nil
	case "execute_language_query":
		return &route{method: "POST", path: "/api/v0/code/language-query", body: map[string]any{
			"language": str(args, "language"), "entity_type": str(args, "entity_type"),
			"query": str(args, "query"), "repo_id": str(args, "repo_id"),
			"limit": intOr(args, "limit", 50),
		}}, nil
	case "find_function_call_chain":
		return &route{method: "POST", path: "/api/v0/code/call-chain", body: map[string]any{
			"start": str(args, "start"), "end": str(args, "end"),
			"max_depth": intOr(args, "max_depth", 5),
		}}, nil
	case "execute_cypher_query":
		return &route{method: "POST", path: "/api/v0/code/cypher", body: map[string]any{
			"cypher_query": str(args, "cypher_query"),
			"limit":        intOr(args, "limit", 100),
		}}, nil
	case "visualize_graph_query":
		return &route{method: "POST", path: "/api/v0/code/visualize", body: map[string]any{
			"cypher_query": str(args, "cypher_query"),
		}}, nil
	case "search_registry_bundles":
		return &route{method: "POST", path: "/api/v0/code/bundles", body: map[string]any{
			"query":       str(args, "query"),
			"unique_only": boolOr(args, "unique_only", false),
		}}, nil

	// ── Repositories ──
	case "list_indexed_repositories":
		return &route{method: "GET", path: "/api/v0/repositories"}, nil
	case "get_repository_stats":
		repoID := str(args, "repo_id")
		if repoID == "" {
			return &route{method: "GET", path: "/api/v0/repositories"}, nil
		}
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(repoID) + "/stats"}, nil
	case "get_repo_context":
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(str(args, "repo_id")) + "/context"}, nil
	case "get_relationship_evidence":
		return &route{method: "GET", path: "/api/v0/evidence/relationships/" + url.PathEscape(str(args, "resolved_id"))}, nil
	case "list_package_registry_packages":
		return &route{method: "GET", path: "/api/v0/package-registry/packages", query: map[string]string{
			"package_id": str(args, "package_id"),
			"ecosystem":  str(args, "ecosystem"),
			"name":       str(args, "name"),
			"limit":      strconv.Itoa(intOr(args, "limit", 50)),
		}}, nil
	case "list_package_registry_versions":
		return &route{method: "GET", path: "/api/v0/package-registry/versions", query: map[string]string{
			"package_id": str(args, "package_id"),
			"limit":      strconv.Itoa(intOr(args, "limit", 50)),
		}}, nil
	case "get_repo_story":
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(str(args, "repo_id")) + "/story"}, nil
	case "get_repo_summary":
		// repo_summary uses repo_name, map to context endpoint
		name := str(args, "repo_name")
		if name == "" {
			name = str(args, "repo_id")
		}
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(name) + "/context"}, nil
	case "get_repository_coverage":
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(str(args, "repo_id")) + "/coverage"}, nil

	// ── Entities ──
	case "resolve_entity":
		return &route{method: "POST", path: "/api/v0/entities/resolve", body: resolveEntityBody(args)}, nil
	case "get_entity_context":
		q := map[string]string{}
		if env := str(args, "environment"); env != "" {
			q["environment"] = env
		}
		return &route{method: "GET", path: "/api/v0/entities/" + url.PathEscape(str(args, "entity_id")) + "/context", query: q}, nil
	case "get_workload_context":
		q := map[string]string{}
		if env := str(args, "environment"); env != "" {
			q["environment"] = env
		}
		return &route{method: "GET", path: "/api/v0/workloads/" + url.PathEscape(str(args, "workload_id")) + "/context", query: q}, nil
	case "get_workload_story":
		q := map[string]string{}
		if env := str(args, "environment"); env != "" {
			q["environment"] = env
		}
		return &route{method: "GET", path: "/api/v0/workloads/" + url.PathEscape(str(args, "workload_id")) + "/story", query: q}, nil
	case "get_service_context":
		q := map[string]string{}
		if env := str(args, "environment"); env != "" {
			q["environment"] = env
		}
		return &route{method: "GET", path: "/api/v0/services/" + url.PathEscape(normalizeQualifiedIdentifier(str(args, "workload_id"))) + "/context", query: q}, nil
	case "get_service_story":
		q := map[string]string{}
		if env := str(args, "environment"); env != "" {
			q["environment"] = env
		}
		return &route{method: "GET", path: "/api/v0/services/" + url.PathEscape(normalizeQualifiedIdentifier(str(args, "workload_id"))) + "/story", query: q}, nil
	case "investigate_service":
		return &route{method: "GET", path: "/api/v0/investigations/services/" + url.PathEscape(normalizeQualifiedIdentifier(str(args, "service_name"))), query: map[string]string{
			"environment": str(args, "environment"),
			"intent":      str(args, "intent"),
			"question":    str(args, "question"),
		}}, nil

	// ── Content ──
	case "get_file_content":
		return &route{method: "POST", path: "/api/v0/content/files/read", body: map[string]any{
			"repo_id": str(args, "repo_id"), "relative_path": str(args, "relative_path"),
		}}, nil
	case "get_file_lines":
		return &route{method: "POST", path: "/api/v0/content/files/lines", body: args}, nil
	case "get_entity_content":
		return &route{method: "POST", path: "/api/v0/content/entities/read", body: map[string]any{
			"entity_id": str(args, "entity_id"),
		}}, nil
	case "search_file_content":
		return &route{method: "POST", path: "/api/v0/content/files/search", body: contentSearchBody(args)}, nil
	case "search_entity_content":
		return &route{method: "POST", path: "/api/v0/content/entities/search", body: contentSearchBody(args)}, nil

	// ── Infra ──
	case "find_infra_resources":
		return &route{method: "POST", path: "/api/v0/infra/resources/search", body: map[string]any{
			"query":    str(args, "query"),
			"category": str(args, "category"),
			"limit":    intOr(args, "limit", 50),
		}}, nil
	case "analyze_infra_relationships":
		return &route{method: "POST", path: "/api/v0/infra/relationships", body: map[string]any{
			"entity_id": str(args, "target"), "relationship_type": str(args, "query_type"),
		}}, nil
	case "get_ecosystem_overview":
		return &route{method: "GET", path: "/api/v0/ecosystem/overview"}, nil

	// ── Impact ──
	case "trace_deployment_chain":
		return &route{method: "POST", path: "/api/v0/impact/trace-deployment-chain", body: map[string]any{
			"service_name":                 str(args, "service_name"),
			"direct_only":                  boolOr(args, "direct_only", true),
			"max_depth":                    intOr(args, "max_depth", 8),
			"include_related_module_usage": boolOr(args, "include_related_module_usage", false),
		}}, nil
	case "find_blast_radius":
		return &route{method: "POST", path: "/api/v0/impact/blast-radius", body: args}, nil
	case "find_change_surface":
		return &route{method: "POST", path: "/api/v0/impact/change-surface", body: args}, nil
	case "trace_resource_to_code":
		return &route{method: "POST", path: "/api/v0/impact/trace-resource-to-code", body: args}, nil
	case "explain_dependency_path":
		return &route{method: "POST", path: "/api/v0/impact/explain-dependency-path", body: args}, nil

	// ── Compare ──
	case "compare_environments":
		return &route{method: "POST", path: "/api/v0/compare/environments", body: args}, nil

	// ── Status ──
	case "list_ingesters":
		return &route{method: "GET", path: "/api/v0/status/ingesters"}, nil
	case "get_ingester_status":
		ingester := str(args, "ingester")
		if ingester == "" {
			ingester = "repository"
		}
		return &route{method: "GET", path: "/api/v0/status/ingesters/" + url.PathEscape(ingester)}, nil
	case "get_index_status":
		return &route{method: "GET", path: "/api/v0/index-status"}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}
