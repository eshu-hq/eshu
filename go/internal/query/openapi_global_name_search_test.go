// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
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
		"Requests without repo_id require a supported type",
		"repository, directory, and file types require repo_id",
	} {
		if !strings.Contains(OpenAPISpec(), fragment) {
			t.Fatalf("OpenAPI spec missing global name-search contract %q", fragment)
		}
	}
}

// TestOpenAPIDoesNotDocumentRemovedMatchesAlias pins #7170: the `matches` alias
// of `results` is gone from every producer, so the spec must not advertise it
// on any schema or require it on CodeSearchResponse.
func TestOpenAPIDoesNotDocumentRemovedMatchesAlias(t *testing.T) {
	t.Parallel()
	spec := OpenAPISpec()
	for _, fragment := range []string{`"matches"`, "Compatibility alias for results."} {
		if strings.Contains(spec, fragment) {
			t.Fatalf("OpenAPI spec still contains %q; the matches alias was removed in #7170", fragment)
		}
	}
}
