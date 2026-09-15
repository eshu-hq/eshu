// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
)

// CypherGroupStatement is one statement in an atomic materializer write group.
type CypherGroupStatement struct {
	Cypher     string
	Parameters map[string]any
}

// CypherGroupExecutor executes all statements in one atomic graph transaction.
type CypherGroupExecutor interface {
	ExecuteCypherGroup(context.Context, []CypherGroupStatement) error
}

const batchRuntimePlatformRunsOnLegacyIdentityCleanupCypher = `UNWIND $rows AS row
MATCH (i:WorkloadInstance {id: row.instance_id})
MATCH (p:Platform {id: row.platform_id})
MATCH (i)-[rel:RUNS_ON]->(p)
WHERE rel.identity_key IS NULL
DELETE rel`

func (m *WorkloadMaterializer) executeBatchedGroup(
	ctx context.Context,
	queries []string,
	rows []map[string]any,
) error {
	executor, ok := m.executor.(CypherGroupExecutor)
	if !ok {
		return fmt.Errorf("atomic Cypher group executor is required")
	}
	batchSize := m.batchSize()
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		params := map[string]any{"rows": rows[start:end]}
		statements := make([]CypherGroupStatement, 0, len(queries))
		for _, query := range queries {
			statements = append(statements, CypherGroupStatement{
				Cypher:     query,
				Parameters: params,
			})
		}
		if err := executor.ExecuteCypherGroup(ctx, statements); err != nil {
			return err
		}
	}
	return nil
}
