package mcp

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestQueryPlaybookToolsExistInRegistry cross-checks that every first-class tool
// referenced by a query playbook step or drilldown is a real read-only MCP tool.
// The query package cannot import mcp (mcp imports query), so query exports
// PlaybookToolNames and this test in the mcp package closes the loop against the
// authoritative ReadOnlyTools registry.
func TestQueryPlaybookToolsExistInRegistry(t *testing.T) {
	t.Parallel()

	registry := make(map[string]struct{})
	for _, tool := range ReadOnlyTools() {
		registry[tool.Name] = struct{}{}
	}

	names := query.PlaybookToolNames()
	if len(names) == 0 {
		t.Fatal("query.PlaybookToolNames returned no names")
	}
	for _, name := range names {
		if _, ok := registry[name]; !ok {
			t.Errorf("playbook references tool %q that is not a read-only MCP tool", name)
		}
	}
}

func TestInvestigationWorkflowToolsExistInRegistry(t *testing.T) {
	t.Parallel()

	registry := make(map[string]struct{})
	for _, tool := range ReadOnlyTools() {
		registry[tool.Name] = struct{}{}
	}

	names := query.InvestigationWorkflowToolNames()
	if len(names) == 0 {
		t.Fatal("query.InvestigationWorkflowToolNames returned no names")
	}
	for _, name := range names {
		if _, ok := registry[name]; !ok {
			t.Errorf("investigation workflow references tool %q that is not a read-only MCP tool", name)
		}
	}
}
