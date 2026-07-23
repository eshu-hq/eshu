// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	identity     string
	name         string
	transport    string
	method       string
	path         string
	tool         string
	query        map[string]any
	arguments    map[string]any
	auth         graphReadProbeAuth
	execute      bool
	unsafeReason string
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
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return runGraphReadProbe(stdout, client, apiBaseURL, mcpURL, userToken, adminToken)
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
		return errors.New("invalid API base URL")
	}
	if _, err := url.ParseRequestURI(mcpURL); err != nil {
		return errors.New("invalid MCP URL")
	}
	registry, err := buildCurrentProbeRegistry()
	if err != nil {
		return err
	}
	selectors, err := discoverProbeSelectors(client, apiBaseURL, userToken)
	if err != nil {
		return err
	}
	for index, rawProbe := range registry {
		if !rawProbe.execute {
			_, _ = fmt.Fprintf(stdout, "SKIP %s reason=%s\n", rawProbe.identity, rawProbe.unsafeReason)
			continue
		}
		probe, err := resolveProbeSelectors(rawProbe, selectors)
		if err != nil {
			return fmt.Errorf("surface %s fixture: %w", rawProbe.identity, err)
		}
		token := userToken
		switch probe.auth {
		case graphReadProbeAdminAuth:
			token = adminToken
		case "public":
			token = ""
		}
		if err := executeGraphReadProbe(client, apiBaseURL, mcpURL, token, index+1, probe); err != nil {
			return fmt.Errorf("surface %s failed: %w", probe.identity, err)
		}
		_, _ = fmt.Fprintf(stdout, "PASS %s auth=%s\n", probe.identity, probe.auth)
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
	if len(probe.query) > 0 {
		values := url.Values{}
		for name, value := range probe.query {
			values.Set(name, fmt.Sprint(value))
		}
		requestURL += "?" + values.Encode()
	}
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
	request, err := http.NewRequestWithContext(ctx, method, requestURL, body) // #nosec G704 -- requestURL derives from the operator-supplied Eshu API endpoint for this diagnostic probe CLI, not user- or network-taint input
	if err != nil {
		return errors.New("build request failed")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request) // #nosec G704 -- request targets the operator-supplied Eshu API base URL for this diagnostic probe CLI, not user- or network-taint input
	if err != nil {
		return errors.New("request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if !probeAcceptsStatus(probe, response.StatusCode) {
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

func probeAcceptsStatus(probe graphReadProbe, status int) bool {
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return true
	}
	if probe.identity == "api:GET /api/v0/auth/github/login" {
		return status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusServiceUnavailable
	}
	if probe.identity == "api:GET /api/v0/auth/github/callback" {
		return status == http.StatusBadRequest
	}
	return false
}
