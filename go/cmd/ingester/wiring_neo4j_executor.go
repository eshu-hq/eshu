// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type ingesterNeo4jExecutor struct {
	Driver                 neo4jdriver.DriverWithContext
	DatabaseName           string
	TxTimeout              time.Duration
	ProfileGroupStatements bool
	Instruments            *telemetry.Instruments
}

func (e ingesterNeo4jExecutor) Execute(ctx context.Context, statement sourcecypher.Statement) error {
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

func (e ingesterNeo4jExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
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
		err := sourcecypher.ExecuteProfiledStatementGroup(ctx, stmts, func(ctx context.Context, stmt sourcecypher.Statement) error {
			result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if runErr != nil {
				return runErr
			}
			summary, consumeErr := result.Consume(ctx)
			if consumeErr != nil {
				return consumeErr
			}
			counts = append(counts, ingesterStatementRetractionCounts(stmt, summary))
			return nil
		}, e.ProfileGroupStatements, nil)
		if err != nil {
			return nil, err
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

func ingesterStatementRetractionCounts(
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

// DrainWriteResult carries the result rows and graph-driver delete counters from
// one bounded drain step. The counters feed reconciliation-drift telemetry in
// executeDrainLoop; they match the counters that Execute/ExecuteGroup capture via
// ResultSummary.Counters() on the non-drain path.
type DrainWriteResult = storagenornicdb.DrainWriteResult

// retractDrainReader executes a bounded drain step in a write session and
// returns the collected records and graph-driver delete counters. It is
// implemented by ingesterNeo4jExecutor and used by nornicDBPhaseGroupExecutor
// to drive the drain loop for unbounded full-refresh DETACH DELETE statements
// on NornicDB.
type retractDrainReader = storagenornicdb.DrainReader

// RunWrite opens a write session, runs the supplied Cypher with the supplied
// parameters, collects all result records and the graph-driver delete counters,
// and returns them. It is used by the bounded drain loop in
// nornicDBPhaseGroupExecutor to read the __drained counter and accumulate
// NodesDeleted/RelationshipsDeleted for reconciliation-drift telemetry.
func (e ingesterNeo4jExecutor) RunWrite(ctx context.Context, cypher string, params map[string]any) (DrainWriteResult, error) {
	if e.Driver == nil {
		return DrainWriteResult{}, fmt.Errorf("neo4j driver is required")
	}

	session := e.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.DatabaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, cypher, params, e.transactionConfigurers()...)
	if err != nil {
		return DrainWriteResult{}, err
	}

	var rows []map[string]any
	for result.Next(ctx) {
		record := result.Record()
		row := make(map[string]any, len(record.Keys))
		for _, key := range record.Keys {
			val, _ := record.Get(key)
			row[key] = val
		}
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		return DrainWriteResult{}, err
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return DrainWriteResult{}, err
	}
	return DrainWriteResult{
		Rows:                 rows,
		NodesDeleted:         int64(summary.Counters().NodesDeleted()),
		RelationshipsDeleted: int64(summary.Counters().RelationshipsDeleted()),
	}, nil
}

func (e ingesterNeo4jExecutor) transactionConfigurers() []func(*neo4jdriver.TransactionConfig) {
	if e.TxTimeout <= 0 {
		return nil
	}
	return []func(*neo4jdriver.TransactionConfig){neo4jdriver.WithTxTimeout(e.TxTimeout)}
}

func canonicalTransactionTimeout(graphBackend runtimecfg.GraphBackend, getenv func(string) string) time.Duration {
	if graphBackend != runtimecfg.GraphBackendNornicDB {
		return 0
	}
	return nornicDBCanonicalWriteTimeout(getenv)
}

func neo4jProfileGroupStatements(getenv func(string) string) (bool, error) {
	raw := strings.TrimSpace(getenv("ESHU_NEO4J_PROFILE_GROUP_STATEMENTS"))
	if raw == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse ESHU_NEO4J_PROFILE_GROUP_STATEMENTS=%q: %w", raw, err)
	}
	return enabled, nil
}

type ingesterNeo4jDriverCloser struct {
	Driver neo4jdriver.DriverWithContext
}

func (c ingesterNeo4jDriverCloser) Close() error {
	return closeIngesterNeo4jDriver(c.Driver)
}

func closeIngesterNeo4jDriver(driver neo4jdriver.DriverWithContext) error {
	if driver == nil {
		return nil
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), ingesterConnectionTimeout)
	defer cancel()
	return driver.Close(closeCtx)
}
