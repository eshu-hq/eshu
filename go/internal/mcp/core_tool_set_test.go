// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

const (
	// maxCoreRouterTools is the largest core_router set the epic allows (#8759).
	maxCoreRouterTools = 10
	// maxCoreRouterBytes is the largest serialized core set the epic allows.
	maxCoreRouterBytes = 40 * 1024
)

// coreSetProblems checks a candidate core_router tool-name list against the
// registry and the epic's limits. It returns one message per violation and the
// serialized size of the core definitions it could resolve. It lives in the mcp
// package because the router package must not import the registry.
func coreSetProblems(names []string, registry []ToolDefinition) ([]string, int) {
	byName := make(map[string]ToolDefinition, len(registry))
	for _, def := range registry {
		byName[def.Name] = def
	}
	var problems []string
	if len(names) > maxCoreRouterTools {
		problems = append(problems, fmt.Sprintf("core set has %d tools, want at most %d", len(names), maxCoreRouterTools))
	}
	seen := make(map[string]bool, len(names))
	defs := make([]ToolDefinition, 0, len(names))
	for _, name := range names {
		if seen[name] {
			problems = append(problems, fmt.Sprintf("core tool %q is listed twice", name))
			continue
		}
		seen[name] = true
		def, ok := byName[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("core tool %q is not in ReadOnlyTools()", name))
			continue
		}
		defs = append(defs, def)
	}
	raw, err := json.Marshal(defs)
	if err != nil {
		return append(problems, fmt.Sprintf("marshal core definitions: %v", err)), 0
	}
	if len(raw) > maxCoreRouterBytes {
		problems = append(problems, fmt.Sprintf("serialized core set is %d bytes, want at most %d", len(raw), maxCoreRouterBytes))
	}
	return problems, len(raw)
}

// TestCoreRouterToolSetGuardrails holds the shipped core_router tool list to
// the registry, the ten-tool cap, and the 40 KB advertised-size cap (#8759).
func TestCoreRouterToolSetGuardrails(t *testing.T) {
	t.Parallel()
	names := router.CoreToolNames()
	problems, size := coreSetProblems(names, ReadOnlyTools())
	for _, p := range problems {
		t.Error(p)
	}
	routerRaw, err := json.Marshal(router.Tools())
	if err != nil {
		t.Fatalf("marshal router tools: %v", err)
	}
	t.Logf("core_router core set: %d tools, %d serialized bytes (limit %d); find_tool+call_tool add %d bytes",
		len(names), size, maxCoreRouterBytes, len(routerRaw))
}

// TestCoreRouterLimitsMatchEpic pins the limits to the epic's numbers, so a
// quiet edit to either constant fails here instead of loosening the guard.
func TestCoreRouterLimitsMatchEpic(t *testing.T) {
	t.Parallel()
	if maxCoreRouterTools != 10 {
		t.Errorf("maxCoreRouterTools = %d, epic #8752 allows 10", maxCoreRouterTools)
	}
	if maxCoreRouterBytes != 40*1024 {
		t.Errorf("maxCoreRouterBytes = %d, epic #8752 allows %d", maxCoreRouterBytes, 40*1024)
	}
}

// TestCoreSetProblemsFlagsSeededViolations runs the guard against planted bad
// lists, so a guard that stops checking fails here and not silently.
func TestCoreSetProblemsFlagsSeededViolations(t *testing.T) {
	t.Parallel()
	registry := ReadOnlyTools()
	if len(registry) < maxCoreRouterTools+1 {
		t.Fatalf("registry has %d tools, need at least %d to seed the cap test", len(registry), maxCoreRouterTools+1)
	}
	elevenNames := make([]string, 0, maxCoreRouterTools+1)
	for _, def := range registry[:maxCoreRouterTools+1] {
		elevenNames = append(elevenNames, def.Name)
	}
	bigRegistry := []ToolDefinition{{
		Name:        "oversized_tool",
		Description: strings.Repeat("x", maxCoreRouterBytes),
		InputSchema: map[string]any{"type": "object"},
	}}

	tests := []struct {
		name     string
		names    []string
		registry []ToolDefinition
		want     string
	}{
		{"unregistered name", []string{registry[0].Name, "no_such_tool"}, registry, `"no_such_tool" is not in ReadOnlyTools()`},
		{"eleven entries", elevenNames, registry, fmt.Sprintf("want at most %d", maxCoreRouterTools)},
		{"duplicate entry", []string{registry[0].Name, registry[0].Name}, registry, "is listed twice"},
		{"oversized set", []string{"oversized_tool"}, bigRegistry, "serialized core set is"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			problems, _ := coreSetProblems(tt.names, tt.registry)
			if !strings.Contains(strings.Join(problems, "\n"), tt.want) {
				t.Fatalf("coreSetProblems problems = %q, want one containing %q", problems, tt.want)
			}
		})
	}

	t.Run("clean list passes", func(t *testing.T) {
		t.Parallel()
		problems, size := coreSetProblems([]string{registry[0].Name, registry[1].Name}, registry)
		if len(problems) != 0 {
			t.Fatalf("coreSetProblems problems = %q, want none", problems)
		}
		if size <= 0 {
			t.Fatalf("serialized size = %d, want > 0", size)
		}
	})
}
