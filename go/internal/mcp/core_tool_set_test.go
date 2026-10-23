// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	// maxCoreRouterTools is the largest core_router set the epic allows (#8759).
	maxCoreRouterTools = 10
	// maxCoreRouterBytes is the largest serialized core set the epic allows.
	maxCoreRouterBytes = 40 * 1024
)

// measuredCoreToolNames is the core_router list the QA traffic measurement in
// docs/internal/design/8759-mcp-core-router-tool-set.md selected, in share
// order. The pin test fails when core.go drifts from it, so a list change needs
// a matching change to the record's table and to this slice.
var measuredCoreToolNames = []string{
	"list_codeowners_ownership",
	"search_file_content",
	"get_file_lines",
	"get_repository_freshness",
	"get_file_content",
	"list_repository_files",
	"find_code",
	"list_repositories_by_language",
	"find_infra_resources",
	"investigate_code_topic",
}

// coreSetProblems checks a candidate core_router tool-name list against the
// registry, the production hide set, and the epic's limits. It returns one
// message per violation and the serialized size of the core definitions it
// could resolve. It lives in the mcp package because the router package must
// not import the registry.
func coreSetProblems(names []string, registry []ToolDefinition, hidden map[string]bool) ([]string, int) {
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
		if hidden[name] {
			problems = append(problems, fmt.Sprintf("core tool %q is hidden in the production profile", name))
		}
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
	problems, size := coreSetProblems(names, ReadOnlyTools(), productionHiddenToolNames())
	for _, p := range problems {
		t.Error(p)
	}
	if len(names) == 0 {
		t.Error("core_router core set is empty, want the measured list from the #8759 decision record")
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
		hidden   map[string]bool
		want     string
	}{
		{"unregistered name", []string{registry[0].Name, "no_such_tool"}, registry, nil, `"no_such_tool" is not in ReadOnlyTools()`},
		{"eleven entries", elevenNames, registry, nil, fmt.Sprintf("want at most %d", maxCoreRouterTools)},
		{"duplicate entry", []string{registry[0].Name, registry[0].Name}, registry, nil, "is listed twice"},
		{"hidden in production", []string{registry[0].Name}, registry, map[string]bool{registry[0].Name: true}, "is hidden in the production profile"},
		{"oversized set", []string{"oversized_tool"}, bigRegistry, nil, "serialized core set is"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			problems, _ := coreSetProblems(tt.names, tt.registry, tt.hidden)
			if !strings.Contains(strings.Join(problems, "\n"), tt.want) {
				t.Fatalf("coreSetProblems problems = %q, want one containing %q", problems, tt.want)
			}
		})
	}

	t.Run("duplicated unregistered name reports both problems", func(t *testing.T) {
		t.Parallel()
		problems, _ := coreSetProblems([]string{"no_such_tool", "no_such_tool"}, registry, nil)
		joined := strings.Join(problems, "\n")
		for _, want := range []string{`"no_such_tool" is not in ReadOnlyTools()`, `"no_such_tool" is listed twice`} {
			if !strings.Contains(joined, want) {
				t.Errorf("coreSetProblems problems = %q, want one containing %q", problems, want)
			}
		}
	})

	t.Run("clean list passes", func(t *testing.T) {
		t.Parallel()
		problems, size := coreSetProblems([]string{registry[0].Name, registry[1].Name}, registry, nil)
		if len(problems) != 0 {
			t.Fatalf("coreSetProblems problems = %q, want none", problems)
		}
		if size <= 0 {
			t.Fatalf("serialized size = %d, want > 0", size)
		}
	})
}

// TestCoreRouterToolSetMatchesMeasuredList pins core.go to the list the
// decision record measured, so a quiet edit to coreToolNames fails here.
func TestCoreRouterToolSetMatchesMeasuredList(t *testing.T) {
	t.Parallel()
	got := router.CoreToolNames()
	if strings.Join(got, ",") != strings.Join(measuredCoreToolNames, ",") {
		t.Fatalf("router.CoreToolNames() = %v, want the measured list %v", got, measuredCoreToolNames)
	}
}

// TestCoreRouterToolsAreAdvertisedInProduction proves each core tool survives
// the production profile filter, so core_router never advertises a name the
// production tools/list hides.
func TestCoreRouterToolsAreAdvertisedInProduction(t *testing.T) {
	t.Parallel()
	advertised := make(map[string]bool)
	for _, def := range filterToolsForProfile(ReadOnlyTools(), querycontract.ProfileProduction) {
		advertised[def.Name] = true
	}
	for _, name := range router.CoreToolNames() {
		if !advertised[name] {
			t.Errorf("core tool %q is not advertised by the production profile", name)
		}
	}
}
