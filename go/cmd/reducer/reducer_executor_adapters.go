// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// cypherRunner is the narrow interface shared by both executor adapters.
type cypherRunner interface {
	RunCypher(ctx context.Context, cypher string, params map[string]any) error
	RunCypherGroup(ctx context.Context, stmts []sourcecypher.Statement) error
}

// reducerNeo4jExecutor adapts a cypherRunner to the sourcecypher.Executor
// interface used by EdgeWriter. retry is a PERSISTENT
// *sourcecypher.RetryingExecutor constructed once at startup (see
// newReducerNeo4jExecutor / openReducerNeo4jAdapters) -- not rebuilt per
// call the way the now-removed executeReducerCypherWithRetry did. This
// hoist (#5048) is what lets go/cmd/reducer's ifafaultinjection-tagged
// wrapIfaFaultExecutor arm a below-the-seam fault decorator on retry.Inner:
// arming a fresh, per-call RetryingExecutor would have had nothing to reach.
type reducerNeo4jExecutor struct {
	session cypherRunner
	retry   *sourcecypher.RetryingExecutor
}

// newReducerNeo4jExecutor builds a reducerNeo4jExecutor around session with
// its own persistent RetryingExecutor. instruments may be nil (no retry
// counter recorded, matching the pre-#5048 default -- see the doc on
// openReducerNeo4jAdapters for why that default silently dropped
// Neo4jDeadlockRetries and is now wired here).
func newReducerNeo4jExecutor(session cypherRunner, instruments *telemetry.Instruments) reducerNeo4jExecutor {
	return reducerNeo4jExecutor{
		session: session,
		retry: &sourcecypher.RetryingExecutor{
			Inner:       cypherRunnerStatementExecutor{runner: session},
			Instruments: instruments,
		},
	}
}

func (e reducerNeo4jExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	return e.retry.Execute(ctx, stmt)
}

// ExecuteGroup runs all statements in one graph transaction through the same
// persistent retry seam as Execute. RetryingExecutor may repeat an atomic group
// after a retryable immediate transient/connectivity failure. A commit-ambiguous
// failure is not retried in place by RetryingExecutor. The still-pending
// repo-dependency acceptance unit may be replayed later after backoff; its
// source-scoped retract and deterministic MERGE upserts are idempotent.
// Malformed connectivity failures become safe terminal errors. Commit-time
// UNIQUE retry is narrower and requires every statement to be MERGE-shaped.
func (e reducerNeo4jExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	return e.retry.ExecuteGroup(ctx, stmts)
}

// reducerCypherExecutor adapts a cypherRunner to the reducer.CypherExecutor
// interface used by WorkloadMaterializer. retry is a PERSISTENT
// *sourcecypher.RetryingExecutor constructed once at startup; see
// reducerNeo4jExecutor's doc for why persistence matters (#5048).
type reducerCypherExecutor struct {
	retry *sourcecypher.RetryingExecutor
}

// newReducerCypherExecutor builds a reducerCypherExecutor around session
// with its own persistent RetryingExecutor. instruments may be nil.
func newReducerCypherExecutor(session cypherRunner, instruments *telemetry.Instruments) reducerCypherExecutor {
	return reducerCypherExecutor{
		retry: &sourcecypher.RetryingExecutor{
			Inner:       cypherRunnerStatementExecutor{runner: session},
			Instruments: instruments,
		},
	}
}

func (e reducerCypherExecutor) ExecuteCypher(ctx context.Context, cypher string, params map[string]any) error {
	return e.retry.Execute(ctx, sourcecypher.Statement{
		Operation:  sourcecypher.OperationCanonicalUpsert,
		Cypher:     cypher,
		Parameters: params,
	})
}

// cypherRunnerStatementExecutor adapts a cypherRunner's RunCypher method to
// sourcecypher.Executor. It is the value both reducerNeo4jExecutor and
// reducerCypherExecutor install as their persistent RetryingExecutor's Inner
// (see newReducerNeo4jExecutor / newReducerCypherExecutor), and the exact
// seam go/cmd/reducer's ifafaultinjection-tagged wrapIfaFaultExecutor
// replaces with an armed decorator for the executor-retry fault lane
// (#5048): RetryingExecutor.Execute calls retry.Inner.Execute on every
// attempt, so swapping retry.Inner is how a scripted fault reaches the
// retry loop from below rather than short-circuiting above it.
type cypherRunnerStatementExecutor struct {
	runner cypherRunner
}

func (e cypherRunnerStatementExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	return e.runner.RunCypher(ctx, stmt.Cypher, stmt.Parameters)
}

func (e cypherRunnerStatementExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	return e.runner.RunCypherGroup(ctx, stmts)
}

type nornicDBSemanticObservedExecutor struct {
	inner sourcecypher.Executor
}

func (e nornicDBSemanticObservedExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	if e.inner == nil {
		return nil
	}
	start := time.Now()
	err := e.inner.Execute(ctx, stmt)
	duration := time.Since(start)
	attrs := []any{
		"graph_backend", string(runtimecfg.GraphBackendNornicDB),
		"label", semanticStatementLabel(stmt),
		"rows", semanticStatementRows(stmt),
		"duration_s", duration.Seconds(),
		"operation", string(stmt.Operation),
		"statement", semanticStatementSummary(stmt),
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
		slog.Warn("nornicdb semantic statement failed", attrs...)
		return err
	}
	slog.Info("nornicdb semantic statement completed", attrs...)
	return nil
}

func semanticStatementLabel(stmt sourcecypher.Statement) string {
	label, _ := stmt.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
	label = strings.TrimSpace(label)
	if label != "" {
		return label
	}
	if stmt.Operation == sourcecypher.OperationCanonicalRetract {
		return "semantic_retract"
	}
	return "unknown"
}

func semanticStatementRows(stmt sourcecypher.Statement) int {
	if rows, ok := stmt.Parameters["rows"].([]map[string]any); ok {
		return len(rows)
	}
	if rows, ok := stmt.Parameters["rows"].([]any); ok {
		return len(rows)
	}
	if repoIDs, ok := stmt.Parameters["repo_ids"].([]string); ok {
		return len(repoIDs)
	}
	if _, ok := stmt.Parameters["entity_id"]; ok {
		return 1
	}
	return 0
}

func semanticStatementSummary(stmt sourcecypher.Statement) string {
	if summary, ok := stmt.Parameters[sourcecypher.StatementMetadataSummaryKey].(string); ok {
		if summary = strings.TrimSpace(summary); summary != "" {
			return summary
		}
	}
	return summarizeReducerCypher(stmt.Cypher)
}

func summarizeReducerCypher(cypher string) string {
	fields := strings.Fields(cypher)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > 16 {
		fields = fields[:16]
	}
	return strings.Join(fields, " ")
}
