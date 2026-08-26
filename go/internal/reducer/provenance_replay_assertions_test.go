// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"fmt"
	"testing"
)

func readProvenanceReplayDerivedFrom(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
	digest string,
	baseDigest string,
	scopeID string,
) []map[string]any {
	t.Helper()
	rows, err := executor.readRows(ctx, `MATCH (:ContainerImage {digest: $digest})-[rel:DERIVED_FROM]->(:ContainerImage {digest: $base_digest})
WHERE rel.scope_id = $scope_id
RETURN rel.scope_id AS scope_id, rel.generation_id AS generation_id,
       rel.evidence_source AS evidence_source, rel.evidence_kinds AS evidence_kinds,
       rel.attribution_basis AS attribution_basis, rel.source_tool AS source_tool`,
		map[string]any{"digest": digest, "base_digest": baseDigest, "scope_id": scopeID})
	if err != nil {
		t.Fatalf("read DERIVED_FROM graph truth: %v", err)
	}
	return rows
}

func assertProvenanceReplayRelationship(t *testing.T, rows []map[string]any, want map[string]any) {
	t.Helper()
	if len(rows) != 1 {
		t.Fatalf("relationship rows = %#v, want exactly one", rows)
	}
	for key, expected := range want {
		actual := rows[0][key]
		if key == "evidence_kinds" {
			if !provenanceReplayEvidenceContains(actual, expected.(string)) {
				t.Errorf("%s = %#v, want single token %q", key, actual, expected)
			}
			continue
		}
		if actual != expected {
			t.Errorf("%s = %#v, want %#v", key, actual, expected)
		}
	}
}

func provenanceReplayEvidenceContains(value any, want string) bool {
	switch typed := value.(type) {
	case []any:
		return len(typed) == 1 && fmt.Sprint(typed[0]) == want
	case []string:
		return len(typed) == 1 && typed[0] == want
	default:
		return false
	}
}
