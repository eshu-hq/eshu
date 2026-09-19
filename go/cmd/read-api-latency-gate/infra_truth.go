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
func assertInfraServedFromReadModel(ctx context.Context, baseURL, apiKey string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	for _, route := range infraTruthRoutes {
		basis, err := infraTruthBasis(ctx, client, baseURL+route, apiKey)
		if err != nil {
			return fmt.Errorf("infra read model check %s: %w", route, err)
		}
		if basis != "hybrid" && basis != "content_index" {
			return fmt.Errorf("infra read model check %s: truth.basis = %q, want hybrid or content_index (the route is not served from the read model)", route, basis)
		}
	}
	return nil
}

func infraTruthBasis(ctx context.Context, client *http.Client, url, apiKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/eshu.envelope+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > hardFailedBodyCap {
			snippet = snippet[:hardFailedBodyCap]
		}
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
	}
	var envelope struct {
		Truth *struct {
			Basis string `json:"basis"`
		} `json:"truth"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("decode envelope: %w", err)
	}
	if envelope.Truth == nil {
		return "", fmt.Errorf("response has no truth envelope")
	}
	return envelope.Truth.Basis, nil
}
