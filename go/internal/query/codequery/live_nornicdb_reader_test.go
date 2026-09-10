// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// liveNornicDBReader is the test-only live-backend GraphQuery for the code
// family's NornicDB compatibility proofs (live_nornicdb_* tag-gated files and
// the env-skipped route-to-caller proof). Production serves these reads
// through root package query's Neo4jReader, which codequery cannot import
// without a cycle; this helper opens a read session per call and decodes rows
// with the same Keys/Values mapping. It deliberately carries none of the
// production read policy (deadlines, retries, telemetry): the proofs pin
// backend-compatible query TEXT, not the policy layer.
type liveNornicDBReader struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

// newLiveNornicDBReader constructs a live-backend GraphQuery for tests that
// prove query text against a real NornicDB build.
func newLiveNornicDBReader(driver neo4jdriver.DriverWithContext, database string) *liveNornicDBReader {
	return &liveNornicDBReader{driver: driver, database: database}
}

// Run executes a read-only Cypher query and returns results as maps.
func (r *liveNornicDBReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.database,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("live nornicdb query: %w", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("live nornicdb collect: %w", err)
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

// RunSingle executes a Cypher query expecting at most one result row.
func (r *liveNornicDBReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}
