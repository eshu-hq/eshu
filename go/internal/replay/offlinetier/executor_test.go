// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

import (
	"context"
	"fmt"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
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
// through livePhaseGroupExecutor below, which mirrors the production NornicDB
// phase-group path: per-phase ExecuteGroup transactions over this same driver.
type liveExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

// livePhaseGroupExecutor mirrors the production NornicDB phase-group write path
// (cmd/ingester nornicDBPhaseGroupExecutor): it exposes Execute and
// ExecutePhaseGroup — but NOT ExecuteGroup — so storage/cypher.CanonicalNodeWriter
// routes through its PhaseGroupExecutor branch, running each canonical phase as
// its own transaction. Each phase is dispatched to the inner liveExecutor's real
// ExecuteGroup, so directory nodes commit before the directory-edge phase MATCHes
// them, exactly as in production. This is the real backend write path; it is not
// a fake.
type livePhaseGroupExecutor struct {
	inner liveExecutor
}

// Execute runs a singleton statement through the inner driver-backed executor.
func (e livePhaseGroupExecutor) Execute(ctx context.Context, stmt cypher.Statement) error {
	return e.inner.Execute(ctx, stmt)
}

// ExecutePhaseGroup runs one canonical write phase as a single real transaction.
// It does NOT span phases, which is the production NornicDB contract the #4019
// directory phase-split depends on.
func (e livePhaseGroupExecutor) ExecutePhaseGroup(ctx context.Context, stmts []cypher.Statement) error {
	return e.inner.ExecuteGroup(ctx, stmts)
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
