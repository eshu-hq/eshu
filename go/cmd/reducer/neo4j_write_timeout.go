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

// neo4jCanonicalWriteTimeout returns the server-side transaction timeout for
// Neo4j graph writes. Neo4j applies ESHU_CANONICAL_WRITE_TIMEOUT only when an
// operator sets it to a valid positive duration; unset or invalid values
// return zero so a Neo4j deployment that never configured the budget keeps
// its unbounded transactions instead of inheriting the NornicDB 30s default.
// The driver sends the value with each transaction (neo4j.WithTxTimeout) and
// the server terminates and rolls back a transaction that exceeds it, which
// bounds how long a write can outlive the lease that admitted it.
func neo4jCanonicalWriteTimeout(getenv func(string) string) time.Duration {
	parsed, err := time.ParseDuration(strings.TrimSpace(getenv(canonicalWriteTimeoutEnv)))
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

// warnUnboundedNeo4jWriteTimeout logs one structured WARN when the graph
// backend is Neo4j and ESHU_CANONICAL_WRITE_TIMEOUT is unset or invalid. In
// that state Neo4j writes have no transaction timeout, so a hung write can
// outlive the lease that admitted it; the WARN makes that visible to an
// operator at startup. NornicDB always has a timeout and never warns.
func warnUnboundedNeo4jWriteTimeout(
	logger *slog.Logger,
	graphBackend runtimecfg.GraphBackend,
	getenv func(string) string,
) {
	if logger == nil || graphBackend == runtimecfg.GraphBackendNornicDB || neo4jCanonicalWriteTimeout(getenv) > 0 {
		return
	}
	logger.Warn(
		"graph writes have no transaction timeout; set ESHU_CANONICAL_WRITE_TIMEOUT to bound them below the lease TTLs",
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
// forwards Execute, ExecuteGroup, and ExecuteProbe. A zero timeout (unset)
// returns inner unchanged.
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
