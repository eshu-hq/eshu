// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	nornicDBTransactionCommitFailedCode = "Neo.ClientError.Transaction.TransactionCommitFailed"
	nornicDBTransactionOutdatedCode     = "Neo.TransientError.Transaction.Outdated"
	nornicDBStatementSyntaxErrorCode    = "Neo.ClientError.Statement.SyntaxError"

	graphWriteRetryReasonConnectivity   = "connectivity_error"
	graphWriteRetryReasonTransient      = "transient_error"
	graphWriteRetryReasonWriteConflict  = "write_conflict"
	graphWriteRetryReasonUniqueConflict = "commit_unique_conflict"

	nornicDBRelationshipSnapshotConflictMessage = "UNWIND MERGE chain relationship update failed: not found"
	// nornicDBRelationshipCreateMissingEndpointPrefix and
	// ...Suffix bracket the create-side sibling of the message above:
	// "UNWIND MERGE chain relationship create failed: start node
	// nornic:<uuid> does not exist", reported when the endpoint the statement
	// just resolved is no longer readable -- observed live while a backend
	// restart tore the store down mid-write. BOTH fragments are required
	// because the uuid between them is per-node, and because matching the
	// "create failed" prefix alone would also swallow "create failed: not
	// found", which TestRetryingExecutorDoesNotBroadenRelationshipSnapshotRetry
	// deliberately keeps terminal.
	nornicDBRelationshipCreateMissingEndpointPrefix = "UNWIND MERGE chain relationship create failed: start node "
	nornicDBRelationshipCreateMissingEndpointSuffix = " does not exist"
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
		func(err error) string { return classifyRetryableGraphWriteError(err, stmt) },
	)
}

// ExecuteGroup delegates to Inner.ExecuteGroup, retrying on transient Neo4j
// errors, relationship snapshot conflicts, and commit-time UNIQUE conflicts
// when every statement in the group is MERGE-shaped (and therefore idempotent
// on re-execution). Without this retry, concurrent canonical writers on the
// same identity surface backend races as non-retryable projection failures
// even though re-executing the group is safe by construction. Worker-knob
// serialization (e.g. ESHU_PROJECTION_WORKERS=1) is not an acceptable
// mitigation per the project rule "Serialization Is Not A Fix" — the design
// must absorb the race here.
//
// Driver-level session.ExecuteWrite retries handle Neo.TransientError.*
// codes. If the driver returns TransactionExecutionLimit after exhausting its
// own budget, this loop does not repeat that entire budget. The outer loop
// additionally covers Neo.ClientError.Transaction.TransactionCommitFailed
// when the typed code or fallback message classifies as a NornicDB commit-time
// UNIQUE conflict on a MERGE-shaped group.
func (r *RetryingExecutor) ExecuteGroup(ctx context.Context, stmts []Statement) error {
	ge, ok := r.Inner.(GroupExecutor)
	if !ok {
		return fmt.Errorf("inner executor does not support ExecuteGroup")
	}
	return r.runWithRetry(
		ctx,
		groupOperationLabel(stmts),
		func() error { return ge.ExecuteGroup(ctx, stmts) },
		func(err error) string { return classifyRetryableGraphWriteGroupError(err, stmts) },
	)
}

// ExecuteProbe delegates to Inner.ExecuteProbe when Inner implements
// ProbeExecutor. It returns an error without retrying when Inner does not
// support probing, mirroring ExecuteGroup's fail-closed capability check so
// the caller's type assertion for ProbeExecutor at this layer succeeds while
// the call itself still reports "unsupported" -- callers MUST treat that
// error as "unknown", not "zero rows" (see ProbeExecutor's doc).
//
// Deliberately does NOT retry, unlike Execute and ExecuteGroup (#5998 review).
// A probe is a latency optimisation whose fallback is both cheap and always
// correct: every caller treats an error as "unknown" and runs the DELETE it
// guards. Routing probes through runWithRetry would inherit the write budget
// -- four attempts, each bounded by ESHU_CANONICAL_WRITE_TIMEOUT (30s by
// default) -- so a backend sustaining TransientError could hold the partition
// lease for roughly two minutes before the fail-safe DELETE even starts, to
// avoid a single DELETE the guard exists to make cheaper. Failing straight
// through bounds the worst case at that one DELETE instead. The DELETE that
// follows still retries, so no transient-error resilience is lost on the write
// path; only the read that decides whether to attempt it gives up early.
func (r *RetryingExecutor) ExecuteProbe(ctx context.Context, stmt Statement) (bool, error) {
	pe, ok := r.Inner.(ProbeExecutor)
	if !ok {
		return false, fmt.Errorf("inner executor does not support ExecuteProbe")
	}
	return pe.ExecuteProbe(ctx, stmt)
}

