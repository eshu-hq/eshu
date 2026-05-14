package mcp

import (
	"os"
	"strings"
	"testing"
)

func TestMCPToolContractMatrixCoversReadOnlyTools(t *testing.T) {
	t.Parallel()

	markdown, err := os.ReadFile("../../../docs/docs/reference/mcp-tool-contract-matrix.md")
	if err != nil {
		t.Fatalf("read MCP tool contract matrix: %v", err)
	}
	content := string(markdown)
	for _, tool := range ReadOnlyTools() {
		rowMarker := "| `" + tool.Name + "` |"
		if !strings.Contains(content, rowMarker) {
			t.Fatalf("contract matrix missing row for %s", tool.Name)
		}
	}
}
