// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// The Neo4j adapter behind the projector's canonical writer. Neo4j is
// compatibility only -- NornicDB is the default canonical graph backend -- so
// this adapter is kept out of runtime_wiring.go, which wires the backend the
// projector actually runs on.

import (
	"context"
	"errors"
	"fmt"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph/capture"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type projectorNeo4jExecutor struct {
	Driver       neo4jdriver.DriverWithContext
	DatabaseName string
	TxTimeout    time.Duration
	Instruments  *telemetry.Instruments
}

func (e projectorNeo4jExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if e.Driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}
	if len(stmts) == 0 {
		return nil
	}

	session := e.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.DatabaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	rawCounts, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		counts := make([]sourcecypher.StatementRetractionCounts, 0, len(stmts))
		for _, stmt := range stmts {
			result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if runErr != nil {
				return nil, runErr
			}
			summary, consumeErr := result.Consume(ctx)
			if consumeErr != nil {
				return nil, consumeErr
			}
			counts = append(counts, statementRetractionCounts(stmt, summary))
		}
		return counts, nil
	}, e.transactionConfigurers()...)
	if err != nil {
		return err
	}
	if counts, ok := rawCounts.([]sourcecypher.StatementRetractionCounts); ok {
		sourcecypher.RecordReconciliationDriftRetractionCounts(ctx, e.Instruments, counts)
	}
	return nil
}

func (e projectorNeo4jExecutor) Execute(ctx context.Context, statement sourcecypher.Statement) error {
	if e.Driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}

	session := e.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.DatabaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, statement.Cypher, statement.Parameters, e.transactionConfigurers()...)
	if err != nil {
		return err
	}
	summary, err := result.Consume(ctx)
	if err == nil {
		sourcecypher.RecordReconciliationDriftRetractions(
			ctx,
			e.Instruments,
			statement,
			int64(summary.Counters().NodesDeleted()),
			int64(summary.Counters().RelationshipsDeleted()),
		)
	}
	return err
}

func (e projectorNeo4jExecutor) RunWrite(
	ctx context.Context,
	cypher string,
	parameters map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	if e.Driver == nil {
		return storagenornicdb.DrainWriteResult{}, fmt.Errorf("neo4j driver is required")
	}
	session := e.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.DatabaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, cypher, parameters, e.transactionConfigurers()...)
	if err != nil {
		return storagenornicdb.DrainWriteResult{}, err
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
		return storagenornicdb.DrainWriteResult{}, err
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return storagenornicdb.DrainWriteResult{}, err
	}
	return storagenornicdb.DrainWriteResult{
		Rows:                 rows,
		NodesDeleted:         int64(summary.Counters().NodesDeleted()),
		RelationshipsDeleted: int64(summary.Counters().RelationshipsDeleted()),
	}, nil
}

func (e projectorNeo4jExecutor) transactionConfigurers() []func(*neo4jdriver.TransactionConfig) {
	if e.TxTimeout <= 0 {
		return nil
	}
	return []func(*neo4jdriver.TransactionConfig){neo4jdriver.WithTxTimeout(e.TxTimeout)}
}

func statementRetractionCounts(
	statement sourcecypher.Statement,
	summary neo4jdriver.ResultSummary,
) sourcecypher.StatementRetractionCounts {
	counters := summary.Counters()
	return sourcecypher.StatementRetractionCounts{
		Statement:            statement,
		NodesDeleted:         int64(counters.NodesDeleted()),
		RelationshipsDeleted: int64(counters.RelationshipsDeleted()),
	}
}

type projectorNeo4jDriverCloser struct {
	Driver neo4jdriver.DriverWithContext
	// captureSession surfaces a stashed streaming failure at shutdown. It
	// is nil unless differential capture opened a session; records already
	// stream to disk as statements execute, so Close never replays them.
	captureSession *capture.Session
}

func (c projectorNeo4jDriverCloser) Close() error {
	var sessionErr error
	if c.captureSession != nil {
		sessionErr = c.captureSession.Close()
	}
	return errors.Join(sessionErr, closeProjectorNeo4jDriver(c.Driver))
}

func closeProjectorNeo4jDriver(driver neo4jdriver.DriverWithContext) error {
	if driver == nil {
		return nil
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), projectorConnectionTimeout)
	defer cancel()
	return driver.Close(closeCtx)
}
