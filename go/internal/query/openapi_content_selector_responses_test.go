// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestOpenAPISpecContentSelectorRoutesDocumentSelectorStatuses pins the
// statuses the content read and search routes write (#7626): an unmatched
// repository selector or a missing file or entity answers 404, and a reader
// fence answers 503, so each route must declare both.
func TestOpenAPISpecContentSelectorRoutesDocumentSelectorStatuses(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	for _, route := range []string{
		"/api/v0/content/files/read",
		"/api/v0/content/files/lines",
		"/api/v0/content/entities/read",
		"/api/v0/content/files/search",
		"/api/v0/content/entities/search",
	} {
		operation := testutil.MustMapField(t, testutil.MustMapField(t, paths, route), "post")
		responses := testutil.MustMapField(t, operation, "responses")
		for _, status := range []string{"404", "503"} {
			if _, ok := responses[status]; !ok {
				t.Errorf("%s responses missing %s", route, status)
			}
		}
	}
}
