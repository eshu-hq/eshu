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
		`"required": ["source", "source_backend", "query", "repo_id", "results", "matches", "count", "limit", "truncated"]`,
		"at least 3 Unicode characters",
		"Requests without repo_id require a supported type",
		"repository, directory, and file types require repo_id",
	} {
		if !strings.Contains(OpenAPISpec(), fragment) {
			t.Fatalf("OpenAPI spec missing global name-search contract %q", fragment)
		}
	}
}
