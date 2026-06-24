// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestResolvedRelationshipEvidenceTypeMapsGCPCloudRelationship(t *testing.T) {
	t.Parallel()

	relationship := relationships.ResolvedRelationship{
		Details: map[string]any{
			"evidence_kinds": []any{string(relationships.EvidenceKindGCPCloudRelationship)},
		},
	}

	got := resolvedRelationshipEvidenceType(relationship)
	if got != "gcp_cloud_relationship" {
		t.Fatalf("evidence type = %q, want gcp_cloud_relationship", got)
	}
}
