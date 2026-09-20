// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// infraTruthRoutes are the routes the infra read model serves.
var infraTruthRoutes = []string{
	"/api/v0/infra/resources/count",
	"/api/v0/infra/resources/inventory",
}

// assertInfraServedFromReadModel requests each infra route once with the
// envelope media type (the only request shape the API attaches a truth basis
// to) and requires truth.basis to be "hybrid" or "content_index": the values
// the read model reports. "authoritative_graph" means the route fell back to the
// graph, which happens whenever the backfill marker is missing or a repository
// is still marked dirty; sweeping then would measure the old path. This is more
// direct than inferring the store from latency. The sweep itself keeps plain
// requests.
func assertInfraServedFromReadModel(ctx context.Context, baseURL, apiKey string, timeout time.Duration, log io.Writer) error {
	client := &http.Client{Timeout: timeout}
	for _, route := range infraTruthRoutes {
		basis, err := infraTruthBasisWithRetry(ctx, client, baseURL+route, apiKey, log)
		if err != nil {
			return fmt.Errorf("infra read model check %s: %w", route, err)
		}
		if basis != "hybrid" && basis != "content_index" {
			return fmt.Errorf("infra read model check %s: truth.basis = %q, want hybrid or content_index (the route is not served from the read model)", route, basis)
		}
	}
	return nil
}

// infraTruthAttempts bounds how often a 5xx from an infra route is retried.
// The check asks which store served the route, not how fast: on a cold
// NornicDB the count route's graph pass for the graph-only labels took 8.3s
// against the API's 10s deadline, so a FIRST request can 504 while the next
// succeeds. A wrong basis is never retried; that is the finding.
const infraTruthAttempts = 3

func infraTruthBasisWithRetry(ctx context.Context, client *http.Client, url, apiKey string, log io.Writer) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= infraTruthAttempts; attempt++ {
		basis, status, err := infraTruthBasis(ctx, client, url, apiKey)
		if err == nil {
			return basis, nil
		}
		lastErr = err
		if status < 500 {
			return "", err
		}
		_, _ = fmt.Fprintf(log, "read-api-latency-gate: infra read model check attempt %d/%d failed: %v\n", attempt, infraTruthAttempts, err)
	}
	return "", lastErr
}

func infraTruthBasis(ctx context.Context, client *http.Client, url, apiKey string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/eshu.envelope+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", resp.StatusCode, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > hardFailedBodyCap {
			snippet = snippet[:hardFailedBodyCap]
		}
		return "", resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
	}
	var envelope struct {
		Truth *struct {
			Basis string `json:"basis"`
		} `json:"truth"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", resp.StatusCode, fmt.Errorf("decode envelope: %w", err)
	}
	if envelope.Truth == nil {
		return "", resp.StatusCode, fmt.Errorf("response has no truth envelope")
	}
	return envelope.Truth.Basis, resp.StatusCode, nil
}
