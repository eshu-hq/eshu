// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"strings"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// unboundedNeo4jWriteTimeoutEvent names the startup WARN logged when Neo4j
// graph writes run without a transaction timeout.
const unboundedNeo4jWriteTimeoutEvent = "graph.write_timeout.unbounded"

// defaultNeo4jCanonicalWriteTimeout bounds Neo4j canonical writes when
// ESHU_CANONICAL_WRITE_TIMEOUT is unset or invalid (issue #7471). It is
// ~15x over the largest locally measured full write (19.6s at 50k markers;
// the ops-qa 25k-marker shape measured 14.4s here), confirmed by the arbiter;
// the reference-corpus measurement is NOT_CHECKED on this machine. An
// explicit non-positive duration keeps the deliberate unbounded opt-out.
const defaultNeo4jCanonicalWriteTimeout = 300 * time.Second

// neo4jCanonicalWriteTimeout returns the server-side transaction timeout for
// Neo4j graph writes. A configured positive ESHU_CANONICAL_WRITE_TIMEOUT wins;
// an unset or invalid value falls back to defaultNeo4jCanonicalWriteTimeout so
// a typo never silently unbounds writes, and only an explicit non-positive
// duration returns zero (the deliberate unbounded opt-out). The driver sends
// the value with each transaction (neo4j.WithTxTimeout) and the server
// terminates and rolls back a transaction that exceeds it, which bounds how
// long a write can outlive the lease that admitted it.
func neo4jCanonicalWriteTimeout(getenv func(string) string) time.Duration {
	raw := strings.TrimSpace(getenv(canonicalWriteTimeoutEnv))
	if raw == "" {
		return defaultNeo4jCanonicalWriteTimeout
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return defaultNeo4jCanonicalWriteTimeout
	}
	if parsed <= 0 {
		return 0
	}
	return parsed
}

// warnUnboundedNeo4jWriteTimeout logs one structured WARN when the graph
// backend is Neo4j and the effective ESHU_CANONICAL_WRITE_TIMEOUT is zero,
// which under the #7471 default happens only when the operator explicitly
// opts out with a non-positive duration. In that state Neo4j writes have no
// transaction timeout, so a hung write can outlive the lease that admitted
// it; the WARN makes that visible to an operator at startup. NornicDB always
// has a timeout and never warns.
func warnUnboundedNeo4jWriteTimeout(
	logger *slog.Logger,
	graphBackend runtimecfg.GraphBackend,
	getenv func(string) string,
) {
	if logger == nil || graphBackend == runtimecfg.GraphBackendNornicDB || neo4jCanonicalWriteTimeout(getenv) > 0 {
		return
	}
	logger.Warn(
		"graph writes have no transaction timeout because ESHU_CANONICAL_WRITE_TIMEOUT explicitly opts out; unset it or set a positive duration to bound writes below the lease TTLs",
		telemetry.EventAttr(unboundedNeo4jWriteTimeoutEvent),
		"graph_backend", string(graphBackend),
		"env_var", canonicalWriteTimeoutEnv,
	)
}

// boundNeo4jWrites wraps a Neo4j graph-write executor in the TimeoutExecutor
// NornicDB already uses on this path. Its client deadline fires at the
// configured timeout, just before the server terminates the transaction, so a
// write that outlives ESHU_CANONICAL_WRITE_TIMEOUT ends as a retryable
// graph_write_timeout that requeues, not a terminal dead letter. The wrapper
// forwards Execute, ExecuteGroup, and ExecuteProbe. A zero timeout (explicit
// unbounded opt-out) returns inner unchanged.
func boundNeo4jWrites(inner sourcecypher.Executor, timeout time.Duration) sourcecypher.Executor {
	if timeout <= 0 {
		return inner
	}
	return sourcecypher.TimeoutExecutor{
		Inner:       inner,
		Timeout:     timeout,
		TimeoutHint: canonicalWriteTimeoutEnv,
	}
}
