package mcp

import (
	"os"
	"strings"
	"testing"
)

func TestMCPCookbookKeepsRawCypherDiagnosticsOnly(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../../docs/docs/reference/mcp-cookbook.md")
	if err != nil {
		t.Fatalf("ReadFile(mcp-cookbook.md) error = %v, want nil", err)
	}
	cookbook := string(raw)
	before, after, ok := strings.Cut(cookbook, "## Advanced Cypher Queries")
	if !ok {
		t.Fatal("mcp-cookbook.md missing Advanced Cypher Queries diagnostics section")
	}
	if strings.Contains(before, "**Tool:** `execute_cypher_query`") {
		t.Fatal("mcp-cookbook.md uses execute_cypher_query before the diagnostics-only section")
	}
	if !strings.Contains(after, "diagnostics-only") {
		t.Fatal("Advanced Cypher Queries section must explicitly mark raw Cypher as diagnostics-only")
	}
	for _, line := range strings.Split(after, "\n") {
		if strings.Contains(line, `"cypher_query"`) && !strings.Contains(line, `"limit"`) {
			t.Fatalf("raw Cypher cookbook example missing explicit limit: %s", line)
		}
	}
}
