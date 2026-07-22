// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/eshu-hq/eshu/go/internal/query"
)

const selectorPrefix = "{{selector:"

func selector(name string) string { return selectorPrefix + name + "}}" }

func buildCurrentProbeRegistry() ([]graphReadProbe, error) {
	targets, err := currentAPIAndMCPSurfaces()
	if err != nil {
		return nil, err
	}
	tools := make(map[string]mcp.ToolDefinition, len(mcp.ReadOnlyTools()))
	for _, tool := range mcp.ReadOnlyTools() {
		tools[tool.Name] = tool
	}
	registry := make([]graphReadProbe, 0, len(targets))
	for _, identity := range targets {
		if strings.HasPrefix(identity, "api:") {
			probe, err := buildAPIProbe(identity)
			if err != nil {
				return nil, err
			}
			registry = append(registry, probe)
			continue
		}
		name := strings.TrimPrefix(identity, "mcp:")
		tool, ok := tools[name]
		if !ok {
			return nil, fmt.Errorf("current MCP surface %q has no tool definition", name)
		}
		execute, reason := classifyMCPExecution(name)
		registry = append(registry, graphReadProbe{
			identity: identity, name: "mcp_" + name, transport: "mcp", tool: name,
			arguments: synthesizeToolArguments(tool.InputSchema), auth: mcpProbeAuth(name), execute: execute, unsafeReason: reason,
		})
	}
	if err := enrichAPIProbeFixtures(registry); err != nil {
		return nil, err
	}
	applyMappedDeltaFixtures(registry)
	sort.Slice(registry, func(left, right int) bool { return registry[left].identity < registry[right].identity })
	return registry, nil
}

func enrichAPIProbeFixtures(registry []graphReadProbe) error {
	var document map[string]any
	if err := json.Unmarshal([]byte(query.OpenAPISpec()), &document); err != nil {
		return fmt.Errorf("parse current OpenAPI fixtures: %w", err)
	}
	paths, _ := document["paths"].(map[string]any)
	for index := range registry {
		probe := &registry[index]
		if probe.transport != "api" || !probe.execute {
			continue
		}
		operationText := strings.TrimPrefix(probe.identity, "api:")
		parts := strings.SplitN(operationText, " ", 2)
		if len(parts) != 2 {
			continue
		}
		pathItem, _ := paths[parts[1]].(map[string]any)
		operation, _ := pathItem[strings.ToLower(parts[0])].(map[string]any)
		if operation == nil {
			continue
		}
		parameters := append(openAPIObjectSlice(pathItem["parameters"]), openAPIObjectSlice(operation["parameters"])...)
		for _, parameter := range parameters {
			name, _ := parameter["name"].(string)
			location, _ := parameter["in"].(string)
			required, _ := parameter["required"].(bool)
			if !required && name != "limit" && name != "offset" {
				continue
			}
			value := synthesizeOpenAPISchemaValue(document, name, parameter["schema"])
			if location == "query" {
				if probe.query == nil {
					probe.query = map[string]any{}
				}
				probe.query[name] = value
			}
		}
		requestBody, _ := operation["requestBody"].(map[string]any)
		content, _ := requestBody["content"].(map[string]any)
		media, _ := content["application/json"].(map[string]any)
		if schema := media["schema"]; schema != nil {
			if body, ok := synthesizeOpenAPISchemaValue(document, "body", schema).(map[string]any); ok {
				probe.arguments = body
			}
		}
	}
	return nil
}

func openAPIObjectSlice(value any) []map[string]any {
	raw, _ := value.([]any)
	objects := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if object, ok := item.(map[string]any); ok {
			objects = append(objects, object)
		}
	}
	return objects
}

func synthesizeOpenAPISchemaValue(document map[string]any, name string, raw any) any {
	schema := resolveOpenAPISchema(document, raw)
	if schema == nil {
		return synthesizeSchemaValue(name, nil)
	}
	if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
		return values[0]
	}
	if value, ok := schema["default"]; ok {
		return value
	}
	if variants, ok := schema["allOf"].([]any); ok {
		merged := map[string]any{}
		for _, variant := range variants {
			if object, ok := synthesizeOpenAPISchemaValue(document, name, variant).(map[string]any); ok {
				for key, value := range object {
					merged[key] = value
				}
			}
		}
		return merged
	}
	if variants, ok := schema["oneOf"].([]any); ok && len(variants) > 0 {
		return synthesizeOpenAPISchemaValue(document, name, variants[0])
	}
	if variants, ok := schema["anyOf"].([]any); ok && len(variants) > 0 {
		return synthesizeOpenAPISchemaValue(document, name, mergeOpenAPISchemaVariant(document, schema, variants[0]))
	}
	switch schema["type"] {
	case "object", nil:
		properties, _ := schema["properties"].(map[string]any)
		required := stringSlice(schema["required"])
		object := make(map[string]any, len(required))
		for _, propertyName := range required {
			object[propertyName] = synthesizeOpenAPISchemaValue(document, propertyName, properties[propertyName])
		}
		return object
	case "array":
		return []any{synthesizeOpenAPISchemaValue(document, name, schema["items"])}
	default:
		return synthesizeSchemaValue(name, schema)
	}
}