// runWithRetry centralizes the retry loop for both Execute and ExecuteGroup.
// classify returns a bounded reason for errors that are safe to retry; do
// performs the work. Both callers share the same exponential-backoff-with-
// jitter cadence and the same retry-budget exhaustion behavior.
func (r *RetryingExecutor) runWithRetry(
	ctx context.Context,
	operationLabel string,
	do func() error,
	classify func(error) string,
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
		retryReason := classify(lastErr)
		if retryReason == "" {
			if isMalformedNeo4jConnectivityError(lastErr) {
				return errMalformedNeo4jConnectivity
			}
			return lastErr
		}
		if attempt == maxRetries {
			break
		}

		if r.Instruments != nil && r.Instruments.Neo4jDeadlockRetries != nil {
			r.Instruments.Neo4jDeadlockRetries.Add(ctx, 1,
				metric.WithAttributes(
					telemetry.AttrWritePhase(operationLabel),
					telemetry.AttrReason(retryReason),
				))
		}

		delay := baseDelay * time.Duration(1<<uint(attempt))
		jitter := time.Duration(float64(delay) * (0.5 + rand.Float64())) // #nosec G404 -- non-security jitter for exponential backoff retry delay
		slog.Warn(
			"neo4j transient error, retrying",
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
	return classifyTransientNeo4jError(err) != ""
}

// classifyTransientNeo4jError returns one bounded retry-reason value for
// immediate graph-write failures that are safe to replay. A driver's
// CommitFailedDeadError is deliberately excluded: it is wrapped as a
// ConnectivityError, but its commit outcome is unknown and the Neo4j driver
// itself classifies it as non-retryable.
func classifyTransientNeo4jError(err error) string {
	if err == nil {
		return ""
	}
	var connectivityErr *neo4jdriver.ConnectivityError
	if errors.As(err, &connectivityErr) {
		if isRetryableNeo4jConnectivityError(connectivityErr) {
			return graphWriteRetryReasonConnectivity
		}
		return ""
	}
	if isNornicDBRestartTransactionStartFailure(err) {
		return graphWriteRetryReasonConnectivity
	}
	msg := err.Error()
	// NornicDB reports relationship snapshot conflicts with a transient code.
	// Classify the narrow conflict shape before the generic TransientError
	// fallback so retry telemetry preserves the actionable reason.
	if isNornicDBWriteConflict(msg) {
		return graphWriteRetryReasonWriteConflict
	}
	if strings.Contains(msg, "TransientError") ||
		strings.Contains(msg, "DeadlockDetected") ||
		strings.Contains(msg, "LockClient") ||
		strings.Contains(msg, "lock acquisition") {
		return graphWriteRetryReasonTransient
	}
	return ""
}

func isRetryableNeo4jConnectivityError(connectivityErr *neo4jdriver.ConnectivityError) bool {
	if connectivityErr.Inner == nil {
		return false
	}
	if !neo4jdriver.IsRetryable(connectivityErr) {
		return false
	}
	// CommitFailedDeadError is private to the driver. Keep the public error
	// shape as a defensive compatibility guard in addition to IsRetryable so
	// older driver versions cannot turn an unknown commit outcome into replay.
	return !strings.HasPrefix(connectivityErr.Inner.Error(), "Connection lost during commit:")
}

func isNeo4jConnectivityError(err error) bool {
	var connectivityErr *neo4jdriver.ConnectivityError
	return errors.As(err, &connectivityErr)
}

// isRetryableGraphWriteError classifies bounded graph-write retries using both
// driver-level transient signals and statement-aware NornicDB commit conflicts.
func isRetryableGraphWriteError(err error, stmt Statement) bool {
	return classifyRetryableGraphWriteError(err, stmt) != ""
}

func classifyRetryableGraphWriteError(err error, stmt Statement) string {
	if reason := classifyTransientNeo4jError(err); reason != "" {
		return reason
	}
	if isNeo4jConnectivityError(err) {
		return ""
	}
	if isNornicDBMergeRelationshipSnapshotConflict(err, stmt.Cypher) {
		return graphWriteRetryReasonWriteConflict
	}
	if err != nil && isNornicDBMergeUniqueConflict(err, stmt.Cypher) {
		return graphWriteRetryReasonUniqueConflict
	}
	return ""
}

// classifyRetryableGraphWriteGroupError classifies a phase-group write failure
// as retryable when EVERY statement in the group converges on re-execution
// (see allStatementsAreReplaySafe) AND the underlying error matches a NornicDB
// relationship snapshot or commit-time UNIQUE conflict pattern. Groups holding
// a statement that does NOT converge — CREATE, an accumulating SET — are NOT
// retried, because re-executing one can double-apply effects.
//
// Immediate driver-level transient errors (deadlocks, lock timeouts) remain
// retryable regardless of statement shape because session.ExecuteWrite re-runs
// the entire transaction body from scratch. TransactionExecutionLimit is the
// exception: it means session.ExecuteWrite already exhausted that inner retry
// budget, so repeating it here would multiply the failure window.
func classifyRetryableGraphWriteGroupError(err error, stmts []Statement) string {
	var exhausted *neo4jdriver.TransactionExecutionLimit
	if errors.As(err, &exhausted) {
		return ""
	}
	if reason := classifyTransientNeo4jError(err); reason != "" {
		return reason
	}
	if isNeo4jConnectivityError(err) {
		return ""
	}
	if err == nil {
		return ""
	}
	if !allStatementsAreReplaySafe(stmts) {
		return ""
	}
	if isNornicDBRelationshipSnapshotConflict(err) {
		return graphWriteRetryReasonWriteConflict
	}
	if isNornicDBCommitTimeUniqueConflictError(err) {
		return graphWriteRetryReasonUniqueConflict
	}
	return ""
}

func isNornicDBWriteConflict(msg string) bool {
	return strings.Contains(msg, "conflict:") &&
		strings.Contains(msg, "changed after transaction start")
}

// isNornicDBMergeRelationshipSnapshotConflict recognizes the pinned
// NornicDB relationship-update snapshot race only for an idempotent MERGE
// statement. The backend reports the race as a Statement.SyntaxError even
// though a fresh transaction can safely match and update the winning edge.
func isNornicDBMergeRelationshipSnapshotConflict(err error, cypher string) bool {
	if !strings.Contains(strings.ToUpper(cypher), "MERGE") {
		return false
	}
	return isNornicDBRelationshipSnapshotConflict(err)
}

// isNornicDBRelationshipSnapshotConflict matches the typed wire errors emitted
// when NornicDB's relationship lookup cannot act on an endpoint the current
// transaction snapshot resolved: the update-side conflict, where a
// concurrently committed edge is no longer updatable, and the create-side
// failure, where a just-resolved start node is no longer readable (seen while
// a backend restart tore the store down mid-write). Both surface under
// nornicDBStatementSyntaxErrorCode, which a genuinely malformed query also
// uses, so the message carries the discrimination and every caller gates on a
// MERGE-shaped statement before acting on the result.
//
// Replay is safe on the create-side shape for its own reasons, not only by
// analogy: a MERGE-shaped statement converges on re-execution, and a start node
// that genuinely does not exist fails again and dead-letters once the retry
// budget is spent — the same terminal outcome as classifying it terminal up
// front, reached only after recovery was actually attempted.
func isNornicDBRelationshipSnapshotConflict(err error) bool {
	if err == nil {
		return false
	}
	var neo4jErr *neo4jdriver.Neo4jError
	if !errors.As(err, &neo4jErr) {
		return false
	}
	if neo4jErr.Code != nornicDBStatementSyntaxErrorCode {
		return false
	}
	return strings.Contains(neo4jErr.Msg, nornicDBRelationshipSnapshotConflictMessage) ||
		isNornicDBRelationshipCreateMissingEndpoint(neo4jErr.Msg)
}

// isNornicDBRelationshipCreateMissingEndpoint reports whether msg is the
// create-side missing-endpoint failure. The two fragments must genuinely
// BRACKET the node id -- suffix after prefix -- which is the narrowness the
// guard's justification rests on. Two unordered Contains calls would also
// accept a message carrying the fragments in the wrong order or in unrelated
// clauses, which is a wider match than the comment claims and than the
// neighbouring terminal bodies deserve.
func isNornicDBRelationshipCreateMissingEndpoint(msg string) bool {
	start := strings.Index(msg, nornicDBRelationshipCreateMissingEndpointPrefix)
	if start < 0 {
		return false
	}
	rest := msg[start+len(nornicDBRelationshipCreateMissingEndpointPrefix):]
	return strings.Contains(rest, nornicDBRelationshipCreateMissingEndpointSuffix)
}

// isNornicDBMergeUniqueConflict treats commit-time unique conflicts from
// MERGE-shaped upserts as retryable because a concurrent writer may have
// created the intended node between match and commit. The cypher guard
// ensures we only retry when the originating statement is itself
// idempotent on re-execution.
func isNornicDBMergeUniqueConflict(err error, cypher string) bool {
	if !strings.Contains(strings.ToUpper(cypher), "MERGE") {
		return false
	}
	return isNornicDBCommitTimeUniqueConflictError(err)
}

// isNornicDBCommitTimeUniqueConflictError classifies typed Neo4j errors by
// their transaction-commit failure code when the driver exposes one. It also
// recognizes NornicDB's narrow Statement.SyntaxError compatibility shape when
// the body proves the failure occurred at commit on a UNIQUE constraint. The
// caller still requires MERGE-shaped Cypher before replay. Historical string
// wrapping remains supported for older NornicDB surfaces.
func isNornicDBCommitTimeUniqueConflictError(err error) bool {
	if err == nil {
		return false
	}

	var neo4jErr *neo4jdriver.Neo4jError
	if errors.As(err, &neo4jErr) && strings.TrimSpace(neo4jErr.Code) != "" {
		switch neo4jErr.Code {
		case nornicDBTransactionCommitFailedCode, nornicDBTransactionOutdatedCode:
			return isNornicDBUniqueConflictBody(neo4jErr.Msg)
		case nornicDBStatementSyntaxErrorCode:
			// NornicDB can surface a concurrent MERGE loser under the generic
			// SyntaxError code even though parsing succeeded and the failure
			// occurred at commit. Keep this exception narrower than the code:
			// require both the commit-failure prefix observed on the wire and
			// the exact UNIQUE-conflict body before the caller's MERGE guard may
			// replay the statement.
			return strings.Contains(neo4jErr.Msg, "commit failed: constraint violation") &&
				isNornicDBUniqueConflictBody(neo4jErr.Msg)
		default:
			return false
		}
	}

	return isNornicDBCommitTimeUniqueConflict(err.Error())
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
	if !isNornicDBUniqueConflictBody(msg) {
		return false
	}
	return strings.Contains(msg, "failed to commit implicit transaction") ||
		strings.Contains(msg, "commit failed") ||
		strings.Contains(msg, "TransactionCommitFailed")
}

func isNornicDBUniqueConflictBody(msg string) bool {
	if !strings.Contains(msg, "constraint violation") {
		return false
	}
	if !strings.Contains(msg, "UNIQUE on") {
		return false
	}
	if !strings.Contains(msg, "already exists") {
		return false
	}
	return true
}

// whereClausePattern captures the WHERE body up to the first write clause.
var whereClausePattern = regexp.MustCompile(`(?is)\bWHERE\b(.*?)(?:\bDETACH\s+DELETE\b|\bDELETE\b|\bREMOVE\b|\bRETURN\b|\z)`)

// predicateSeparatorPattern splits a WHERE body into its terms. It splits on OR
// as well as AND: an OR term WIDENS the match set, so leaving it joined would
// let `n.repo_id IN $repo_ids OR n.stale` pass as one term containing a $param.
var predicateSeparatorPattern = regexp.MustCompile(`(?i)\b(AND|OR)\b`)

// everyConjunctIsBounded requires every WHERE term to name a parameter.
//
// A predicate can name a parameter and still read mutable graph state:
// `WHERE n.repo_id IN $repo_ids AND n.stale` passes both the membership and the
// open-ended checks in hasParameterBoundedPredicate. The delete stays inside the
// key space $repo_ids enumerates, so the blast radius is smaller than the
// complement case -- but within that space the set still moves, because a
// concurrent writer flipping n.stale between the failed attempt and the replay
// puts a node in range the first attempt never saw. Same broken premise.
//
// A literal comparison is NOT accepted as bounding: `n.stale = true` reads the
// same mutable property as the bare `n.stale`. Only a $param names a value the
// caller fixed for the duration of the statement.
//
// Every retract reaching this path today is a conjunction of parameter-bound
// terms, so requiring it rejects nothing that currently retries.
func everyConjunctIsBounded(cypher string) bool {
	where := whereClausePattern.FindStringSubmatch(cypher)
	if where == nil {
		return true
	}
	for _, term := range predicateSeparatorPattern.Split(where[1], -1) {
		if strings.TrimSpace(term) == "" {
			continue
		}
		if !strings.Contains(term, "$") {
			return false
		}
	}
	return true
}
