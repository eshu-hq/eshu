// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

import (
	"context"
	"fmt"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
)

// liveExecutor adapts a Bolt driver to the storage/cypher.Executor and
// storage/cypher.GroupExecutor seams plus graph.CypherExecutor (schema DDL) and
// read helpers for graph-truth read-back. It is the same driver-backed adapter
// shape proven by the secrets/IAM live conformance test; nothing here is a fake
// — every call runs real Cypher over a real session.
//
// The canonical node writer is NOT driven through liveExecutor directly. On
// NornicDB the production projector wraps the driver in a phase-group executor
// (each canonical write phase is its own transaction) rather than one atomic
// group across all phases, because NornicDB does not give a same-transaction
// UNWIND-driven MATCH read-your-writes against nodes MERGE'd earlier in that
// transaction once the schema's uid lookup indexes exist. Driving the writer
// through the full-atomic GroupExecutor path would silently drop the directory
// CONTAINS edges (the #4019 bug class). The tier therefore drives the writer
// through the shared storage/nornicdb PhaseGroupExecutor below: bounded
// per-phase ExecuteGroup transactions over this same driver.
type liveExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

// livePhaseGroupExecutor returns the exact production NornicDB adapter with
// production defaults over the live Bolt executor. The returned type exposes
// PhaseGroupExecutor but not GroupExecutor, so the tier cannot silently drift
// back to a private mirror or whole-materialization transaction.
func livePhaseGroupExecutor(inner liveExecutor) storagenornicdb.PhaseGroupExecutor {
	return storagenornicdb.PhaseGroupExecutor{
		Inner:                    inner,
		MaxStatements:            storagenornicdb.DefaultPhaseGroupStatements,
		DirectoryMaxStatements:   storagenornicdb.DefaultDirectoryPhaseStatements,
		FileMaxStatements:        storagenornicdb.DefaultFilePhaseStatements,
		EntityMaxStatements:      storagenornicdb.DefaultEntityPhaseStatements,
		EntityLabelMaxStatements: storagenornicdb.DefaultEntityLabelPhaseStatements(storagenornicdb.DefaultEntityPhaseStatements),
		EntityPhaseConcurrency:   storagenornicdb.DefaultEntityPhaseConcurrency(),
		DrainReader:              inner,
		RetractBatchSize:         storagenornicdb.DefaultCanonicalRetractBatchSize,
	}
}

// Execute runs one write statement in its own auto-commit session.
func (e liveExecutor) Execute(ctx context.Context, stmt cypher.Statement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return fmt.Errorf("execute write: %w", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		return fmt.Errorf("consume write: %w", err)
	}
	return nil
}

// ExecuteGroup runs a batch of statements in a single managed write transaction.
// The canonical node writer routes through this path, so the directory edge
// phase MATCHes the directory nodes MERGE'd earlier in the same group — exactly
// the NornicDB single-label read-your-writes behavior the #4019 fix relies on.
func (e liveExecutor) ExecuteGroup(ctx context.Context, stmts []cypher.Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
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
		return fmt.Errorf("execute write group: %w", err)
	}
	return nil
}

// RunWrite executes one bounded retract-drain iteration and returns its rows
// and delete counters through the production NornicDB DrainReader contract.
func (e liveExecutor) RunWrite(
	ctx context.Context,
	cypherText string,
	parameters map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypherText, parameters)
	if err != nil {
		return storagenornicdb.DrainWriteResult{}, fmt.Errorf("execute drain write: %w", err)
	}
	rows := make([]map[string]any, 0)
	for result.Next(ctx) {
		record := result.Record()
		row := make(map[string]any, len(record.Keys))
		for _, key := range record.Keys {
			value, _ := record.Get(key)
			row[key] = value
		}
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		return storagenornicdb.DrainWriteResult{}, fmt.Errorf("iterate drain write: %w", err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return storagenornicdb.DrainWriteResult{}, fmt.Errorf("consume drain write: %w", err)
	}
	return storagenornicdb.DrainWriteResult{
		Rows:                 rows,
		NodesDeleted:         int64(summary.Counters().NodesDeleted()),
		RelationshipsDeleted: int64(summary.Counters().RelationshipsDeleted()),
	}, nil
}

// ExecuteCypher satisfies graph.CypherExecutor so EnsureSchemaWithBackendStrict
// can apply the real schema DDL through the same driver.
func (e liveExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	return e.Execute(ctx, cypher.Statement{Cypher: stmt.Cypher, Parameters: stmt.Parameters})
}

// count runs a single-row COUNT query and returns the int64 value.
func (e liveExecutor) count(ctx context.Context, cypherText string, params map[string]any) (int64, error) {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypherText, params)
	if err != nil {
		return 0, fmt.Errorf("run count query: %w", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("collect count query: %w", err)
	}
	value, ok := record.Values[0].(int64)
	if !ok {
		return 0, fmt.Errorf("count value is not int64: %T", record.Values[0])
	}
	return value, nil
}