func mergeOpenAPISchemaVariant(document map[string]any, parent map[string]any, rawVariant any) map[string]any {
	merged := make(map[string]any, len(parent))
	for key, value := range parent {
		if key != "anyOf" {
			merged[key] = value
		}
	}
	variant := resolveOpenAPISchema(document, rawVariant)
	for key, value := range variant {
		if key != "properties" && key != "required" {
			merged[key] = value
		}
	}
	properties := map[string]any{}
	for key, value := range openAPIObject(parent["properties"]) {
		properties[key] = value
	}
	for key, value := range openAPIObject(variant["properties"]) {
		properties[key] = value
	}
	if len(properties) > 0 {
		merged["properties"] = properties
	}
	merged["required"] = appendUniqueStrings(stringSlice(parent["required"]), stringSlice(variant["required"])...)
	return merged
}

func openAPIObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if _, ok := seen[value]; !ok {
			values = append(values, value)
			seen[value] = struct{}{}
		}
	}
	return values
}

func resolveOpenAPISchema(document map[string]any, raw any) map[string]any {
	schema, _ := raw.(map[string]any)
	reference, _ := schema["$ref"].(string)
	if !strings.HasPrefix(reference, "#/") {
		return schema
	}
	current := any(document)
	for _, part := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
		object, ok := current.(map[string]any)
		if !ok {
			return schema
		}
		current = object[part]
	}
	resolved, _ := current.(map[string]any)
	return resolved
}

func buildAPIProbe(identity string) (graphReadProbe, error) {
	operation := strings.TrimPrefix(identity, "api:")
	parts := strings.SplitN(operation, " ", 2)
	if len(parts) != 2 {
		return graphReadProbe{}, fmt.Errorf("invalid API surface identity %q", identity)
	}
	method, path := parts[0], parts[1]
	execute, reason := classifyAPIExecution(method, path)
	probe := graphReadProbe{
		identity: identity, name: "api_" + strings.ToLower(method) + "_" + sanitizeProbeName(path),
		transport: "api", method: method, path: path, auth: apiProbeAuth(path),
		execute: execute, unsafeReason: reason,
	}
	if execute {
		for _, placeholder := range pathPlaceholders(path) {
			probe.path = strings.ReplaceAll(probe.path, "{"+placeholder+"}", selector(selectorClass(placeholder)))
		}
	}
	return probe, nil
}

func sanitizeProbeName(path string) string {
	return strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(strings.TrimPrefix(path, "/"))
}

func classifyAPIExecution(method, path string) (bool, string) {
	if strings.Contains(path, "{incident_id}") {
		return false, "incident context needs a caller-selected provider incident identifier; no bounded discovery route exists"
	}
	if method == http.MethodDelete || method == http.MethodPatch || method == http.MethodPut {
		return false, strings.ToLower(method) + " route mutates persisted authentication or platform state"
	}
	if strings.HasPrefix(path, "/api/v0/auth/") {
		if path == "/api/v0/auth/github/login" || path == "/api/v0/auth/github/callback" || path == "/api/v0/auth/setup-state" {
			return true, ""
		}
		return false, "authentication route creates or mutates credentials, sessions, users, or provider configuration"
	}
	if method == http.MethodPost && strings.HasPrefix(path, "/api/v0/admin/") && !strings.HasSuffix(path, "/query") {
		return false, "admin route may mutate queue, generation, replay, backfill, or reindex state"
	}
	return true, ""
}

func classifyMCPExecution(name string) (bool, string) {
	if name == "get_incident_context" {
		return false, "incident context needs a caller-selected provider incident identifier; no bounded discovery tool exists"
	}
	return true, ""
}

func apiProbeAuth(path string) graphReadProbeAuth {
	if path == "/health" || path == "/api/v0/openapi.json" || path == "/api/v0/docs" || path == "/api/v0/redoc" ||
		path == "/api/v0/auth/github/login" || path == "/api/v0/auth/github/callback" || path == "/api/v0/auth/setup-state" {
		return "public"
	}
	if strings.HasPrefix(path, "/api/v0/admin/") || path == "/api/v0/code/cypher" || path == "/api/v0/code/visualize" {
		return graphReadProbeAdminAuth
	}
	return graphReadProbeUserAuth
}

func mcpProbeAuth(name string) graphReadProbeAuth {
	if name == "execute_cypher_query" || name == "visualize_graph_query" {
		return graphReadProbeAdminAuth
	}
	return graphReadProbeUserAuth
}

