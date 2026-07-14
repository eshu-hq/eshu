// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ifaFaultScriptEnv names the fault-script path environment variable. Set
// only under the ifafaultinjection build tag; reading it (and the
// sourcecypher.FaultingExecutor decorator it wires in) is unreachable from
// any untagged build (see ifa_fault_wiring_off.go). This is issue #4580
// Layer 4 / P6 slice S4's in-binary fault-injection entry point for the
// (separate, deferred) Docker gate verify-ifa-fault-injection.sh.
const ifaFaultScriptEnv = "ESHU_IFA_FAULT_SCRIPT"

// ifaFaultSentinelSuffix names the restart-backend-between-phase-groups
// sentinel file this wiring derives deterministically from the fault-script
// path: <script path>.restart-sentinel. This is a fixed convention rather
// than a second environment variable so a fault run stays fully described by
// one script path; the (deferred) gate script that restarts the graph
// backend and deletes the sentinel is expected to derive the same path the
// same way.
const ifaFaultSentinelSuffix = ".restart-sentinel"

// wrapIfaFaultExecutor wraps inner with the in-binary fault decorator when
// ESHU_IFA_FAULT_SCRIPT names a fault script, reading and validating it via
// faultreplay.Load. It returns inner unchanged when the env var is unset (the
// common case for every normal run of a tagged binary). Errors here are
// fail-closed: an operator who sets the env var to a bad path or an invalid
// script gets a startup error, never a silently-ignored fault script.
func wrapIfaFaultExecutor(inner sourcecypher.Executor, getenv func(string) string, logger *slog.Logger) (sourcecypher.Executor, error) {
	path := strings.TrimSpace(getenv(ifaFaultScriptEnv))
	if path == "" {
		return inner, nil
	}
	script, err := faultreplay.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load %s=%q: %w", ifaFaultScriptEnv, path, err)
	}
	faulting, err := sourcecypher.NewFaultingExecutor(inner, script, path+ifaFaultSentinelSuffix)
	if err != nil {
		return nil, fmt.Errorf("build ifa faulting executor from %s=%q: %w", ifaFaultScriptEnv, path, err)
	}
	// #5048: wire the below-the-seam armer so the executor-retry lane
	// retries in place through the reducer's real persistent
	// RetryingExecutor instead of surfacing above it. armExecutorRetrySeam
	// returns nil when inner is not the reducer's own executor chain (for
	// example a test stub), in which case the fault decorator keeps its
	// pre-#5048 fallback behavior for every lane.
	if fe, ok := faulting.(*sourcecypher.FaultingExecutor); ok {
		if armed := armExecutorRetrySeam(inner); armed != nil {
			fe.SetExecutorRetryArmer(armed)
		}
	}
	if logger != nil {
		logger.Warn(
			"ifa fault injection enabled for reducer graph writes",
			"fault_script", path,
			"fault_count", len(script.Faults),
		)
	}
	return faulting, nil
}

// armExecutorRetrySeam locates the reducer's persistent
// *sourcecypher.RetryingExecutor inside inner -- unwrapping a
// sourcecypher.InstrumentedExecutor when present, matching how
// go/cmd/reducer/observed_service_wiring.go wraps neo4jExecutor before
// buildReducerService ever sees it -- and installs an armed decorator
// directly below it, replacing retry.Inner. FaultingExecutor asks the returned
// value (via SetExecutorRetryArmer) for a derived context instead of returning
// the executor-retry lane's shaped error itself, so the reducer's real per-call
// retry loop absorbs the scripted failure in place (#5048) rather than the
// failure surfacing above RetryingExecutor and collapsing to queue-retry.
//
// retry.Inner is a pointer-field mutation: reducerNeo4jExecutor is a value
// type, but its retry field is *sourcecypher.RetryingExecutor, so mutating
// rne.retry.Inner here mutates the SAME RetryingExecutor the original
// executor (buried inside inner, e.g. under InstrumentedExecutor.Inner)
// already holds a pointer to -- no need to reconstruct or reassign inner.
//
// Returns nil when inner is not the reducer's own executor chain, in which
// case the caller must not call SetExecutorRetryArmer and the fault
// decorator keeps its pre-#5048 fallback for the executor-retry lane.
func armExecutorRetrySeam(inner sourcecypher.Executor) *ifaExecutorRetryArmedExecutor {
	target := inner
	if instrumented, ok := target.(*sourcecypher.InstrumentedExecutor); ok {
		target = instrumented.Inner
	}
	rne, ok := target.(reducerNeo4jExecutor)
	if !ok || rne.retry == nil {
		return nil
	}
	armed := &ifaExecutorRetryArmedExecutor{inner: rne.retry.Inner}
	rne.retry.Inner = armed
	return armed
}

