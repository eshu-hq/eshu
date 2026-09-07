// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestOpenAPIRepositoryListDocumentsBoundedGraphReadFailures lives in root
// (not the repository family package) because it covers the root OpenAPI
// spec surface, which the family package cannot import.
func TestOpenAPIRepositoryListDocumentsBoundedGraphReadFailures(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/repositories")
	get := querytestutil.MustMapField(t, path, "get")
	responses := querytestutil.MustMapField(t, get, "responses")
	for _, status := range []string{"503", "504"} {
		if _, ok := responses[status]; !ok {
			t.Errorf("repository-list OpenAPI responses missing %s bounded graph-read response", status)
		}
	}
}