func synthesizeToolArguments(inputSchema any) map[string]any {
	schema, _ := inputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	required := stringSlice(schema["required"])
	arguments := make(map[string]any, len(required)+2)
	for _, name := range required {
		arguments[name] = synthesizeSchemaValue(name, properties[name])
	}
	for _, name := range []string{"limit", "offset"} {
		if property, ok := properties[name]; ok {
			arguments[name] = synthesizeSchemaValue(name, property)
		}
	}
	return arguments
}

func synthesizeSchemaValue(name string, raw any) any {
	schema, _ := raw.(map[string]any)
	if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
		return values[0]
	}
	if value, ok := schema["default"]; ok {
		return value
	}
	switch schema["type"] {
	case "integer", "number":
		if minimum, ok := schema["minimum"]; ok {
			return minimum
		}
		return 1
	case "boolean":
		return false
	case "array":
		return []any{synthesizeSchemaValue(name, schema["items"])}
	case "object":
		return synthesizeToolArguments(schema)
	}
	switch name {
	case "cypher_query":
		return graphReadProbeCypher
	case "question", "query", "search_term", "topic", "pattern":
		return "probe"
	case "language":
		return "go"
	case "domain":
		return "reducer"
	case "entity_type":
		return "function"
	case "ingester":
		return "repository"
	case "query_type":
		return "find_callers"
	case "verb":
		return "DEPENDS_ON"
	case "view":
		return "service_story"
	case "left":
		return "production"
	case "right":
		return "staging"
	}
	return selector(selectorClass(name))
}

func selectorClass(name string) string {
	aliases := map[string]string{
		"repo_id": "repository_id", "repository_id": "repository_id", "repo_name": "repository_name",
		"provider_repo_id": "repository_id",
		"service_id":       "service_id", "service_name": "service_name", "workload_id": "workload_id",
		"entity_id": "entity_id", "target": "entity_id", "from": "entity_id", "resource_id": "cloud_resource_id",
		"relative_path": "relative_path", "scope_id": "scope_id", "generation_id": "generation_id",
		"package_id": "package_id", "fact_kind": "fact_kind", "family": "collector_family",
		"component_id": "component_id", "finding_id": "finding_id", "packet_id": "packet_id",
		"incident_id": "incident_id", "provider_incident_id": "incident_id", "resolved_id": "relationship_id",
		"workflow_id": "workflow_id", "playbook_id": "playbook_id", "terraform_state_scope": "terraform_state_scope",
		"advisory_id": "finding_id", "since_generation_id": "generation_id", "name": "service_name",
		"path": "relative_path", "source": "entity_id", "start": "entity_id", "symbol": "entity_id",
		"changed_paths": "relative_path",
		"ingester":      "collector_family",
	}
	if class, ok := aliases[name]; ok {
		return class
	}
	return name
}

func pathPlaceholders(path string) []string {
	var names []string
	for {
		start := strings.IndexByte(path, '{')
		if start < 0 {
			return names
		}
		end := strings.IndexByte(path[start:], '}')
		if end < 0 {
			return names
		}
		names = append(names, path[start+1:start+end])
		path = path[start+end+1:]
	}
}

func stringSlice(value any) []string {
	if values, ok := value.([]string); ok {
		return values
	}
	raw, _ := value.([]any)
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			values = append(values, text)
		}
	}
	return values
}

func applyMappedDeltaFixtures(registry []graphReadProbe) {
	for index := range registry {
		probe := &registry[index]
		switch probe.identity {
		case "api:GET /api/v0/repositories":
			probe.query = map[string]any{"limit": 1}
		case "api:POST /api/v0/code/cypher", "api:POST /api/v0/code/visualize":
			probe.arguments = map[string]any{"cypher_query": graphReadProbeCypher, "limit": 1}
		case "mcp:list_indexed_repositories":
			probe.arguments = map[string]any{"limit": 1, "offset": 0}
		case "mcp:get_repository_stats":
			probe.arguments = map[string]any{}
		case "mcp:execute_cypher_query", "mcp:visualize_graph_query":
			probe.arguments = map[string]any{"cypher_query": graphReadProbeCypher, "limit": 1}
		case "api:GET /api/v0/auth/github/login":
			probe.query = map[string]any{"provider_config_id": "invalid-provider-config"}
		case "api:GET /api/v0/auth/github/callback":
			probe.query = map[string]any{"code": "invalid", "state": "invalid"}
		case "api:GET /api/v0/codeowners/ownership":
			probe.query = map[string]any{"repository_id": selector("repository_id"), "limit": 1}
		case "api:GET /api/v0/replatforming/selectors":
			probe.query = map[string]any{"limit": 1}
		case "api:POST /api/v0/terraform/config-state-drift/findings":
			probe.arguments = map[string]any{"scope_id": selector("scope_id"), "limit": 1, "offset": 0}
		case "mcp:list_codeowners_ownership":
			probe.arguments = map[string]any{"repository_id": selector("repository_id"), "limit": 1}
		case "mcp:list_terraform_config_state_drift_findings":
			probe.arguments = map[string]any{"scope_id": selector("scope_id"), "limit": 1, "offset": 0}
		}
	}
}
