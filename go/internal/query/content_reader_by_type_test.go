// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

// TestListRepoEntitiesByTypeOrdersByEntityIDTiebreaker proves the ORDER BY
// clause carries entity_id as a tiebreaker after relative_path, start_line
// (#5343 review P2: one-token determinism hardening). Without it, which rows
// land past a truncated LIMIT (see fetchK8sResourceCandidates in
// content_relationships.go) is unspecified among rows sharing a
// relative_path/start_line -- this makes the drop reproducible.
func TestListRepoEntitiesByTypeOrdersByEntityIDTiebreaker(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"deployment-1", "repo-1", "deploy/deployment.yaml", "K8sResource", "demo",
					int64(1), int64(20), "yaml", "", []byte(`{}`),
				},
			},
			queryContainsInOrder: []string{
				"FROM content_entities",
				"WHERE repo_id = $1 AND entity_type = $2",
				"ORDER BY relative_path, start_line, entity_id",
				"LIMIT $3",
			},
		},
	})

	reader := NewContentReader(db)
	if _, err := reader.ListRepoEntitiesByType(context.Background(), "repo-1", "K8sResource", 10); err != nil {
		t.Fatalf("ListRepoEntitiesByType() error = %v, want nil", err)
	}
}
