// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp"
	toolcontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// findTool sends one find_tool tools/call to POST /mcp/message, the
// production transport, and returns the decoded tool result. It returns an
// error instead of failing the test so concurrent callers can use it.
func findTool(handler http.Handler, intent string) (map[string]any, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": router.FindToolName, "arguments": map[string]any{"intent": intent}},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/message", strings.NewReader(string(body))))
	if rec.Code != http.StatusOK {
		return nil, fmt.Errorf("POST /mcp/message status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Error != nil {
		return nil, fmt.Errorf("decode response %s: err=%v rpcError=%v", rec.Body.String(), err, resp.Error)
	}
	if isErr, _ := resp.Result["isError"].(bool); isErr {
		return nil, fmt.Errorf("find_tool isError: %v", resp.Result)
	}
	return resp.Result, nil
}

func findToolOverHTTP(t *testing.T, handler http.Handler, intent string) map[string]any {
	t.Helper()
	result, err := findTool(handler, intent)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// TestMCPServerFindToolReturnsCandidatesWithTheProductionRanker proves the
// production option set (newMCPServer with newLexicalRanker) answers find_tool
// with candidates on every surface instead of mcp_router_ranker_unavailable
// (#8771).
func TestMCPServerFindToolReturnsCandidatesWithTheProductionRanker(t *testing.T) {
	t.Parallel()

	for _, surface := range []router.Surface{router.SurfaceRouter, router.SurfaceCoreRouter, router.SurfaceFull} {
		t.Run(string(surface), func(t *testing.T) {
			t.Parallel()
			server, build, err := newMCPServer(http.NewServeMux(), discardLogger(), nil,
				query.ProfileProduction, surface, newLexicalRanker)
			if err != nil {
				t.Fatalf("newMCPServer(%s) error = %v", surface, err)
			}
			if !build.built || build.tools == 0 || build.entries == 0 {
				t.Fatalf("ranker build = %+v, want a built ranker over the registry and catalog entries", build)
			}
			result := findToolOverHTTP(t, server.Handler(nil), "list the indexed repositories")
			structured, _ := result["structuredContent"].(map[string]any)
			candidates, _ := structured["candidates"].([]any)
			if len(candidates) == 0 {
				t.Fatalf("find_tool returned no candidates: %v", result)
			}
			first, _ := candidates[0].(map[string]any)
			if first["name"] == "" || first["name"] == nil {
				t.Fatalf("first candidate has no name: %v", first)
			}
		})
	}
}

// TestNewMCPServerRefusesARouterSurfaceWithoutARanker proves a nil ranker on
// router and core_router fails startup, not the first find_tool request.
func TestNewMCPServerRefusesARouterSurfaceWithoutARanker(t *testing.T) {
	t.Parallel()

	nilRanker := func([]toolcontract.ToolDefinition, map[string]catalog.Entry) router.Ranker { return nil }
	for _, tt := range []struct {
		name      string
		surface   router.Surface
		newRanker router.RankerBuilder
		wantErr   bool
	}{
		{name: "router nil build", surface: router.SurfaceRouter, newRanker: nilRanker, wantErr: true},
		{name: "core_router nil build", surface: router.SurfaceCoreRouter, newRanker: nilRanker, wantErr: true},
		{name: "router no builder", surface: router.SurfaceRouter, newRanker: nil, wantErr: true},
		{name: "core_router no builder", surface: router.SurfaceCoreRouter, newRanker: nil, wantErr: true},
		{name: "full nil build", surface: router.SurfaceFull, newRanker: nilRanker, wantErr: false},
		{name: "router lexical", surface: router.SurfaceRouter, newRanker: newLexicalRanker, wantErr: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := newMCPServer(http.NewServeMux(), discardLogger(), nil, query.ProfileLocalAuthoritative, tt.surface, tt.newRanker)
			if (err != nil) != tt.wantErr {
				t.Fatalf("newMCPServer(%s) error = %v, wantErr %v", tt.surface, err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), string(tt.surface)) {
				t.Fatalf("error %q does not name the surface %q", err, tt.surface)
			}
		})
	}
}

// identitySpy wraps the production ranker and records the inputs of every
// Rank call, so the test can compare them with the build inputs by identity.
type identitySpy struct {
	inner   router.Ranker
	mu      sync.Mutex
	tools   [][]toolcontract.ToolDefinition
	entries []map[string]catalog.Entry
}

