// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// TestECSTaskReadIndexMigrationsStayConcurrent protects fact ingestion while
// each new read index is built on a populated fact_records table. A sole
// concurrent statement is the migration runner's autocommit classification.
func TestECSTaskReadIndexMigrationsStayConcurrent(t *testing.T) {
	for _, tc := range []struct {
		migration string
		index     string
		key       string
		predicate string
	}{
		{"ecs_runtime_service_arn_index", "fact_records_ecs_service_arn_idx", "payload->>'arn'", "payload->>'resource_type' = 'aws_ecs_service'"},
		{"ecs_runtime_task_scope_index", "fact_records_ecs_task_scope_cluster_group_idx", "scope_id, generation_id", "payload->>'resource_type' = 'aws_ecs_task'"},
	} {
		t.Run(tc.migration, func(t *testing.T) {
			if strings.Contains(factRecordSchemaSQL, tc.index) {
				t.Fatalf("%s is in EnsureSchema DDL; populated builds must be migration-only", tc.index)
			}
			statement := MigrationSQL(tc.migration)
			if !coordination.IsSoleConcurrentIndexStatement(statement) {
				t.Fatalf("migration %s is not one concurrent index statement: %s", tc.migration, statement)
			}
			for _, fragment := range []string{"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + tc.index, tc.key, tc.predicate, "is_tombstone = FALSE"} {
				if !strings.Contains(statement, fragment) {
					t.Fatalf("migration %s missing %q: %s", tc.migration, fragment, statement)
				}
			}
		})
	}
}
