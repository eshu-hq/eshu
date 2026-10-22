// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// toolsListResult runs tools/list through handleMessage and returns the
// marshaled result plus the advertised tool list.
func toolsListResult(t *testing.T, s *Server) ([]byte, []ToolDefinition) {
	t.Helper()
	resp := s.handleMessage(context.Background(), &jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"}, "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/list response = %+v, want result", resp)
	}
	result, ok := resp.Result.(mcpToolsListResult)
	if !ok {
		t.Fatalf("tools/list result is %T, want mcpToolsListResult", resp.Result)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal tools/list result: %v", err)
	}
	return raw, result.Tools
}

// toolsListMismatch compares a tools/list response with the expected registry
// list byte for byte. It returns "" on a match, otherwise a message naming the
// first tool whose advertised bytes differ.
func toolsListMismatch(got []byte, gotTools, want []ToolDefinition) string {
	wantRaw, err := json.Marshal(mcpToolsListResult{Tools: want})
	if err != nil {
		return fmt.Sprintf("marshal expected registry list: %v", err)
	}
	if bytes.Equal(got, wantRaw) {
		return ""
	}
	for i := 0; i < len(gotTools) || i < len(want); i++ {
		switch {
		case i >= len(want):
			return fmt.Sprintf("tools/list advertises extra tool %q at index %d", gotTools[i].Name, i)
		case i >= len(gotTools):
			return fmt.Sprintf("tools/list is missing registry tool %q at index %d", want[i].Name, i)
		}
		g, _ := json.Marshal(gotTools[i])
		w, _ := json.Marshal(want[i])
		if !bytes.Equal(g, w) {
			return fmt.Sprintf("tools/list tool %q at index %d differs from the registry (got %q at that index)", want[i].Name, i, gotTools[i].Name)
		}
	}
	return "tools/list bytes differ from the registry list but no single tool differs"
}

// TestToolsListDefaultSurfaceAdvertisesExactlyRegistry holds the router epic's
// invariant (#8752): with no surface or profile option, tools/list returns the
// registry (ReadOnlyTools) in order, byte for byte. It compares against the
// registry, not a stored hash, so unrelated description or schema edits do not
// break it.
func TestToolsListDefaultSurfaceAdvertisesExactlyRegistry(t *testing.T) {
	raw, got := toolsListResult(t, testServer())
	if msg := toolsListMismatch(raw, got, ReadOnlyTools()); msg != "" {
		t.Fatal(msg)
	}
}

// TestToolsListFullUnderProductionEqualsFilteredRegistry carries the #8762
// review item: full under the production profile is byte-identical to the
// profile-filtered registry.
func TestToolsListFullUnderProductionEqualsFilteredRegistry(t *testing.T) {
	s := NewServer(testServer().handler, nil,
		WithQueryProfile(querycontract.ProfileProduction), WithToolSurface(router.SurfaceFull))
	raw, got := toolsListResult(t, s)
	want := filterToolsForProfile(ReadOnlyTools(), querycontract.ProfileProduction)
	if msg := toolsListMismatch(raw, got, want); msg != "" {
		t.Fatal(msg)
	}
}

// TestToolsListMismatchDetectsDrift proves the comparison fails when the
// advertised list adds, drops, or alters one tool, and still passes when only
// the expected side is rebuilt from the same registry.
func TestToolsListMismatchDetectsDrift(t *testing.T) {
	registry := ReadOnlyTools()
	extra := ToolDefinition{Name: "injected_tool", Description: "x", InputSchema: map[string]any{"type": "object"}}

	tests := []struct {
		name     string
		tools    []ToolDefinition
		wantName string
	}{
		{name: "drops one tool", tools: append(append([]ToolDefinition{}, registry[:3]...), registry[4:]...), wantName: registry[3].Name},
		{name: "adds one tool", tools: append(append([]ToolDefinition{}, registry...), extra), wantName: extra.Name},
		{name: "alters one description", tools: func() []ToolDefinition {
			c := append([]ToolDefinition{}, registry...)
			c[7].Description += " changed"
			return c
		}(), wantName: registry[7].Name},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testServer()
			s.tools = tt.tools
			raw, got := toolsListResult(t, s)
			msg := toolsListMismatch(raw, got, registry)
			if msg == "" {
				t.Fatalf("mismatch not detected for %s", tt.name)
			}
			if !bytes.Contains([]byte(msg), []byte(`"`+tt.wantName+`"`)) {
				t.Fatalf("message %q does not name %q", msg, tt.wantName)
			}
		})
	}

	t.Run("clean server passes", func(t *testing.T) {
		raw, got := toolsListResult(t, testServer())
		if msg := toolsListMismatch(raw, got, ReadOnlyTools()); msg != "" {
			t.Fatal(msg)
		}
	})
}
