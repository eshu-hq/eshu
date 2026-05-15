package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

var diagnosticQueryLimitPattern = regexp.MustCompile(`(?i)\bLIMIT\s+\d+\b`)

func TestMCPCookbookKeepsRawCypherDiagnosticsOnly(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../../docs/docs/reference/mcp-cookbook.md")
	if err != nil {
		t.Fatalf("ReadFile(mcp-cookbook.md) error = %v, want nil", err)
	}
	cookbook := string(raw)
	before, after, ok := strings.Cut(cookbook, "## Diagnostic Cypher Queries")
	if !ok {
		t.Fatal("mcp-cookbook.md missing Diagnostic Cypher Queries diagnostics section")
	}
	if strings.Contains(before, "**Tool:** `execute_cypher_query`") {
		t.Fatal("mcp-cookbook.md uses execute_cypher_query before the diagnostics-only section")
	}
	if !strings.Contains(after, "diagnostics-only") {
		t.Fatal("Diagnostic Cypher Queries section must explicitly mark raw Cypher as diagnostics-only")
	}
	if err := validateDiagnosticCypherBlocks(after); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDiagnosticCypherBlocksAcceptsMultilineJSONWithLimit(t *testing.T) {
	t.Parallel()

	diagnostics := "## Diagnostic Cypher Queries\n\n```json\n{\n  \"cypher_query\": \"MATCH (n) RETURN n\",\n  \"limit\": 25\n}\n```\n"

	if err := validateDiagnosticCypherBlocks(diagnostics); err != nil {
		t.Fatalf("validateDiagnosticCypherBlocks() error = %v, want nil", err)
	}
}

func TestMCPCookbookToolReferencesAreRegistered(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../../docs/docs/reference/mcp-cookbook.md")
	if err != nil {
		t.Fatalf("ReadFile(mcp-cookbook.md) error = %v, want nil", err)
	}
	registered := map[string]bool{}
	for _, tool := range ReadOnlyTools() {
		registered[tool.Name] = true
	}
	for _, tool := range cookbookToolReferences(string(raw)) {
		if !registered[tool] {
			t.Fatalf("mcp-cookbook.md references unregistered MCP tool %q", tool)
		}
	}
}

func cookbookToolReferences(markdown string) []string {
	pattern := regexp.MustCompile(`\*\*Tool:\*\* ` + "`" + `([^` + "`" + `]+)` + "`")
	matches := pattern.FindAllStringSubmatch(markdown, -1)
	tools := make([]string, 0, len(matches))
	for _, match := range matches {
		tools = append(tools, match[1])
	}
	return tools
}

func validateDiagnosticCypherBlocks(markdown string) error {
	for _, block := range jsonFenceBlocks(markdown) {
		if !strings.Contains(block, `"cypher_query"`) {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(block), &payload); err != nil {
			return fmt.Errorf("diagnostic Cypher JSON block is invalid: %w", err)
		}
		if _, ok := payload["limit"]; !ok {
			return fmt.Errorf("diagnostic Cypher JSON block missing top-level limit: %s", oneLine(block))
		}
		query, _ := payload["cypher_query"].(string)
		if diagnosticQueryLimitPattern.MatchString(query) {
			return fmt.Errorf("diagnostic Cypher query should rely on tool-level limit, not in-query LIMIT: %s", oneLine(block))
		}
	}
	return nil
}

func jsonFenceBlocks(markdown string) []string {
	var blocks []string
	var builder strings.Builder
	inJSON := false
	for _, line := range strings.Split(markdown, "\n") {
		switch {
		case strings.TrimSpace(line) == "```json" && !inJSON:
			inJSON = true
			builder.Reset()
		case strings.TrimSpace(line) == "```" && inJSON:
			inJSON = false
			blocks = append(blocks, builder.String())
		case inJSON:
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
	}
	return blocks
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
