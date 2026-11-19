// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// RunCypherCountedWrite runs one mutating statement and reports how many
// rows its MatchedCountColumn RETURN bound (#10938). The statement's own
// RETURN row is collected before Consume, mirroring the retirement-report
// collection in the projector executor: a write template that reports the
// column yields exactly one row, and anything else fails the batch closed
// rather than verifying a count that was never read.
func (r neo4jSessionRunner) RunCypherCountedWrite(ctx context.Context, cypher string, params map[string]any) (int64, error) {
	if r.Driver == nil {
		return 0, fmt.Errorf("neo4j driver is required")
	}

	session := r.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: r.DatabaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, params, r.transactionConfigurers()...)
	if err != nil {
		return 0, err
	}
	count, err := collectMatchedCount(ctx, result)
	if err != nil {
		return 0, err
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return 0, err
	}
	// Issue #6783: publish Bolt summary counters to the call's
	// write-counts collector. Without a stashed collector this is a
	// one-lookup no-op.
	sourcecypher.ReportWriteCounts(ctx, cypher, params, sourcecypher.WriteCountersFromSummary(summary.Counters()))
	return count, nil
}

// RunCypherCountedWriteGroup executes multiple counted-write statements in
// one write transaction and reports one matched count per statement, aligned
// with stmts (#10938). A short or missing RETURN row on any statement fails
// the whole group closed: the transaction rolls back and the caller retries
// the unverified batch.
func (r neo4jSessionRunner) RunCypherCountedWriteGroup(ctx context.Context, stmts []sourcecypher.Statement) ([]int64, error) {
	if r.Driver == nil {
		return nil, fmt.Errorf("neo4j driver is required")
	}
	if len(stmts) == 0 {
		return nil, nil
	}

	session := r.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: r.DatabaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	rawCounts, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		counts := make([]int64, 0, len(stmts))
		for _, stmt := range stmts {
			result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if runErr != nil {
				return nil, runErr
			}
			count, collectErr := collectMatchedCount(ctx, result)
			if collectErr != nil {
				return nil, collectErr
			}
			summary, consumeErr := result.Consume(ctx)
			if consumeErr != nil {
				return nil, consumeErr
			}
			sourcecypher.ReportWriteCounts(ctx, stmt.Cypher, stmt.Parameters, sourcecypher.WriteCountersFromSummary(summary.Counters()))
			counts = append(counts, count)
		}
		return counts, nil
	}, r.transactionConfigurers()...)
	if err != nil {
		return nil, err
	}
	counts, ok := rawCounts.([]int64)
	if !ok {
		return nil, fmt.Errorf("counted write group returned an unexpected count shape")
	}
	if err := sourcecypher.CountedWriteCounts(stmts, counts); err != nil {
		return nil, err
	}
	return counts, nil
}

// collectMatchedCount drains one counted write statement's RETURN rows and
// returns its MatchedCountColumn value (#10938). A `RETURN count(...)`
// template yields exactly one row on both supported backends; zero rows or
// more than one means the template does not report the column the writer
// verifies, so the batch fails closed instead of acting on a count that was
// never produced.
func collectMatchedCount(ctx context.Context, result neo4jdriver.ResultWithContext) (int64, error) {
	var count int64
	rows := 0
	for result.Next(ctx) {
		record := result.Record()
		if record == nil {
			continue
		}
		value, found := record.Get(sourcecypher.MatchedCountColumn)
		if !found {
			return 0, fmt.Errorf("counted write statement did not return %q", sourcecypher.MatchedCountColumn)
		}
		n, ok := sourcecypher.CountProbeIntValue(value)
		if !ok {
			return 0, fmt.Errorf("counted write statement returned a non-integer %q value", sourcecypher.MatchedCountColumn)
		}
		count = n
		rows++
	}
	if err := result.Err(); err != nil {
		return 0, err
	}
	if rows != 1 {
		return 0, fmt.Errorf("counted write statement returned %d rows, want exactly 1", rows)
	}
	return count, nil
}
