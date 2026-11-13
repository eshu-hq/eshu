// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestOpenAPIRepoStoryEvidenceDetailNamesFamilies pins the repository story
// evidence_detail contract (#10605): every row family
// ApplyRepoStoryEvidenceDetail projects is named in the OpenAPI description,
// the MCP default is handles, and unknown values are a 400. Ranging over
// RepoStoryHandleListFamilies means a family added to the list cannot ship
// undescribed; keep the list in step with the families the shaper projects.
func TestOpenAPIRepoStoryEvidenceDetailNamesFamilies(t *testing.T) {
	t.Parallel()

	spec := OpenAPISpec()
	for _, family := range querycontract.RepoStoryHandleListFamilies() {
		if !strings.Contains(spec, family) {
			t.Fatalf("OpenAPI spec missing repo story handles family %q", family)
		}
	}
	for _, fragment := range []string{
		"The MCP get_repo_story tool defaults to handles.",
		"Unknown values return 400 invalid_argument.",
		`"evidence_detail"`,
		`"full", "handles"`,
	} {
		if !strings.Contains(spec, fragment) {
			t.Fatalf("OpenAPI spec missing repo story evidence_detail contract %q", fragment)
		}
	}
}
