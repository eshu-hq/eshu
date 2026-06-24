// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"os"
	"strings"
	"testing"
)

func TestMCPToolContractMatrixCoversReadOnlyTools(t *testing.T) {
	t.Parallel()

	markdown, err := os.ReadFile("../../../docs/public/reference/mcp-tool-contract-matrix.md")
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

func TestMCPPromptEpicDocsDoNotAdvertiseClosedGaps(t *testing.T) {
	t.Parallel()

	staleClaims := map[string][]string{
		"../../../docs/public/reference/mcp-tool-contract-matrix.md": {
			"class hierarchy/overrides remain tracked by #291",
		},
		"../../../docs/public/reference/mcp-prompt-surface-audit.md": {
			"| Recursive and hub-function prompts | None yet | Tracked by #360 |",
			"Keep recursive and hub-function prompts quarantined to #360",
		},
	}

	for path, claims := range staleClaims {
		path, claims := path, claims
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			content := string(raw)
			for _, claim := range claims {
				if containsNormalizedText(content, claim) {
					t.Fatalf("%s still advertises closed MCP gap: %s", path, claim)
				}
			}
		})
	}

	raw, err := os.ReadFile("../../../docs/public/reference/mcp-tool-contract-matrix.md")
	if err != nil {
		t.Fatalf("read MCP tool contract matrix: %v", err)
	}
	assertPromptAuditRemainingTrackedWorkExcludesClosedIssues(t, string(raw))
}

func TestContainsNormalizedTextMatchesLineWrappedClaims(t *testing.T) {
	t.Parallel()

	haystack := "Security prompts remain\n deliberately unsolved by raw Cypher"
	needle := "Security prompts remain deliberately unsolved by raw Cypher"
	if !containsNormalizedText(haystack, needle) {
		t.Fatal("containsNormalizedText() missed a line-wrapped stale claim")
	}
}

func TestMarkdownTableRowsSplitsCells(t *testing.T) {
	t.Parallel()

	rows := markdownTableRows("| A | B | C |\n| --- | --- | --- |\n| x | y | z |\n")
	if len(rows) != 2 {
		t.Fatalf("markdownTableRows() row count = %d, want 2", len(rows))
	}
	if got, want := rows[1][2], "z"; got != want {
		t.Fatalf("markdownTableRows()[1][2] = %q, want %q", got, want)
	}
}

func assertPromptAuditRemainingTrackedWorkExcludesClosedIssues(t *testing.T, markdown string) {
	t.Helper()

	closedIssuesByPromptFamily := map[string][]string{
		"Cross-repo service story, onboarding, runbooks":    {"#285"},
		"Symbol discovery and implementation lookup":        {"#287"},
		"Broad code-topic and implementation investigation": {"#286"},
		"Callers, callees, imports, call chains":            {"#288"},
		"Dead code and code quality":                        {"#289"},
		"Security hardcoded secrets":                        {"#292"},
		"Deployment, GitOps, and resource tracing":          {"#293", "#294", "#295"},
		"Environment comparison":                            {"#296"},
		"Runtime and indexing status prompts":               {"#297"},
		"Documentation/confluence prompts":                  {"#298"},
		"Structural code inventory":                         {"#362"},
	}
	rowsByFamily := map[string][]string{}
	for _, row := range markdownTableRows(markdown) {
		if len(row) < 4 || row[0] == "Prompt family from docs" {
			continue
		}
		rowsByFamily[row[0]] = row
	}
	for family, closedIssues := range closedIssuesByPromptFamily {
		row, ok := rowsByFamily[family]
		if !ok {
			t.Fatalf("prompt-family table missing row for %q", family)
		}
		remainingTrackedWork := row[3]
		for _, issue := range closedIssues {
			if strings.Contains(remainingTrackedWork, issue) {
				t.Fatalf("prompt-family row %q still advertises closed issue %s as remaining work: %s", family, issue, remainingTrackedWork)
			}
		}
	}
}

func containsNormalizedText(haystack, needle string) bool {
	return strings.Contains(normalizeWhitespace(haystack), normalizeWhitespace(needle))
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func markdownTableRows(markdown string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
			continue
		}
		trimmed = strings.Trim(trimmed, "|")
		cells := strings.Split(trimmed, "|")
		for idx := range cells {
			cells[idx] = strings.TrimSpace(cells[idx])
		}
		if len(cells) > 0 && strings.Trim(cells[0], "-: ") == "" {
			continue
		}
		rows = append(rows, cells)
	}
	return rows
}
