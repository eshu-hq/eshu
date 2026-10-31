// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !windows

package eshulocal

import (
	"strconv"
	"strings"
)

// Local embedded-Postgres connection budget (#4456).
//
// The runtime invariant is max_connections >= (pool-holding services) *
// per-process pool ceiling + reserved/admin headroom. internal/runtime enforces
// exactly this for the Compose stacks in
// TestComposePostgresMaxConnectionsCoversPoolBudget; the embedded local server
// had been exempt and carried a bare "35" literal, which is how it came to sit
// below the budget for even a single pool-holding process.
//
// localPostgresPoolHolders enumerates the processes that can hold a pool
// against this server at the same time, rather than stating a count. A count is
// a number someone has to keep true; a list is one a reader can check against
// the code that starts these processes. Deriving the ceiling from len() means
// adding a holder here raises the ceiling in the same edit.
//
// The first two are the local supervisor's own children
// (internal/cli/localsupervisor/host.go: StartChildProcess for eshu-reducer
// and eshu-ingester). The two eshu-api entries are distinct concurrent
// processes: the MCP session child (localsupervisor.StartMCPSession, one per
// stdio or owner-attached HTTP session) and the short-lived loopback API that
// `eshu vuln-scan repo` launches against a running owner; the satellite
// eshu-mcp-server holds no Postgres pool (it reads through the session API),
// so it is not listed. The last entry is the vuln-scan bootstrap-index child
// (cmd/eshu/gotchas-read-surface-commands.md). All five take the shared
// 30-connection default, so the worst case is concurrent, not hypothetical.
//
// KNOWN LIMIT, stated because the list implies a completeness it cannot have:
// the local supervisor also opens its own connections with a bare
// sql.Open("pgx", dsn) and never calls runtime.ConfigurePostgresPool --
// config.go:216, content_search_indexes.go:42, iac_reachability_finalizer.go:28
// and progress.go:28. database/sql defaults MaxOpenConns to 0, i.e. unlimited,
// and the first two are long-lived for a whole authoritative run. So no finite
// ceiling here is strictly sound until those are bounded; this raises the floor
// from "guaranteed exhaustion" to "covers every capped holder", which is an
// improvement rather than a proof. That is the #4456 gap still open in the
// supervisor. Concurrent MCP sessions each add one more session eshu-api
// beyond the enumerated pair, the same unboundedness the old per-session
// stdio server had; the budget covers the enumerated worst case.
var localPostgresPoolHolders = [...]string{
	"eshu-reducer",
	"eshu-ingester",
	"eshu-api", // MCP session child (localsupervisor.StartMCPSession).
	"eshu-api", // vulnscan one-shot (vulnscan.prepareLocalRuntime).
	"eshu-bootstrap-index",
}

const (
	// localPostgresPoolHolderCount is a compile-time len() of the array above,
	// so the ceiling cannot drift from the list it is derived from.
	localPostgresPoolHolderCount = len(localPostgresPoolHolders)

	// localPostgresPerProcessPoolConns mirrors runtime.defaultPostgresMaxOpenConns.
	// It is duplicated rather than imported because internal/eshulocal/AGENTS.md
	// requires this package stay a leaf with no internal imports.
	localPostgresPerProcessPoolConns = 30

	// localPostgresReservedConns mirrors the reserved/admin headroom the Compose
	// budget leaves above the pool sum: superuser_reserved_connections (3 on the
	// embedded server), an operator psql session, the admin-status probe, and
	// transient tooling.
	localPostgresReservedConns = 20

	// LocalPostgresMaxConnections is the max_connections floor for the embedded
	// local server, derived from the budget above rather than chosen. It is a
	// floor and not the final value: ResolveLocalPostgresMaxConnections raises
	// it when the operator configures a larger per-process pool. Configuration
	// can never lower it, which is why the scope note in
	// docs/internal/evidence/eshulocal-connection-budget.md says the budget
	// cannot be reduced below the invariant by environment.
	LocalPostgresMaxConnections = localPostgresPoolHolderCount*localPostgresPerProcessPoolConns + localPostgresReservedConns
)

// localPostgresMaxOpenConnsEnv is the shared per-process pool knob. It is named
// here rather than imported because internal/eshulocal/AGENTS.md requires this
// package stay a leaf with no internal imports; runtime.LoadPostgresConfig reads
// the same key with the same default.
const localPostgresMaxOpenConnsEnv = "ESHU_POSTGRES_MAX_OPEN_CONNS"

// ResolveLocalPostgresMaxConnections returns the max_connections the embedded
// local server must start with for the pool size the operator has actually
// configured.
//
// WHY THIS IS NOT A CONSTANT. localsupervisor.ChildEnv ends in
// procexec.MergeEnvironment(procexec.Environ(), values) and does not set
// ESHU_POSTGRES_MAX_OPEN_CONNS, so every supervised child inherits the
// operator's value verbatim and sizes its own pool from it. The knob is
// documented and tunable upward -- docs/public/reference/postgres-tuning.md
// states the invariant as sum(pool-holding services * ESHU_POSTGRES_MAX_OPEN_CONNS)
// and tells operators to raise it when workers block on connections. A ceiling
// that hard-codes the default 30 therefore contradicts that documented contract
// the moment the knob is raised: five holders at 60 want 320 against a fixed
// 170, which is the same exhaustion this budget exists to prevent (#6603
// review).
//
// Parse rules mirror runtime.LoadPostgresConfig: unset or empty means the
// default, and a non-positive or unparseable value is not honoured. This
// function does not report that error because it sits on a config-construction
// path with no error return; it falls back to the default pool, which yields
// the documented floor. The child process that reads the same variable through
// runtime.LoadPostgresConfig still fails loudly on it, so the bad value is
// reported rather than swallowed -- it is simply not this function's job to
// report it.
func ResolveLocalPostgresMaxConnections(getenv func(string) string) int {
	perProcess := localPostgresPerProcessPoolConns
	if getenv != nil {
		if raw := strings.TrimSpace(getenv(localPostgresMaxOpenConnsEnv)); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				perProcess = parsed
			}
		}
	}
	derived := localPostgresPoolHolderCount*perProcess + localPostgresReservedConns
	if derived < LocalPostgresMaxConnections {
		return LocalPostgresMaxConnections
	}
	return derived
}
