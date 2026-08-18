// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
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

// cypherProber is the narrow read-only capability
// cypherRunnerStatementExecutor.ExecuteProbe needs from its wrapped runner
// (#5998 rationale retract probe guard). It is intentionally NOT folded into
// cypherRunner: widening cypherRunner would force every fake and adapter that
// only ever writes (never probes) to implement a read primitive it has no use
// for. neo4jSessionRunner (neo4j_wiring.go) already satisfies this interface
// via QueryCypherExists, built for the pre-existing CanonicalNodeChecker
// pre-flight check, so no new session-layer code is needed -- only the
// adapter-layer forward below.
type cypherProber interface {
	QueryCypherExists(ctx context.Context, cypher string, params map[string]any) (bool, error)
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

// ExecuteProbe forwards to the same persistent RetryingExecutor object as
// Execute and ExecuteGroup (#5998) -- the same object matters because the
// ifafaultinjection build swaps retry.Inner, though that seam deliberately
// does not retry probes (see RetryingExecutor.ExecuteProbe). Without this forward, the reducer's real executor
// chain never satisfies sourcecypher.ProbeExecutor at this layer: every
// wrapper above it (the backpressure gates, RetryingExecutor itself) already
// forwards the capability when present, but reducerNeo4jExecutor is the
// production Executor buildReducerService actually constructs, so a missing
// forward here makes the whole probe guard permanently report "unsupported"
// and silently fall back to the unconditional DELETE it exists to skip.
func (e reducerNeo4jExecutor) ExecuteProbe(ctx context.Context, stmt sourcecypher.Statement) (bool, error) {
	return e.retry.ExecuteProbe(ctx, stmt)
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

// ExecuteProbe type-asserts the wrapped runner against cypherProber and, when
// supported, delegates to QueryCypherExists (#5998). cypherRunner itself has
// no read primitive (see cypherProber's doc comment for why it stays
// separate), so this is the one seam that must type-assert rather than call
// through an interface method directly. When the runner does not implement
// cypherProber, this returns an error rather than a bare "not found" -- the
// caller's fail-safe guard (RetractEdges' probe-then-delete guard) MUST run
// the paired DELETE unconditionally on an unknown answer, never skip it.
func (e cypherRunnerStatementExecutor) ExecuteProbe(ctx context.Context, stmt sourcecypher.Statement) (bool, error) {
	prober, ok := e.runner.(cypherProber)
	if !ok {
		return false, fmt.Errorf("cypher runner %T does not support ExecuteProbe (no QueryCypherExists)", e.runner)
	}
	return prober.QueryCypherExists(ctx, stmt.Cypher, stmt.Parameters)
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
