// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"fmt"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// provenanceReplayExecutor is a test-only Bolt adapter for the real writer.
// Execute is auto-commit, ExecuteGroup is managed, and readRows verifies graph
// truth without substituting an in-memory graph.
type provenanceReplayExecutor struct {
	driver          neo4jdriver.DriverWithContext
	database        string
	bookmarkManager neo4jdriver.BookmarkManager
}

func newProvenanceReplayExecutor(
	driver neo4jdriver.DriverWithContext,
	database string,
) provenanceReplayExecutor {
	return provenanceReplayExecutor{
		driver:          driver,
		database:        database,
		bookmarkManager: neo4jdriver.NewBookmarkManager(neo4jdriver.BookmarkManagerConfig{}),
	}
}

func (e provenanceReplayExecutor) sessionConfig(mode neo4jdriver.AccessMode) neo4jdriver.SessionConfig {
	return neo4jdriver.SessionConfig{
		AccessMode: mode, DatabaseName: e.database, BookmarkManager: e.bookmarkManager,
	}
}

// Execute runs retracts and setup statements as auto-commit transactions.
func (e provenanceReplayExecutor) Execute(ctx context.Context, stmt cypher.Statement) error {
	session := e.driver.NewSession(ctx, e.sessionConfig(neo4jdriver.AccessModeWrite))
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return fmt.Errorf("execute auto-commit statement: %w", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		return fmt.Errorf("consume auto-commit statement: %w", err)
	}
	return nil
}

// ExecuteGroup runs upserts in the managed transaction selected by the real
// writer when its executor implements cypher.GroupExecutor.
func (e provenanceReplayExecutor) ExecuteGroup(ctx context.Context, stmts []cypher.Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	session := e.driver.NewSession(ctx, e.sessionConfig(neo4jdriver.AccessModeWrite))
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		for _, stmt := range stmts {
			result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if runErr != nil {
				return nil, runErr
			}
			if _, consumeErr := result.Consume(ctx); consumeErr != nil {
				return nil, consumeErr
			}
		}
		return nil, nil
	})
	if err != nil {
		return fmt.Errorf("execute managed statement group: %w", err)
	}
	return nil
}

func (e provenanceReplayExecutor) readRows(
	ctx context.Context,
	query string,
	params map[string]any,
) ([]map[string]any, error) {
	session := e.driver.NewSession(ctx, e.sessionConfig(neo4jdriver.AccessModeRead))
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("run graph-truth read: %w", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect graph-truth read: %w", err)
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for index, key := range record.Keys {
			row[key] = record.Values[index]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (e provenanceReplayExecutor) count(
	ctx context.Context,
	query string,
	params map[string]any,
) (int64, error) {
	rows, err := e.readRows(ctx, query, params)
	if err != nil {
		return 0, err
	}
	if len(rows) != 1 {
		return 0, fmt.Errorf("count query rows = %d, want one", len(rows))
	}
	count, ok := rows[0]["count"].(int64)
	if !ok {
		return 0, fmt.Errorf("count query value has type %T, want int64", rows[0]["count"])
	}
	return count, nil
}
