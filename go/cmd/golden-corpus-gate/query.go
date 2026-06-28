// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// httpDoer is the minimal HTTP seam the gate needs, satisfied by *http.Client and
// faked in tests.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// queryClient fetches canonical API responses for B-7(c) query truth. The API
// key authenticates the data endpoints; health endpoints need none.
type queryClient struct {
	baseURL string
	apiKey  string
	doer    httpDoer
}

func newQueryClient(baseURL, apiKey string) *queryClient {
	return &queryClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		doer:    &http.Client{Timeout: 30 * time.Second},
	}
}

// get issues an authenticated GET and returns the status code and body.
func (c *queryClient) get(ctx context.Context, path string) (int, []byte, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("build request %s: %w", path, err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.doer.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read %s body: %w", path, err)
	}
	return resp.StatusCode, body, nil
}

// parseHTTPShapeKey splits a snapshot HTTP shape key like "GET /api/v0/foo" into
// its method and path. Only GET is supported (every canonical query shape is a
// read).
func parseHTTPShapeKey(key string) (method, path string, err error) {
	parts := strings.Fields(key)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("query shape key %q is not '<METHOD> <path>'", key)
	}
	if parts[0] != http.MethodGet {
		return "", "", fmt.Errorf("query shape key %q: only GET is supported", key)
	}
	return parts[0], parts[1], nil
}

// checkQuery validates every HTTP query shape in the snapshot. Each is a required
// B-7(c) finding: a non-2xx status or a missing field fails the gate.
func checkQuery(ctx context.Context, c *queryClient, snap Snapshot, r *Report) error {
	keys := make([]string, 0, len(snap.QueryShapes.HTTP))
	for k := range snap.QueryShapes.HTTP {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		shape := snap.QueryShapes.HTTP[key]
		_, path, err := parseHTTPShapeKey(key)
		if err != nil {
			return err
		}
		status, body, err := c.get(ctx, path)
		if err != nil {
			return fmt.Errorf("query %s: %w", key, err)
		}
		if status < 200 || status >= 300 {
			r.AddCheck("query", key, false, true,
				fmt.Sprintf("HTTP %d from %s", status, path))
			continue
		}
		f := EvaluateQueryShape(key, shape, body)
		r.Add(f)
	}
	return nil
}
