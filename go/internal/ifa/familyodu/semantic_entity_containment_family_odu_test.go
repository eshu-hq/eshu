// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// TestSemanticEntityContainmentCanonicalEntityIDLiterals independently
// reproduces (rather than trusts hand-typed) the three canonical-owned uid
// literals pinned in semantic_entity_containment_family_odu.go, mirroring
// TestSymbolRuntimeFamilyCanonicalEntityIDLiterals. The projector re-derives
// the graph uid from the content_entity fact's name/path/line, ignoring the
// fact's entity_id, so a literal that drifts from this derivation makes the
// NornicDB MATCH a silent no-op even though the pure extractor still
// (correctly) derives the row (#8359).
func TestSemanticEntityContainmentCanonicalEntityIDLiterals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		relativePath string
		entityType   string
		entityName   string
		startLine    int
		want         string
	}{
		{
			name:         "go function",
			relativePath: "pkg/handle.go",
			entityType:   "Function",
			entityName:   "Handle",
			startLine:    10,
			want:         semanticEntityContainmentGoFuncUID,
		},
		{
			name:         "python function",
			relativePath: "app/fetch.py",
			entityType:   "Function",
			entityName:   "fetch",
			startLine:    3,
			want:         semanticEntityContainmentPyFuncUID,
		},
		{
			name:         "annotation",
			relativePath: "app/fetch.py",
			entityType:   "Annotation",
			entityName:   "route",
			startLine:    2,
			want:         semanticEntityContainmentAnnotationUID,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := content.CanonicalEntityID(
				semanticEntityContainmentFamilyRepoID,
				tc.relativePath,
				tc.entityType,
				tc.entityName,
				tc.startLine,
			)
			if got != tc.want {
				t.Errorf("canonical uid = %q, want %q", got, tc.want)
			}
		})
	}
}
