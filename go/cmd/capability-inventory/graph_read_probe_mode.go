// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const graphReadProbeCypher = "RETURN 1 AS value"

type graphReadProbeAuth string

const (
	graphReadProbeUserAuth  graphReadProbeAuth = "user_token"
	graphReadProbeAdminAuth graphReadProbeAuth = "admin_all_scope"
)

type graphReadProbe struct {
	name      string
	transport string
	method    string
	path      string
	tool      string
	arguments map[string]any
	auth      graphReadProbeAuth
}

var graphReadProbeRegistry = []graphReadProbe{
	{name: "api_repository_inventory", transport: "api", method: http.MethodGet, path: "/api/v0/repositories?limit=1", auth: graphReadProbeUserAuth},
	{name: "api_execute_cypher", transport: "api", method: http.MethodPost, path: "/api/v0/code/cypher", arguments: graphReadQueryArguments(), auth: graphReadProbeAdminAuth},
	{name: "api_visualize_cypher", transport: "api", method: http.MethodPost, path: "/api/v0/code/visualize", arguments: graphReadQueryArguments(), auth: graphReadProbeAdminAuth},
	{name: "mcp_repository_inventory", transport: "mcp", tool: "list_indexed_repositories", arguments: map[string]any{"limit": 1, "offset": 0}, auth: graphReadProbeUserAuth},
	{name: "mcp_repository_stats_inventory", transport: "mcp", tool: "get_repository_stats", arguments: map[string]any{}, auth: graphReadProbeUserAuth},
	{name: "mcp_execute_cypher", transport: "mcp", tool: "execute_cypher_query", arguments: graphReadQueryArguments(), auth: graphReadProbeAdminAuth},
	{name: "mcp_visualize_cypher", transport: "mcp", tool: "visualize_graph_query", arguments: graphReadQueryArguments(), auth: graphReadProbeAdminAuth},
}

func graphReadQueryArguments() map[string]any {
	return map[string]any{"cypher_query": graphReadProbeCypher, "limit": 1}
}

func validateGraphReadProbeRegistry() error {
	routes, err := enumerateAPIRoutes()
	if err != nil {
		return err
	}
	for _, route := range []string{"GET /api/v0/repositories", "POST /api/v0/code/cypher"} {
		if !slices.Contains(routes, route) {
			return fmt.Errorf("graph-read probe registry references unsupported current API surface %q", route)
		}
	}
	tools := enumerateMCPTools()
	for _, tool := range []string{
		"list_indexed_repositories",
		"get_repository_stats",
		"execute_cypher_query",
		"visualize_graph_query",
	} {
		if !slices.Contains(tools, tool) {
			return fmt.Errorf("graph-read probe registry references unsupported current MCP surface %q", tool)
		}
	}
	return nil
}

func currentAPIAndMCPSurfaces() ([]string, error) {
	routes, err := enumerateAPIRoutes()
	if err != nil {
		return nil, err
	}
	targetSet := make(map[string]struct{}, len(routes)+len(enumerateMCPTools())+5)
	for _, route := range routes {
		targetSet["api:"+route] = struct{}{}
	}
	// These routes are registered outside the OpenAPI paths. Keeping them in
	// the executable target manifest prevents the served surface from being
	// silently smaller than the probe inventory.
	for _, route := range []string{
		"GET /health",
		"GET /api/v0/openapi.json",
		"GET /api/v0/docs",
		"GET /api/v0/redoc",
		"POST /api/v0/code/visualize",
	} {
		targetSet["api:"+route] = struct{}{}
	}
	for _, tool := range enumerateMCPTools() {
		targetSet["mcp:"+tool] = struct{}{}
	}
	targets := make([]string, 0, len(targetSet))
	for target := range targetSet {
		targets = append(targets, target)
	}
	slices.Sort(targets)
	return targets, nil
}

func checkGraphReadProbeMode(
	stdout io.Writer,
	apiBaseURL string,
	mcpURL string,
	userTokenEnv string,
	adminTokenEnv string,
) error {
	if err := validateGraphReadProbeRegistry(); err != nil {
		return err
	}
	userToken := strings.TrimSpace(os.Getenv(userTokenEnv))
	adminToken := strings.TrimSpace(os.Getenv(adminTokenEnv))
	if userToken == "" {
		return fmt.Errorf("graph-read probe requires user bearer token in %s", userTokenEnv)
	}
	if adminToken == "" {
		return fmt.Errorf("graph-read probe requires admin/all-scope bearer token in %s", adminTokenEnv)
	}
	return runGraphReadProbe(stdout, &http.Client{Timeout: 20 * time.Second}, apiBaseURL, mcpURL, userToken, adminToken)
}

func runGraphReadProbe(
	stdout io.Writer,
	client *http.Client,
	apiBaseURL string,
	mcpURL string,
	userToken string,
	adminToken string,
) error {
	if client == nil {
		return fmt.Errorf("graph-read probe HTTP client is required")
	}
	if _, err := url.ParseRequestURI(apiBaseURL); err != nil {
		return fmt.Errorf("invalid API base URL: %w", err)
	}
	if _, err := url.ParseRequestURI(mcpURL); err != nil {
		return fmt.Errorf("invalid MCP URL: %w", err)
	}
	for index, probe := range graphReadProbeRegistry {
		token := userToken
		if probe.auth == graphReadProbeAdminAuth {
			token = adminToken
		}
		if err := executeGraphReadProbe(client, apiBaseURL, mcpURL, token, index+1, probe); err != nil {
			return fmt.Errorf("surface %s unsupported or failed: %w", probe.name, err)
		}
		_, _ = fmt.Fprintf(stdout, "PASS %s auth=%s\n", probe.name, probe.auth)
	}
	targets, err := currentAPIAndMCPSurfaces()
	if err != nil {
		return err
	}
	supported := map[string]struct{}{
		"api:GET /api/v0/repositories":    {},
		"api:POST /api/v0/code/cypher":    {},
		"api:POST /api/v0/code/visualize": {},
		"mcp:list_indexed_repositories":   {},
		"mcp:get_repository_stats":        {},
		"mcp:execute_cypher_query":        {},
		"mcp:visualize_graph_query":       {},
	}
	unsupported := make([]string, 0, len(targets))
	for _, target := range targets {
		if _, ok := supported[target]; !ok {
			unsupported = append(unsupported, target)
		}
	}
	if len(unsupported) > 0 {
		return fmt.Errorf(
			"%d current surfaces lack checked-in fixtures (first: %s)",
			len(unsupported),
			unsupported[0],
		)
	}
	return nil
}

func executeGraphReadProbe(
	client *http.Client,
	apiBaseURL string,
	mcpURL string,
	token string,
	id int,
	probe graphReadProbe,
) error {
	requestURL := strings.TrimRight(apiBaseURL, "/") + probe.path
	method := probe.method
	payload := probe.arguments
	if probe.transport == "mcp" {
		requestURL = mcpURL
		method = http.MethodPost
		payload = map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "tools/call",
			"params": map[string]any{
				"name": probe.tool, "arguments": probe.arguments,
			},
		}
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode fixture: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	if probe.transport != "mcp" {
		return nil
	}
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("decode MCP response: %w", err)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return fmt.Errorf("MCP JSON-RPC error")
	}
	if envelope.Result.IsError {
		return fmt.Errorf("MCP tool result reported an error")
	}
	return nil
}
