package cypher

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RetryingExecutor wraps an Executor with retry logic for transient Neo4j
// errors such as deadlocks. Concurrent MERGE operations on shared nodes
// (Repository, Directory, Module) can trigger Neo4j deadlocks that resolve
// on retry.
type RetryingExecutor struct {
	Inner       Executor
	MaxRetries  int                    // default 3
	BaseDelay   time.Duration          // default 50ms, doubles per retry with jitter
	Instruments *telemetry.Instruments // optional; records retry counter
}

// Execute delegates to Inner, retrying on transient Neo4j errors (deadlocks,
// lock timeouts) with exponential backoff and jitter.
func (r *RetryingExecutor) Execute(ctx context.Context, stmt Statement) error {
	return r.runWithRetry(
		ctx,
		string(stmt.Operation),
		func() error { return r.Inner.Execute(ctx, stmt) },
		func(err error) bool { return isRetryableGraphWriteError(err, stmt) },
	)
}

// ExecuteGroup delegates to Inner.ExecuteGroup, retrying on transient Neo4j
// errors and on commit-time UNIQUE conflicts when every statement in the
// group is MERGE-shaped (and therefore idempotent on re-execution). Without
// this retry, concurrent canonical writers on the same uid surface a
// MERGE-vs-MERGE race as a non-retryable projection_failure even though
// re-executing the group is safe by construction. Worker-knob serialization
// (e.g. ESHU_PROJECTION_WORKERS=1) is not an acceptable mitigation per the
// project rule "Serialization Is Not A Fix" — the design must absorb the
// race here.
//
// Driver-level session.ExecuteWrite retries handle Neo.TransientError.*
// codes; this loop additionally covers Neo.ClientError.Transaction.
// TransactionCommitFailed when the message classifies as a NornicDB
// commit-time UNIQUE conflict on a MERGE-shaped group.
func (r *RetryingExecutor) ExecuteGroup(ctx context.Context, stmts []Statement) error {
	ge, ok := r.Inner.(GroupExecutor)
	if !ok {
		return fmt.Errorf("inner executor does not support ExecuteGroup")
	}
	return r.runWithRetry(
		ctx,
		groupOperationLabel(stmts),
		func() error { return ge.ExecuteGroup(ctx, stmts) },
		func(err error) bool { return isRetryableGraphWriteGroupError(err, stmts) },
	)
}

// runWithRetry centralizes the retry loop for both Execute and ExecuteGroup.
// classify returns true for errors that are safe to retry; do performs the
// work. Both callers share the same exponential-backoff-with-jitter cadence
// and the same retry-budget exhaustion behavior.
func (r *RetryingExecutor) runWithRetry(
	ctx context.Context,
	operationLabel string,
	do func() error,
	classify func(error) bool,
) error {
	maxRetries := r.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	baseDelay := r.BaseDelay
	if baseDelay <= 0 {
		baseDelay = 50 * time.Millisecond
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = do()
		if lastErr == nil {
			return nil
		}
		if !classify(lastErr) {
			return lastErr
		}
		if attempt == maxRetries {
			break
		}

		if r.Instruments != nil && r.Instruments.Neo4jDeadlockRetries != nil {
			r.Instruments.Neo4jDeadlockRetries.Add(ctx, 1,
				metric.WithAttributes(telemetry.AttrWritePhase(operationLabel)))
		}

		delay := baseDelay * time.Duration(1<<uint(attempt))
		jitter := time.Duration(float64(delay) * (0.5 + rand.Float64()))
		slog.Warn("neo4j transient error, retrying",
			"attempt", attempt+1,
			"max_retries", maxRetries,
			"delay", jitter.String(),
			"operation", operationLabel,
			"error", lastErr.Error(),
		)

		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(jitter):
		}
	}

	return &neo4jRetryableError{
		inner: fmt.Errorf("neo4j transient error after %d retries: %w", maxRetries, lastErr),
		code:  "TransientError",
	}
}

