// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ExecuteProbe implements sourcecypher.ProbeExecutor (#6852): it runs stmt as
// a read-only existence check and reports whether it matched at least one
// row. AccessModeWrite for a read-only statement is deliberate, mirroring
// go/cmd/reducer/neo4j_wiring.go's QueryCypherExists: the probe guards a
// following DELETE decision, so it must observe the same leader-routed graph
// state a write session would rather than risk a lagging-follower false
// negative on a routed deployment. This is the seam
// nornicdb.PhaseGroupExecutor.executeDrainLoop dispatches the bounded
// bare-label retract existence probe through via PhaseGroupExecutor.Inner,
// replacing the former DrainReader.RunProbe method, which bypassed
// InstrumentedExecutor and so emitted neither a neo4j.execute_probe span nor
// a Neo4jQueryDuration{operation=probe} point (#6822 follow-up). Split into
// its own file so runtime_wiring.go stays under the 500-line cap.
func (e projectorNeo4jExecutor) ExecuteProbe(ctx context.Context, stmt sourcecypher.Statement) (bool, error) {
	if e.Driver == nil {
		return false, fmt.Errorf("neo4j driver is required")
	}

	session := e.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.DatabaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters, e.transactionConfigurers()...)
	if err != nil {
		return false, err
	}
	hasNext := result.Next(ctx)
	if err := result.Err(); err != nil {
		return false, err
	}
	return hasNext, nil
}

// ExecuteCountProbe implements sourcecypher.CountProbeExecutor (#8657): it
// runs stmt (a read-only COUNT query) and reports its count. AccessModeWrite
// for a read-only statement is deliberate, for the same leader-routing
// reason as ExecuteProbe above: the count guards a following DELETE
// decision, so it must observe the graph state a write session would. The
// first column of the first row carries the count; a missing row or a
// non-numeric first column errors (callers treat that as "unknown", never
// zero), as does a negative count at the guard.
func (e projectorNeo4jExecutor) ExecuteCountProbe(ctx context.Context, stmt sourcecypher.Statement) (int64, error) {
	if e.Driver == nil {
		return 0, fmt.Errorf("neo4j driver is required")
	}

	session := e.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.DatabaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters, e.transactionConfigurers()...)
	if err != nil {
		return 0, err
	}
	if !result.Next(ctx) {
		if err := result.Err(); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("count probe returned no rows")
	}
	record := result.Record()
	if len(record.Values) == 0 {
		return 0, fmt.Errorf("count probe returned a row with no columns")
	}
	count, ok := countProbeIntValue(record.Values[0])
	if !ok {
		return 0, fmt.Errorf("count probe first column has unexpected type %T", record.Values[0])
	}
	if err := result.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

// countProbeIntValue converts the Bolt integer types a COUNT query can
// return. Neo4j integers arrive as int64; the float64 and int cases keep
// the seam honest against drivers that widen or narrow the value.
func countProbeIntValue(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}
