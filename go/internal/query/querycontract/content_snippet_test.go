// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestFileContentSnippetFieldsOmittedWhenEmpty pins the wire contract for
// the 05a search-snippet fields: a FileContent from any non-search producer
// (get_file_content, get_file_lines, listing) marshals byte-identical to
// before, while a search row carries match_snippet/match_line.
func TestFileContentSnippetFieldsOmittedWhenEmpty(t *testing.T) {
	t.Parallel()

	plain, err := json.Marshal(FileContent{
		RepoID:       "repo-a",
		RelativePath: "a/b.go",
		CommitSHA:    "sha",
		Content:      "body",
		ContentHash:  "hash",
		LineCount:    10,
		Language:     "go",
	})
	if err != nil {
		t.Fatalf("marshal plain FileContent: %v", err)
	}
	if strings.Contains(string(plain), "match_snippet") || strings.Contains(string(plain), "match_line") {
		t.Fatalf("non-search FileContent leaks snippet fields: %s", plain)
	}

	snippet, err := json.Marshal(FileContent{
		RepoID:       "repo-a",
		RelativePath: "a/b.go",
		ContentHash:  "hash",
		LineCount:    10,
		MatchSnippet: "needle in a haystack",
		MatchLine:    3,
	})
	if err != nil {
		t.Fatalf("marshal snippet FileContent: %v", err)
	}
	if !strings.Contains(string(snippet), `"match_snippet":"needle in a haystack"`) {
		t.Errorf("search FileContent lost match_snippet: %s", snippet)
	}
	if !strings.Contains(string(snippet), `"match_line":3`) {
		t.Errorf("search FileContent lost match_line: %s", snippet)
	}
}

// TestFileSearchSnippetPageByteBudget is the 05a egress proof at the
// contract layer: a limit-200 page whose every row carries the worst-case
// snippet (120 four-byte runes) plus its line must fit the 256 KiB rendered
// budget in both the plain results shape and the enveloped response shape.
func TestFileSearchSnippetPageByteBudget(t *testing.T) {
	t.Parallel()

	const budget = 262144
	worst := strings.Repeat("\U0001F600", 120)
	rows := make([]FileContent, 0, 200)
	for i := 0; i < 200; i++ {
		rows = append(rows, FileContent{
			RepoID:       "repo-a",
			RelativePath: fmt.Sprintf("src/file-%04d.go", i),
			ContentHash:  "hash",
			LineCount:    100,
			Language:     "go",
			MatchSnippet: worst,
			MatchLine:    i + 1,
		})
	}
	plain, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal plain page: %v", err)
	}
	if len(plain) > budget {
		t.Errorf("plain limit-200 snippet page = %d bytes, want <= %d", len(plain), budget)
	}
	enveloped, err := json.Marshal(map[string]any{
		"results":        rows,
		"count":          len(rows),
		"limit":          200,
		"offset":         0,
		"truncated":      false,
		"source_backend": "postgres_content_store",
	})
	if err != nil {
		t.Fatalf("marshal enveloped page: %v", err)
	}
	if len(enveloped) > budget {
		t.Errorf("enveloped limit-200 snippet page = %d bytes, want <= %d", len(enveloped), budget)
	}
	t.Logf("limit-200 worst-case snippet page: plain %d bytes, enveloped %d bytes (budget %d)", len(plain), len(enveloped), budget)
}