// ifaExecutorRetryArmedExecutor decorates the cypher seam directly below the
// reducer's persistent RetryingExecutor (retry.Inner, normally a
// cypherRunnerStatementExecutor wrapping the real Neo4j session). Arm derives
// a context carrying one private fire-once token. Execute and ExecuteGroup only
// consume a token from their own context, so an unrelated concurrent writer
// cannot steal the fault. RetryingExecutor reuses that context for its retry;
// attempt 1 consumes the token and fails, while attempt 2 reaches inner. This
// is the "fire below" half of the P6 executor-retry lane fix (#5048, #5086):
// FaultingExecutor remains the ordinal/trigger owner and passes the derived
// context into the exact call that matched.
type ifaExecutorRetryArmedExecutor struct {
	inner sourcecypher.Executor
}

type ifaExecutorRetryFaultContextKey struct{}

type ifaExecutorRetryFaultToken struct {
	armed atomic.Bool
}

// Arm implements sourcecypher.ExecutorRetryArmer. It binds one fire-once
// token to the returned context instead of globally arming the shared executor.
func (a *ifaExecutorRetryArmedExecutor) Arm(ctx context.Context) context.Context {
	token := &ifaExecutorRetryFaultToken{}
	token.armed.Store(true)
	return context.WithValue(ctx, ifaExecutorRetryFaultContextKey{}, token)
}

// Execute fires only the token carried by this call's context, at most once.
func (a *ifaExecutorRetryArmedExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	if consumeIfaExecutorRetryFault(ctx) {
		return &ifaExecutorRetryArmedError{}
	}
	return a.inner.Execute(ctx, stmt)
}

// ExecuteGroup fires only the token carried by this group call's context, then
// delegates the complete atomic MERGE group on the retry attempt.
func (a *ifaExecutorRetryArmedExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if consumeIfaExecutorRetryFault(ctx) {
		return &ifaExecutorRetryArmedError{}
	}
	grouped, ok := a.inner.(sourcecypher.GroupExecutor)
	if !ok {
		return fmt.Errorf("ifa executor retry seam: inner executor does not support ExecuteGroup")
	}
	return grouped.ExecuteGroup(ctx, stmts)
}

func consumeIfaExecutorRetryFault(ctx context.Context) bool {
	token, ok := ctx.Value(ifaExecutorRetryFaultContextKey{}).(*ifaExecutorRetryFaultToken)
	return ok && token != nil && token.armed.CompareAndSwap(true, false)
}

// ifaExecutorRetryArmedError is the below-the-seam counterpart of
// sourcecypher's unexported ifaFaultExecutorRetryShapedError: its message
// contains "TransientError" (specifically
// "Neo.TransientError.Transaction.LockClientStopped-shaped") so the
// reducer's persistent sourcecypher.RetryingExecutor classifies it as
// retryable (isTransientNeo4jError matches on message content, not type, so
// no cross-package type assertion is needed) and retries in place.
type ifaExecutorRetryArmedError struct{}

func (e *ifaExecutorRetryArmedError) Error() string {
	return fmt.Sprintf(
		"ifa fault: %s (executor-retry, Neo.TransientError.Transaction.LockClientStopped-shaped) armed below the reducer's persistent RetryingExecutor",
		faultreplay.KindFailGraphWriteOnceThenSucceed,
	)
}