// groupOperationLabel returns a stable label for a phase-group write that
// the retry-counter metric can record. Uses the first statement's Operation
// when present; falls back to "group" for empty groups.
func groupOperationLabel(stmts []Statement) string {
	if len(stmts) == 0 {
		return "group"
	}
	if op := strings.TrimSpace(string(stmts[0].Operation)); op != "" {
		return op
	}
	return "group"
}

// isTransientNeo4jError returns true for Neo4j errors that are safe to retry:
// deadlocks, lock acquisition timeouts, and other transient failures.
func isTransientNeo4jError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "TransientError") ||
		strings.Contains(msg, "DeadlockDetected") ||
		strings.Contains(msg, "LockClient") ||
		strings.Contains(msg, "lock acquisition") ||
		isNornicDBWriteConflict(msg)
}

// isRetryableGraphWriteError classifies bounded graph-write retries using both
// driver-level transient signals and statement-aware NornicDB commit conflicts.
func isRetryableGraphWriteError(err error, stmt Statement) bool {
	if isTransientNeo4jError(err) {
		return true
	}
	if err == nil {
		return false
	}
	return isNornicDBMergeUniqueConflict(err.Error(), stmt.Cypher)
}

// isRetryableGraphWriteGroupError classifies a phase-group write failure as
// retryable when EVERY statement in the group is MERGE-shaped (and therefore
// idempotent on re-execution) AND the underlying error matches a NornicDB
// commit-time UNIQUE conflict pattern. Mixed groups containing non-MERGE
// statements are NOT retried — re-executing a CREATE/DELETE/SET-only
// statement under partial-success conditions can double-apply effects.
//
// Driver-level transient errors (deadlocks, lock timeouts) remain retryable
// regardless of statement shape because session.ExecuteWrite re-runs the
// entire transaction body from scratch.
func isRetryableGraphWriteGroupError(err error, stmts []Statement) bool {
	if isTransientNeo4jError(err) {
		return true
	}
	if err == nil {
		return false
	}
	if !allStatementsAreMerge(stmts) {
		return false
	}
	return isNornicDBCommitTimeUniqueConflict(err.Error())
}

// allStatementsAreMerge returns true when every statement in stmts contains
// MERGE in its Cypher source. Empty groups return false because there is
// nothing safe to retry.
func allStatementsAreMerge(stmts []Statement) bool {
	if len(stmts) == 0 {
		return false
	}
	for _, s := range stmts {
		if !strings.Contains(strings.ToUpper(s.Cypher), "MERGE") {
			return false
		}
	}
	return true
}

func isNornicDBWriteConflict(msg string) bool {
	return strings.Contains(msg, "conflict:") &&
		strings.Contains(msg, "changed after transaction start")
}

// isNornicDBMergeUniqueConflict treats commit-time unique conflicts from
// MERGE-shaped upserts as retryable because a concurrent writer may have
// created the intended node between match and commit. The cypher guard
// ensures we only retry when the originating statement is itself
// idempotent on re-execution.
func isNornicDBMergeUniqueConflict(msg string, cypher string) bool {
	if !strings.Contains(strings.ToUpper(cypher), "MERGE") {
		return false
	}
	return isNornicDBCommitTimeUniqueConflict(msg)
}

// isNornicDBCommitTimeUniqueConflict matches NornicDB's commit-time UNIQUE
// constraint violations across binary versions. Older NornicDB releases
// wrap the failure as "failed to commit implicit transaction: constraint
// violation:..."; timothyswt/nornicdb-amd64-cpu:v1.0.45 and later surface a
// Neo4jError with code Neo.ClientError.Transaction.TransactionCommitFailed
// and body "commit failed: constraint violation:...". Both shapes describe
// the same race-on-commit class and are safe to retry on a MERGE-shaped
// write where MERGE re-execution will match the now-committed node.
func isNornicDBCommitTimeUniqueConflict(msg string) bool {
	if !strings.Contains(msg, "constraint violation") {
		return false
	}
	if !strings.Contains(msg, "UNIQUE on") {
		return false
	}
	if !strings.Contains(msg, "already exists") {
		return false
	}
	return strings.Contains(msg, "failed to commit implicit transaction") ||
		strings.Contains(msg, "commit failed") ||
		strings.Contains(msg, "TransactionCommitFailed")
}