func (s *identitySpy) Rank(ctx context.Context, q router.Query, tools []toolcontract.ToolDefinition,
	entries map[string]catalog.Entry, allowed func(string) bool,
) ([]router.Candidate, error) {
	s.mu.Lock()
	s.tools = append(s.tools, tools)
	s.entries = append(s.entries, entries)
	s.mu.Unlock()
	return s.inner.Rank(ctx, q, tools, entries, allowed)
}

// TestMCPServerBuildsTheRankerOnceAndRanksTheSameInputs pins the index
// lifecycle (router/rank README): one build at startup over the
// profile-filtered tool list, and every Rank call, including concurrent ones,
// handed that same slice and that same entries map.
func TestMCPServerBuildsTheRankerOnceAndRanksTheSameInputs(t *testing.T) {
	t.Parallel()

	for _, profile := range []query.QueryProfile{query.ProfileProduction, query.ProfileLocalAuthoritative} {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()
			var (
				builds       int
				builtTools   []toolcontract.ToolDefinition
				builtEntries map[string]catalog.Entry
				spy          *identitySpy
			)
			newRanker := func(tools []toolcontract.ToolDefinition, entries map[string]catalog.Entry) router.Ranker {
				builds++
				builtTools, builtEntries = tools, entries
				spy = &identitySpy{inner: newLexicalRanker(tools, entries)}
				return spy
			}
			server, _, err := newMCPServer(http.NewServeMux(), discardLogger(), nil, profile, router.SurfaceRouter, newRanker)
			if err != nil {
				t.Fatalf("newMCPServer error = %v", err)
			}
			if builds != 1 {
				t.Fatalf("ranker built %d times at startup, want once", builds)
			}
			registry := len(mcp.ReadOnlyTools())
			if profile == query.ProfileProduction && len(builtTools) >= registry {
				t.Fatalf("production ranker built over %d tools, want the profile-filtered list (< %d)", len(builtTools), registry)
			}
			if profile == query.ProfileLocalAuthoritative && len(builtTools) != registry {
				t.Fatalf("local ranker built over %d tools, want the full registry %d", len(builtTools), registry)
			}

			handler := server.Handler(nil)
			const calls = 8
			var wg sync.WaitGroup
			errs := make(chan error, calls)
			for range calls {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, err := findTool(handler, "show the deployment story for a service"); err != nil {
						errs <- err
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Fatalf("concurrent find_tool: %v", err)
			}

			if builds != 1 {
				t.Fatalf("ranker built %d times after %d requests, want once", builds, calls)
			}
			if len(spy.tools) != calls {
				t.Fatalf("Rank ran %d times, want %d", len(spy.tools), calls)
			}
			for i := range spy.tools {
				if len(spy.tools[i]) != len(builtTools) || &spy.tools[i][0] != &builtTools[0] {
					t.Fatalf("Rank call %d got a different tools slice than the build", i)
				}
				if reflect.ValueOf(spy.entries[i]).Pointer() != reflect.ValueOf(builtEntries).Pointer() {
					t.Fatalf("Rank call %d got a different entries map than the build", i)
				}
			}
		})
	}
}

// TestLexicalRankerOverAnEmptyToolListAnswersNoCandidates covers a profile
// filter that leaves no tools: the build succeeds and find_tool answers an
// empty candidate list, not an error.
func TestLexicalRankerOverAnEmptyToolListAnswersNoCandidates(t *testing.T) {
	t.Parallel()

	host := &router.Host{NewRanker: newLexicalRanker}
	visible := []toolcontract.ToolDefinition{}
	host.Prepare(visible, discardLogger())
	if host.Ranker == nil {
		t.Fatal("Prepare over an empty tool list built no ranker")
	}
	newConfig := func() router.Config { return router.Config{Registry: mcp.ReadOnlyTools(), Logger: discardLogger()} }
	req := router.Request{Visible: visible, Dispatch: func(context.Context, string, map[string]any) (any, error) { return nil, nil }}
	got, ok := host.Serve(context.Background(), newConfig, req, router.FindToolName, map[string]any{"intent": "list repositories"}).(router.ToolResult)
	if !ok || got.IsError {
		t.Fatalf("find_tool over an empty list = %+v, want a non-error result", got)
	}
	raw, err := json.Marshal(got.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if !strings.Contains(string(raw), `"candidates":[]`) {
		t.Fatalf("find_tool over an empty list = %s, want an empty candidates array", raw)
	}
}
