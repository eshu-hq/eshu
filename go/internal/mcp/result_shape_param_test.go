// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
)

func shapeTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	ts := sseTestServer(t, NewServer(fixedBodyHandler(200, shapeFixtureOK), discardDispatchLogger()))
	return ts, ts.URL
}

const callBody = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_indexed_repositories","arguments":{}}}`

// postToolCall posts one tools/call to path and decodes the result content
// kinds and whether structuredContent was present.
func postToolCall(t *testing.T, url string) (status int, kinds []string, structured bool) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(callBody))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil, false
	}
	var decoded struct {
		Result mcpToolResult `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, content := range decoded.Result.Content {
		kinds = append(kinds, content.Type)
	}
	return resp.StatusCode, kinds, decoded.Result.StructuredContent != nil
}

func TestMessageShapeParam(t *testing.T) {
	t.Parallel()
	_, base := shapeTestServer(t)
	for _, tc := range []struct {
		query     string
		wantKinds string
	}{
		{"", "text,resource"},
		{"?shape=dual", "text,resource"},
		{"?shape=structured", "text"},
		{"?shape=%20Structured%20", "text"}, // trimmed and case-folded like surface
		{"?shape=%20%20", "text,resource"},  // whitespace only means absent
	} {
		status, kinds, structured := postToolCall(t, base+"/mcp/message"+tc.query)
		if status != http.StatusOK || strings.Join(kinds, ",") != tc.wantKinds || !structured {
			t.Fatalf("POST %q = (%d, %v, structured=%v), want kinds %s with structuredContent", tc.query, status, kinds, structured, tc.wantKinds)
		}
	}

	resp, err := http.Post(base+"/mcp/message?shape=text", "application/json", strings.NewReader(callBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "dual") || !strings.Contains(string(body), "structured") {
		t.Fatalf("unknown shape = (%d, %s), want 400 naming the allowed values", resp.StatusCode, body)
	}
}

func TestSSEEndpointEventCarriesShape(t *testing.T) {
	ts := sseTestServer(t, testServer())

	_, next := openSSE(t, ts, "?shape=%20Structured%20")
	_, data := next()
	if !strings.HasPrefix(data, "/mcp/message?sessionId=") || !strings.HasSuffix(data, "&shape=structured") {
		t.Fatalf("endpoint data = %q, want canonical &shape=structured", data)
	}
	_, next = openSSE(t, ts, "?surface=router&shape=structured")
	_, data = next()
	if !strings.HasSuffix(data, "&surface=router&shape=structured") {
		t.Fatalf("endpoint data = %q, want surface then shape", data)
	}
	_, next = openSSE(t, ts, "?shape=DUAL")
	_, data = next()
	if !strings.HasSuffix(data, "&shape=dual") { // explicit dual is echoed canonically (this server's process default is dual)
		t.Fatalf("endpoint data = %q, want &shape=dual", data)
	}
	for _, query := range []string{"", "?shape=%20%20"} {
		_, next = openSSE(t, ts, query)
		_, data = next()
		if strings.Contains(data, "shape") {
			t.Fatalf("GET /sse%s endpoint = %q, want no shape parameter", query, data)
		}
	}
	resp, err := http.Get(ts.URL + "/sse?shape=bogus")
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown shape status = %d, want 400", resp.StatusCode)
	}
}

// TestSSELinkedPostUsesSessionShape proves a session's shape applies to its
// SSE-linked POSTs and that a POST shape overrides it.
func TestSSELinkedPostUsesSessionShape(t *testing.T) {
	ts, base := shapeTestServer(t)
	_, next := openSSE(t, ts, "?shape=structured")
	_, endpoint := next()
	cut := strings.Index(endpoint, "&shape=")
	if cut < 0 {
		t.Fatalf("endpoint %q carries no shape", endpoint)
	}
	sessionOnly := endpoint[:cut]

	readMessage := func(path string) []string {
		t.Helper()
		resp, err := http.Post(base+path, "application/json", strings.NewReader(callBody))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		_ = resp.Body.Close()
		_, data := next()
		var decoded struct {
			Result mcpToolResult `json:"result"`
		}
		if err := json.Unmarshal([]byte(data), &decoded); err != nil {
			t.Fatalf("decode %q: %v", data, err)
		}
		var kinds []string
		for _, content := range decoded.Result.Content {
			kinds = append(kinds, content.Type)
		}
		return kinds
	}
	for _, path := range []string{endpoint, sessionOnly} {
		if got := strings.Join(readMessage(path), ","); got != "text" {
			t.Fatalf("POST %s content = %s, want text", path, got)
		}
	}
	if got := strings.Join(readMessage(sessionOnly+"&shape=dual"), ","); got != "text,resource" {
		t.Fatalf("POST shape=dual over a structured session content = %s, want text,resource", got)
	}
}

// TestProcessDefaultShapeAppliesWithoutOverride proves WithResultShape sets the
// shape for a context that carries no override (what the stdio path sends) and
// that, calling handleMessage directly, a dual override wins over a structured
// default and a structured override wins over the dual default. HTTP routing
// of the override is covered by the POST tests above.
func TestProcessDefaultShapeAppliesWithoutOverride(t *testing.T) {
	t.Parallel()
	call := func(s *Server, ctx context.Context) mcpToolResult {
		req := &jsonrpcRequest{
			JSONRPC: "2.0", ID: float64(1), Method: "tools/call",
			Params: json.RawMessage(`{"name":"list_indexed_repositories","arguments":{}}`),
		}
		resp := s.handleMessage(ctx, req, "")
		result, ok := resp.Result.(mcpToolResult)
		if !ok {
			t.Fatalf("result = %T", resp.Result)
		}
		return result
	}
	structuredDefault := NewServer(fixedBodyHandler(200, shapeFixtureOK), discardDispatchLogger(), WithResultShape(resultshape.Structured))
	requireOneTextAndStructured(t, call(structuredDefault, context.Background()))
	if got := call(structuredDefault, resultshape.WithOverride(context.Background(), resultshape.Dual)); len(got.Content) != 2 {
		t.Fatalf("dual override content = %d items, want 2", len(got.Content))
	}
	dualDefault := NewServer(fixedBodyHandler(200, shapeFixtureOK), discardDispatchLogger())
	if got := dualDefault.processShape(); got != resultshape.Dual {
		t.Fatalf("default process shape = %q, want dual", got)
	}
	requireOneTextAndStructured(t, call(dualDefault, resultshape.WithOverride(context.Background(), resultshape.Structured)))
}

// TestExplicitDualOverridesStructuredProcessDefault pins the override direction
// the process default cannot cover: on a server whose process shape is
// structured, an explicit dual (case-folded) is echoed on the SSE endpoint and
// wins on a POST, while a request that names no shape keeps the default.
func TestExplicitDualOverridesStructuredProcessDefault(t *testing.T) {
	t.Parallel()
	s := NewServer(fixedBodyHandler(200, shapeFixtureOK), discardDispatchLogger(), WithResultShape(resultshape.Structured))
	ts := sseTestServer(t, s)

	_, next := openSSE(t, ts, "?shape=DUAL")
	_, data := next()
	if !strings.HasSuffix(data, "&shape=dual") {
		t.Fatalf("endpoint data = %q, want &shape=dual over a structured process default", data)
	}
	_, next = openSSE(t, ts, "")
	_, data = next()
	if strings.Contains(data, "shape") {
		t.Fatalf("endpoint data = %q, want no shape parameter when none was asked for", data)
	}

	for _, tc := range []struct {
		query     string
		wantKinds string
	}{
		{"?shape=DUAL", "text,resource"},
		{"", "text"},
	} {
		status, kinds, structured := postToolCall(t, ts.URL+"/mcp/message"+tc.query)
		if status != http.StatusOK || strings.Join(kinds, ",") != tc.wantKinds || !structured {
			t.Fatalf("POST %q = (%d, %v, structured=%v), want kinds %s with structuredContent", tc.query, status, kinds, structured, tc.wantKinds)
		}
	}
}
