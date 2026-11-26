// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestOpenAPIDocumentsGlobalNameSearchBounds(t *testing.T) {
	t.Parallel()
	for _, fragment := range []string{
		`"exact": {"type": "boolean"`,
		`"limit": {"type": "integer", "description": "Maximum returned page size (default 50, maximum 200)", "default": 50, "minimum": 1, "maximum": 200}`,
		`"required": ["source", "source_backend", "query", "repo_id", "results", "count", "limit", "truncated"]`,
		"at least 3 Unicode characters",
		"Omitting type with no repo_id searches every kind by exact name",
		"repository, directory, and file types require repo_id",
	} {
		if !strings.Contains(OpenAPISpec(), fragment) {
			t.Fatalf("OpenAPI spec missing global name-search contract %q", fragment)
		}
	}
}

// TestOpenAPIDoesNotDocumentRemovedMatchesAlias pins #7170: the `matches` alias
// of `results` is gone from every producer, so no single schema may carry
// both keys side by side, and the compatibility-alias wording must not come
// back. A standalone `matches` key is fine: the dockerfile-installs search
// route genuinely emits `matches` rows from writeDockerfileInstallSearch, so
// the ban targets the alias shape, not the word.
func TestOpenAPIDoesNotDocumentRemovedMatchesAlias(t *testing.T) {
	t.Parallel()
	spec := OpenAPISpec()
	if strings.Contains(spec, "Compatibility alias for results.") {
		t.Fatalf("OpenAPI spec still contains %q; the matches alias was removed in #7170", "Compatibility alias for results.")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(spec), &doc); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	var bad []string
	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch typed := node.(type) {
		case map[string]any:
			if props, ok := typed["properties"].(map[string]any); ok {
				_, hasResults := props["results"]
				_, hasMatches := props["matches"]
				if hasResults && hasMatches {
					bad = append(bad, path)
				}
			}
			for key, value := range typed {
				walk(value, path+"/"+key)
			}
		case []any:
			for i, value := range typed {
				walk(value, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	walk(doc, "$")
	if len(bad) > 0 {
		t.Fatalf("OpenAPI spec reintroduces the #7170 matches/results alias pair at: %v", bad)
	}
}
