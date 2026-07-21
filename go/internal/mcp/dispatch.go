// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
)

// dispatchTool routes an MCP tool call to the appropriate internal HTTP endpoint.
func dispatchTool(ctx context.Context, handler http.Handler, toolName string, args map[string]any, authHeader string, logger *slog.Logger) (*dispatchResult, error) {
	return dispatchToolWithOptions(ctx, handler, toolName, args, authHeader, logger, dispatchOptions{
		timeout:            defaultToolDispatchTimeout,
		responseByteBudget: defaultToolResponseByteBudget,
	})
}

func dispatchToolWithOptions(
	ctx context.Context,
	handler http.Handler,
	toolName string,
	args map[string]any,
	authHeader string,
	logger *slog.Logger,
	options dispatchOptions,
) (*dispatchResult, error) {
	route, err := resolveRoute(toolName, args)
	if err != nil {
		return nil, err
	}

	timeout := options.timeout
	if timeout <= 0 {
		timeout = defaultToolDispatchTimeout
	}
	dispatchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	logger.Debug("dispatch tool", "tool", toolName, "method", route.method, "path", route.path)

	var body io.Reader
	if route.body != nil {
		encoded, err := json.Marshal(route.body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(dispatchCtx, route.method, route.path, body)
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

	if err := dispatchCtx.Err(); err != nil {
		return nil, dispatchContextError(toolName, timeout, err, logger)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if err := dispatchCtx.Err(); err != nil {
		return nil, dispatchContextError(toolName, timeout, err, logger)
	}

	if envelope, ok := parseCanonicalEnvelope(rec.Body.Bytes()); ok {
		return applyResponseBudget(&dispatchResult{
			Value:    envelope,
			Envelope: envelope,
			IsError:  rec.Code >= 400,
		}, toolName, options.responseByteBudget, logger), nil
	}

	if rec.Code >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", rec.Code, rec.Body.String())
	}

	var result any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		return applyResponseBudget(&dispatchResult{Value: rec.Body.String()}, toolName, options.responseByteBudget, logger), nil
	}
	return applyResponseBudget(&dispatchResult{Value: result}, toolName, options.responseByteBudget, logger), nil
}

type route struct {
	method string
	path   string
	body   any
	query  map[string]string
}

// resolveRoute maps a tool name and its arguments to an internal HTTP route.
func resolveRoute(toolName string, args map[string]any) (*route, error) {
	if route, ok := documentationRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok := cloudInventoryRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok := cloudRuntimeDriftRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok := semanticEvidenceRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok := semanticSearchRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok, err := repositoryRoute(toolName, args); ok {
		return route, err
	}
	if route, ok, err := relationshipEdgesRoute(toolName, args); ok {
		return route, err
	}
	if route, ok, err := repositoryFilesRoute(toolName, args); ok {
		return route, err
	}
	if route, ok := ecosystemRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok := compareRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok := freshnessRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok := queryPlaybookRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok := investigationWorkflowRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok := investigationPacketRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok := visualizationRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok, err := statusRoute(toolName, args); ok {
		return route, err
	}
	if route, ok := askRoute(toolName, args); ok {
		return route, nil
	}
	if route, ok := codeFlowRoute(toolName, args); ok {
		return route, nil
	}
	switch toolName {
	// ── Code ──
	case "find_code":
		return &route{method: "POST", path: "/api/v0/code/search", body: map[string]any{
			"query": str(args, "query"), "repo_id": str(args, "repo_id"),
			"language": str(args, "language"), "limit": intOr(args, "limit", 10),
			"exact": boolOr(args, "exact", false),
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
	case "inspect_code_inventory":
		return &route{method: "POST", path: "/api/v0/code/structure/inventory", body: map[string]any{
			"repo_id":        str(args, "repo_id"),
			"language":       str(args, "language"),
			"inventory_kind": str(args, "inventory_kind"),
			"entity_kind":    str(args, "entity_kind"),
			"file_path":      str(args, "file_path"),
			"symbol":         str(args, "symbol"),
			"decorator":      str(args, "decorator"),
			"method_name":    str(args, "method_name"),
			"class_name":     str(args, "class_name"),
			"limit":          intOr(args, "limit", 25),
			"offset":         intOr(args, "offset", 0),
		}}, nil
	case "investigate_import_dependencies":
		return &route{method: "POST", path: "/api/v0/code/imports/investigate", body: map[string]any{
			"query_type":    str(args, "query_type"),
			"repo_id":       str(args, "repo_id"),
			"language":      str(args, "language"),
			"source_file":   str(args, "source_file"),
			"target_file":   str(args, "target_file"),
			"source_module": str(args, "source_module"),
			"target_module": str(args, "target_module"),
			"limit":         intOr(args, "limit", 25),
			"offset":        intOr(args, "offset", 0),
		}}, nil
	case "inspect_call_graph_metrics":
		return &route{method: "POST", path: "/api/v0/code/call-graph/metrics", body: map[string]any{
			"metric_type": str(args, "metric_type"),
			"repo_id":     str(args, "repo_id"),
			"language":    str(args, "language"),
			"limit":       intOr(args, "limit", 25),
			"offset":      intOr(args, "offset", 0),
		}}, nil
	case "trace_route_callers":
		return &route{method: "POST", path: "/api/v0/code/routes/callers", body: map[string]any{
			"repo_id":      str(args, "repo_id"),
			"service_id":   str(args, "service_id"),
			"service_name": str(args, "service_name"),
			"method":       str(args, "method"),
			"path":         str(args, "path"),
			"max_depth":    intOr(args, "max_depth", 2),
			"limit":        intOr(args, "limit", 25),
		}}, nil
	case "investigate_code_topic":
		return &route{method: "POST", path: "/api/v0/code/topics/investigate", body: map[string]any{
			"topic":    str(args, "topic"),
			"intent":   str(args, "intent"),
			"repo_id":  str(args, "repo_id"),
			"language": str(args, "language"),
			"limit":    intOr(args, "limit", 25),
			"offset":   intOr(args, "offset", 0),
		}}, nil
	case "investigate_hardcoded_secrets":
		return &route{method: "POST", path: "/api/v0/code/security/secrets/investigate", body: map[string]any{
			"repo_id":            str(args, "repo_id"),
			"language":           str(args, "language"),
			"finding_kinds":      stringSlice(args, "finding_kinds"),
			"include_suppressed": boolOr(args, "include_suppressed", false),
			"limit":              intOr(args, "limit", 25),
			"offset":             intOr(args, "offset", 0),
		}}, nil
	case "get_code_relationship_story":
		return codeRelationshipStoryRoute(args), nil
	case "analyze_code_relationships":
		return resolveAnalyzeCodeRelationshipsRoute(args)
	case "find_dead_code":
		return &route{method: "POST", path: "/api/v0/code/dead-code", body: map[string]any{
			"repo_id":                str(args, "repo_id"),
			"limit":                  intOr(args, "limit", 100),
			"exclude_decorated_with": stringSlice(args, "exclude_decorated_with"),
		}}, nil
	case "investigate_dead_code":
		return &route{method: "POST", path: "/api/v0/code/dead-code/investigate", body: map[string]any{
			"repo_id":                str(args, "repo_id"),
			"language":               str(args, "language"),
			"limit":                  intOr(args, "limit", 100),
			"offset":                 intOr(args, "offset", 0),
			"exclude_decorated_with": stringSlice(args, "exclude_decorated_with"),
		}}, nil
	case "find_cross_repo_dead_code":
		return &route{method: "POST", path: "/api/v0/code/dead-code/cross-repo", body: map[string]any{
			"repo_id":                str(args, "repo_id"),
			"consumer_repo_ids":      stringValues(args, "consumer_repo_ids"),
			"language":               str(args, "language"),
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
	case "get_iac_management_status":
		return &route{method: "POST", path: "/api/v0/iac/management-status", body: iacManagementStatusBody(args)}, nil
	case "explain_iac_management_status":
		return &route{method: "POST", path: "/api/v0/iac/management-status/explain", body: iacManagementStatusBody(args)}, nil
	case "propose_terraform_import_plan":
		return &route{method: "POST", path: "/api/v0/iac/terraform-import-plan/candidates", body: terraformImportPlanBody(args)}, nil
	case "compose_replatforming_plan":
		return &route{method: "POST", path: "/api/v0/replatforming/plans", body: replatformingPlanBody(args)}, nil
	case "list_aws_runtime_drift_findings":
		return &route{method: "POST", path: "/api/v0/aws/runtime-drift/findings", body: awsRuntimeDriftFindingsBody(args)}, nil
	case "list_terraform_config_state_drift_findings":
		return &route{method: "POST", path: "/api/v0/terraform/config-state-drift/findings", body: terraformConfigStateDriftFindingsBody(args)}, nil
	case "get_replatforming_rollups":
		return &route{method: "POST", path: "/api/v0/replatforming/rollups", body: replatformingRollupsBody(args)}, nil
	case "find_unmanaged_resource_owners":
		return &route{method: "POST", path: "/api/v0/replatforming/ownership-packets", body: replatformingOwnershipBody(args)}, nil
	case "calculate_cyclomatic_complexity":
		body := map[string]any{
			"function_name": str(args, "function_name"),
			"repo_id":       str(args, "repo_id"),
		}
		if entityID := str(args, "entity_id"); entityID != "" {
			body["entity_id"] = entityID
		}
		return &route{method: "POST", path: "/api/v0/code/complexity", body: body}, nil
	case "find_most_complex_functions":
		return &route{method: "POST", path: "/api/v0/code/complexity", body: map[string]any{
			"repo_id": str(args, "repo_id"), "limit": intOr(args, "limit", 10),
		}}, nil
	case "inspect_code_quality":
		return &route{method: "POST", path: "/api/v0/code/quality/inspect", body: map[string]any{
			"check":          str(args, "check"),
			"repo_id":        str(args, "repo_id"),
			"language":       str(args, "language"),
			"entity_id":      str(args, "entity_id"),
			"function_name":  str(args, "function_name"),
			"min_complexity": intOr(args, "min_complexity", 0),
			"min_lines":      intOr(args, "min_lines", 0),
			"min_arguments":  intOr(args, "min_arguments", 0),
			"limit":          intOr(args, "limit", 10),
			"offset":         intOr(args, "offset", 0),
		}}, nil
	case "execute_language_query":
		return &route{method: "POST", path: "/api/v0/code/language-query", body: map[string]any{
			"language": str(args, "language"), "entity_type": str(args, "entity_type"),
			"query": str(args, "query"), "repo_id": str(args, "repo_id"),
			"limit": intOr(args, "limit", 50),
		}}, nil
	case "find_function_call_chain":
		return &route{method: "POST", path: "/api/v0/code/call-chain", body: map[string]any{
			"start":           str(args, "start"),
			"end":             str(args, "end"),
			"repo_id":         str(args, "repo_id"),
			"cross_repo":      boolOr(args, "cross_repo", false),
			"start_repo_id":   str(args, "start_repo_id"),
			"end_repo_id":     str(args, "end_repo_id"),
			"start_entity_id": str(args, "start_entity_id"),
			"end_entity_id":   str(args, "end_entity_id"),
			"max_depth":       intOr(args, "max_depth", 5),
		}}, nil
	case "execute_cypher_query":
		return &route{method: "POST", path: "/api/v0/code/cypher", body: map[string]any{
			"cypher_query": str(args, "cypher_query"),
			"limit":        intOr(args, "limit", 100),
		}}, nil
	case "visualize_graph_query":
		return &route{method: "POST", path: "/api/v0/code/visualize", body: map[string]any{
			"cypher_query": str(args, "cypher_query"),
			"limit":        intOr(args, "limit", 100),
		}}, nil
	case "search_registry_bundles":
		return &route{method: "POST", path: "/api/v0/code/bundles", body: map[string]any{
			"query":       str(args, "query"),
			"ecosystem":   str(args, "ecosystem"),
			"unique_only": boolOr(args, "unique_only", false),
			"limit":       intOr(args, "limit", 50),
		}}, nil

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
		return serviceContextRoute(args)
	case "get_service_story":
		return serviceStoryRoute(args)
	case "get_service_intelligence_report":
		return serviceIntelligenceReportRoute(args)
	case "investigate_service":
		q := map[string]string{
			"environment": str(args, "environment"),
			"intent":      str(args, "intent"),
			"question":    str(args, "question"),
		}
		if serviceID := canonicalWorkloadIdentifier(str(args, "service_name")); serviceID != "" {
			q["service_id"] = serviceID
		}
		if repo := serviceStoryRepositorySelector(args); repo != "" {
			q["repo"] = repo
		}
		return &route{method: "GET", path: "/api/v0/investigations/services/" + url.PathEscape(normalizeQualifiedIdentifier(str(args, "service_name"))), query: q}, nil
	case "get_incident_context":
		incidentID := str(args, "provider_incident_id")
		if incidentID == "" {
			incidentID = str(args, "incident_id")
		}
		return &route{method: "GET", path: "/api/v0/incidents/" + url.PathEscape(incidentID) + "/context", query: map[string]string{
			"provider":   str(args, "provider"),
			"scope_id":   str(args, "scope_id"),
			"service_id": str(args, "service_id"),
			"since":      str(args, "since"),
			"until":      str(args, "until"),
			"limit":      intString(args, "limit", 25),
		}}, nil
	case "list_work_item_evidence":
		return &route{method: "GET", path: "/api/v0/work-items/evidence", query: map[string]string{
			"scope_id":              str(args, "scope_id"),
			"project_key":           str(args, "project_key"),
			"work_item_key":         str(args, "work_item_key"),
			"provider_work_item_id": str(args, "provider_work_item_id"),
			"external_url":          str(args, "external_url"),
			"url_fingerprint":       str(args, "url_fingerprint"),
			"observed_after":        str(args, "observed_after"),
			"after_fact_id":         str(args, "after_fact_id"),
			"limit":                 intString(args, "limit", 25),
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
	case "build_evidence_citation_packet":
		return &route{method: "POST", path: "/api/v0/evidence/citations", body: map[string]any{
			"subject":  args["subject"],
			"question": str(args, "question"),
			"handles":  args["handles"],
			"limit":    intOr(args, "limit", 10),
		}}, nil
	case "search_file_content":
		return &route{method: "POST", path: "/api/v0/content/files/search", body: contentSearchBody(args)}, nil
	case "search_entity_content":
		return &route{method: "POST", path: "/api/v0/content/entities/search", body: contentSearchBody(args)}, nil

	// ── Infra ──
	case "find_infra_resources":
		return infraResourceSearchRoute(args), nil
	case "count_infra_resources":
		return infraResourceAggregateCountRoute(args), nil
	case "get_infra_resource_inventory":
		return infraResourceAggregateInventoryRoute(args), nil
	case "investigate_resource":
		return &route{method: "POST", path: "/api/v0/impact/resource-investigation", body: map[string]any{
			"query":         str(args, "query"),
			"resource_id":   str(args, "resource_id"),
			"resource_type": str(args, "resource_type"),
			"environment":   str(args, "environment"),
			"max_depth":     intOr(args, "max_depth", 4),
			"limit":         intOr(args, "limit", 25),
		}}, nil
	case "analyze_infra_relationships":
		return &route{method: "POST", path: "/api/v0/infra/relationships", body: map[string]any{
			"entity_id": str(args, "target"), "relationship_type": str(args, "query_type"),
		}}, nil

	default:
		// Impact tools are dispatched from dispatch_impact.go to keep this
		// file within the 500-line cap.
		if r, ok, _ := impactRoute(toolName, args); ok {
			return r, nil
		}
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}
