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
)

// mcpClient invokes MCP tools over the JSON-RPC endpoint POST /mcp/message, which
// eshu-mcp-server serves standalone (no SSE session required: handleHTTPMessage
// returns the JSON-RPC response synchronously). It is the MCP-side counterpart to
// queryClient: where checkQuery validates the HTTP routes the MCP tools proxy to,
// checkMCPQuery proves the MCP tool layer itself — tool dispatch, route
// resolution, and the response envelope — produces the canonical query shapes
// (#3866 criterion 4).
type mcpClient struct {
	baseURL string
	apiKey  string
	doer    httpDoer
	id      int
}

func newMCPClient(baseURL, apiKey string) *mcpClient {
	return &mcpClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		doer:    &http.Client{Timeout: 30 * time.Second},
	}
}

type mcpJSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  mcpToolParams `json:"params"`
}

type mcpToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type mcpJSONRPCResponse struct {
	Result *mcpToolCallResult `json:"result"`
	Error  *mcpRPCError       `json:"error"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolCallResult struct {
	Content           []mcpContentEntry `json:"content"`
	StructuredContent json.RawMessage   `json:"structuredContent"`
	IsError           bool              `json:"isError"`
}

type mcpContentEntry struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// callTool invokes one MCP tool via tools/call and returns the tool's JSON
// payload, preferring structuredContent (the canonical machine-readable copy) and
// falling back to the first text content entry. A transport error, a JSON-RPC
// error, or a tool-reported isError is returned as an error so the caller records
// a failing required finding.
func (c *mcpClient) callTool(ctx context.Context, name string, args map[string]any) ([]byte, error) {
	c.id++
	if args == nil {
		args = map[string]any{}
	}
	reqBody, err := json.Marshal(mcpJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.id,
		Method:  "tools/call",
		Params:  mcpToolParams{Name: name, Arguments: args},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal tools/call %s: %w", name, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/mcp/message", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build tools/call %s: %w", name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST tools/call %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tools/call %s: HTTP %d", name, resp.StatusCode)
	}
	var rpc mcpJSONRPCResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rpc); err != nil {
		return nil, fmt.Errorf("decode tools/call %s: %w", name, err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("tools/call %s: rpc error %d: %s", name, rpc.Error.Code, rpc.Error.Message)
	}
	if rpc.Result == nil {
		return nil, fmt.Errorf("tools/call %s: response carried no result", name)
	}
	if rpc.Result.IsError {
		return nil, fmt.Errorf("tools/call %s: tool reported isError; content=%q", name, firstText(rpc.Result.Content))
	}
	if len(rpc.Result.StructuredContent) > 0 {
		return unwrapTruthEnvelope(rpc.Result.StructuredContent), nil
	}
	if t := firstText(rpc.Result.Content); t != "" {
		return unwrapTruthEnvelope([]byte(t)), nil
	}
	return nil, fmt.Errorf("tools/call %s: result carried no structuredContent or text content", name)
}

// unwrapTruthEnvelope returns the tool payload to assert the query shape against.
// Eshu MCP tools wrap their payload in a truth envelope {data, truth, error}; the
// canonical query data the snapshot shapes describe lives under "data". When the
// raw bytes are such an envelope, the "data" object is returned; otherwise the raw
// bytes are returned unchanged so a non-enveloped tool still works.
func unwrapTruthEnvelope(raw []byte) []byte {
	var env struct {
		Data  json.RawMessage `json:"data"`
		Truth json.RawMessage `json:"truth"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Data) > 0 && len(env.Truth) > 0 {
		return env.Data
	}
	return raw
}

func firstText(entries []mcpContentEntry) string {
	for _, e := range entries {
		if e.Text != "" {
			return e.Text
		}
	}
	return ""
}

// checkMCPQuery asserts every MCP query shape in the snapshot by invoking the tool
// live and validating the returned payload against the shape. Each is a required
// B-7(c) finding (Check "mcp:<tool>"), mirroring checkQuery for the HTTP shapes.
func checkMCPQuery(ctx context.Context, c *mcpClient, snap Snapshot, r *Report) error {
	keys := make([]string, 0, len(snap.QueryShapes.MCP))
	for k := range snap.QueryShapes.MCP {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		shape := snap.QueryShapes.MCP[key]
		body, err := c.callTool(ctx, key, shape.Arguments)
		if err != nil {
			r.AddCheck("query", "mcp:"+key, false, true, err.Error())
			continue
		}
		f := EvaluateQueryShape("mcp:"+key, shape, body)
		r.Add(f)
	}
	return nil
}
