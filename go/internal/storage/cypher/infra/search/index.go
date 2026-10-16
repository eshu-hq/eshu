// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// indexStateCypher reads the state of the infra search key index; no row
// means the index does not exist.
const indexStateCypher = "SHOW INDEXES YIELD name, state WHERE name = $name RETURN state"

// IndexState returns the Neo4j state of graph.InfraSearchKeyIndexName
// (ONLINE, POPULATING, FAILED, ...), or "" when the index does not exist.
// The read cutover (#7245 PR-2) trusts the index only while it is ONLINE.
// Neo4j only.
func IndexState(ctx context.Context, runner Runner) (string, error) {
	rows, err := runner.Run(ctx, indexStateCypher, map[string]any{"name": graph.InfraSearchKeyIndexName})
	if err != nil {
		return "", fmt.Errorf("read %s state: %w", graph.InfraSearchKeyIndexName, err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	state, _ := rows[0]["state"].(string)
	return state, nil
}
