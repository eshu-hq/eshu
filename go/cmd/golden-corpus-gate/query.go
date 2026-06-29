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
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// EnvelopeMIMEType is the Eshu response-envelope media type used by query
// truth shapes that assert both data and truth metadata.
const EnvelopeMIMEType = query.EnvelopeMIMEType

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

// request issues an authenticated canonical query request and returns the status
// code and body.
func (c *queryClient) request(ctx context.Context, method, path string, shape QueryShape) (int, []byte, error) {
	url := c.baseURL + path
	var body io.Reader
	if len(shape.RequestBody) > 0 {
		raw, err := json.Marshal(shape.RequestBody)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal request body for %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, nil, fmt.Errorf("build request %s: %w", path, err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if shape.Envelope {
		req.Header.Set("Accept", EnvelopeMIMEType)
	}
	resp, err := c.doer.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read %s body: %w", path, err)
	}
	return resp.StatusCode, responseBody, nil
}

// parseHTTPShapeKey splits a snapshot HTTP shape key like "GET /api/v0/foo" into
// its method and path. Canonical query shapes allow bounded GET reads and POST
// read-style queries whose selectors live in a JSON body.
func parseHTTPShapeKey(key string) (method, path string, err error) {
	parts := strings.Fields(key)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("query shape key %q is not '<METHOD> <path>'", key)
	}
	switch parts[0] {
	case http.MethodGet, http.MethodPost:
	default:
		return "", "", fmt.Errorf("query shape key %q: only GET and POST are supported", key)
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
		method, path, err := parseHTTPShapeKey(key)
		if err != nil {
			return err
		}
		status, body, err := c.request(ctx, method, path, shape)
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
