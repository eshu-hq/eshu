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
	"sort"
	"strings"
	"time"
)

// OpenAPI/MCP-tool sourcing for capability-inventory (Refs eshu-hq/eshu#9669,
// row 7). The graph-read probe registry and the surface inventory are built
// from the satellite, not from in-tree packages: API routes come from the
// served OpenAPI document (GET /api/v0/openapi.json on the eshu-api surface,
// behind RequireAuth like every satellite /api/* route, so the fetch carries
// the admin/all-scope bearer token) and MCP tools come from the satellite MCP
// endpoint (tools/list). Owner decision eshu-hq/eshu-api#101: authed fetcher
// plus the full served surface; no posture downgrade, no public doc.
const (
	// satelliteFetchTimeout bounds one satellite document fetch, matching
	// the bounded discovery timeouts used by the probe selectors.
	satelliteFetchTimeout = 15 * time.Second
	// satelliteOpenAPILimit caps the served OpenAPI document (156 spec
	// fragments assemble to a few megabytes); satelliteMCPToolsLimit caps
	// the tools/list payload the same way discovery caps selector reads.
	satelliteOpenAPILimit  = 16 << 20
	satelliteMCPToolsLimit = 8 << 20
	satelliteOpenAPIPath   = "/api/v0/openapi.json"
	mcpToolsListMethod     = "tools/list"
	jsonRPCVersion         = "2.0"
)

// satelliteMCPTool is one tools/list entry: the name the probe registry
// keys on plus the input schema fixtures are synthesized from.
type satelliteMCPTool struct {
	Name        string `json:"name"`
	InputSchema any    `json:"inputSchema"`
}

// fetchSatelliteOpenAPISpec reads the served OpenAPI document from the
// eshu-api surface. The route sits behind RequireAuth, so the caller
// attaches the admin/all-scope bearer token; a foreign or missing
// credential answers 401 and surfaces as an HTTP status error. Errors
// are sanitized: they never carry the base URL, a token, or the
// response body.
func fetchSatelliteOpenAPISpec(client *http.Client, apiBaseURL, apiToken string) (map[string]any, error) {
	if client == nil {
		return nil, fmt.Errorf("satellite OpenAPI HTTP client is required")
	}
	if _, err := url.ParseRequestURI(apiBaseURL); err != nil {
		return nil, fmt.Errorf("invalid API base URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), satelliteFetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext( // #nosec G704 -- apiBaseURL is the operator-supplied Eshu API endpoint for this diagnostic probe CLI, not user- or network-taint input
		ctx,
		http.MethodGet,
		strings.TrimRight(apiBaseURL, "/")+satelliteOpenAPIPath,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build satellite openapi request failed")
	}
	request.Header.Set("Authorization", "Bearer "+apiToken)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request) // #nosec G704 -- request targets the operator-supplied Eshu API base URL for this diagnostic probe CLI, not user- or network-taint input
	if err != nil {
		return nil, fmt.Errorf("satellite openapi request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("satellite openapi HTTP status %d", response.StatusCode)
	}
	var raw any
	if err := json.NewDecoder(io.LimitReader(response.Body, satelliteOpenAPILimit)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode satellite openapi: %w", err)
	}
	document, _ := raw.(map[string]any)
	if document == nil {
		return nil, fmt.Errorf("satellite openapi is not a JSON object")
	}
	return document, nil
}

// fetchSatelliteMCPTools lists the MCP tools the satellite serves via
// JSON-RPC tools/list. The user token is least privilege for catalog
// enumeration: tools/list advertises the deployment surface without a
// per-tool scope check, and per-probe execution auth is unchanged.
func fetchSatelliteMCPTools(client *http.Client, mcpURL, userToken string) ([]satelliteMCPTool, error) {
	if client == nil {
		return nil, fmt.Errorf("satellite MCP HTTP client is required")
	}
	if _, err := url.ParseRequestURI(mcpURL); err != nil {
		return nil, fmt.Errorf("invalid MCP URL")
	}
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": jsonRPCVersion,
		"id":      1,
		"method":  mcpToolsListMethod,
		"params":  map[string]any{},
	})
	if err != nil {
		return nil, fmt.Errorf("encode MCP tools/list request: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), satelliteFetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(payload)) // #nosec G704 -- mcpURL is the operator-supplied MCP endpoint for this diagnostic probe CLI, not user- or network-taint input
	if err != nil {
		return nil, fmt.Errorf("build MCP tools/list request failed")
	}
	request.Header.Set("Authorization", "Bearer "+userToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request) // #nosec G704 -- request targets the operator-supplied MCP endpoint for this diagnostic probe CLI, not user- or network-taint input
	if err != nil {
		return nil, fmt.Errorf("MCP tools/list request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("MCP tools/list HTTP status %d", response.StatusCode)
	}
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			Tools []satelliteMCPTool `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, satelliteMCPToolsLimit)).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode MCP tools/list response: %w", err)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return nil, fmt.Errorf("MCP tools/list JSON-RPC error")
	}
	return envelope.Result.Tools, nil
}

// httpOperationMethods is the set of OpenAPI path-item keys that are HTTP
// operations. Other keys (parameters, summary, description, servers, $ref) are
// path-item metadata, not operations, and are skipped.
var httpOperationMethods = map[string]struct{}{
	"get": {}, "put": {}, "post": {}, "delete": {},
	"options": {}, "head": {}, "patch": {}, "trace": {},
}

// openAPIRoutes returns one "METHOD /path" surface per OpenAPI path-item
// operation (for example "GET /api/v0/capabilities"). Recording the method,
// not just the path key, means adding a second operation such as
// "POST /api/v0/foo" beside an existing "GET /api/v0/foo" changes the
// inventory, so method-level API surface changes cannot bypass the drift
// gate. The spec is the single declaration of the HTTP surface.
func openAPIRoutes(document map[string]any) []string {
	paths, _ := document["paths"].(map[string]any)
	var names []string
	for path, raw := range paths {
		item, _ := raw.(map[string]any)
		for method := range item {
			if _, ok := httpOperationMethods[strings.ToLower(method)]; !ok {
				continue
			}
			names = append(names, strings.ToUpper(method)+" "+path)
		}
	}
	sort.Strings(names)
	return names
}
