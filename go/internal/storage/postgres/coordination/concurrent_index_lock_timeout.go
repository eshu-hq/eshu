// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordination

import (
	"context"
	"database/sql"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// concurrentIndexOnlyStatementPattern anchors on a bare CREATE INDEX
// CONCURRENTLY / DROP INDEX CONCURRENTLY at the start of a statement, once
// its SQL line comments and surrounding whitespace are stripped.
var concurrentIndexOnlyStatementPattern = regexp.MustCompile(
	`(?is)^(?:CREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY|DROP\s+INDEX\s+CONCURRENTLY)\s`,
)

// IsSoleConcurrentIndexStatement reports whether query, once its SQL line
// comments are stripped, holds exactly one statement and that statement is a
// bare CREATE INDEX CONCURRENTLY or DROP INDEX CONCURRENTLY (#7004).
//
// Postgres refuses to run CONCURRENTLY inside a multi-statement simple-query
// string, so every migration file that uses it already holds exactly one
// such statement and nothing else (see the root package's
// migrations/113_..._v2_idx.sql and 114_drop_..._legacy.sql, whose header
// comments record that constraint). This check stays conservative for any SQL
// text Postgres would actually execute rather than relying on that
// convention holding forever: a statement it cannot prove is a lone CIC/DIC
// -- combined with any other statement, or anything unrecognized -- keeps
// the caller's lock_timeout. Line-comment stripping can be fooled by a `--`
// or `$$...$$` string/dollar-quoted literal that happens to hide a
// following `;` from view, classifying a combined statement as sole; that
// false positive is inert, because Postgres itself refuses to run
// CONCURRENTLY inside the resulting multi-statement string ("cannot run
// inside a transaction block"), so no non-concurrent DDL ever executes
// without lock_timeout as a result. See TestIsSoleConcurrentIndexStatement's
// "comment-like text inside a string literal" case.
func IsSoleConcurrentIndexStatement(query string) bool {
	trimmed := strings.TrimSpace(stripWholeLineSQLComments(query))
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
	if trimmed == "" || strings.Contains(trimmed, ";") {
		return false
	}
	return concurrentIndexOnlyStatementPattern.MatchString(trimmed + " ")
}

// stripWholeLineSQLComments drops every line that is entirely a `--` comment
// once trimmed, matching the root package's migrations/ convention of
// whole-line header comments ahead of a statement (root package's
// schema_index_replay_test.go carries its own test-only copy of the same
// logic under a different name; a _test.go symbol there is not linkable from
// this production package either way).
func stripWholeLineSQLComments(sql string) string {
	lines := strings.Split(sql, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// ConcurrentIndexBuildPlan classifies query once and returns both the
// effective lock_timeout to apply and whether query is a bare CIC/DIC
// statement, so a caller that needs both values -- the timeout to apply and
// whether to run RunWithConcurrentIndexBuildLogging -- gets them from a
// single classification instead of calling IsSoleConcurrentIndexStatement
// twice and risking the two calls diverging (#7004 review thread
// 4085764532). This is production's only call site for
// IsSoleConcurrentIndexStatement; both adapters.go and schema_bootstrap_lock.go
// call this function exactly once per statement.
//
// The effective lock_timeout is disabled (0) for a bare CREATE/DROP INDEX
// CONCURRENTLY statement, the caller's requested bound for everything else
// (#7004).
//
// A concurrent index build takes ShareUpdateExclusiveLock on its table. That
// mode does not conflict with the RowExclusiveLock ordinary INSERT/UPDATE/
// DELETE statements take, so a build waiting on either of its two phases --
// acquiring that initial lock, or its internal wait for transactions with an
// older snapshot to finish -- never blocks application writers. Disabling
// lock_timeout avoids the alternative failure mode: a per-statement timeout
// would cancel the build the moment any transaction in the database has been
// open longer than the timeout, which a busy database can make true
// continuously, and every retry restarts the table scan from zero and never
// converges (see issue #7004's evidence).
//
// This is NOT bounded by the schema bootstrap Job's activeDeadlineSeconds
// (deploy/helm/eshu/values.yaml): that deadline kills the bootstrap CLIENT
// process only. Both bootstrap binaries (eshu-bootstrap-data-plane,
// bootstrap-index) run with a background context and no signal handling, so
// killing the pod does not cancel the statement on the Postgres server -- the
// backend keeps building (or waiting on a conflicting DDL/VACUUM lock) to
// completion or error, still holding the session schema advisory lock the
// whole time. A build that completes leaves a VALID index and releases that
// lock; the NEXT bootstrap run then waits on the same advisory lock (the
// ownership wait WaitForOwnership already covers) and can itself need
// retrying or investigating if the orphan ran unusually long. A build that is
// canceled, terminated, or errors leaves an INVALID index instead, which the
// next run's invalid-index cleanup drops (also without lock_timeout; see
// dropInvalidConcurrentIndexes in the root package) before rebuilding it.
func ConcurrentIndexBuildPlan(query string, requested time.Duration) (lockTimeout time.Duration, isConcurrentIndexBuild bool) {
	if IsSoleConcurrentIndexStatement(query) {
		return 0, true
	}
	return requested, false
}

// LockTimeoutSetting renders d for `SELECT set_config('lock_timeout', $1,
// false)`. Zero is sent as the literal Postgres GUC value "0" (disabled)
// rather than time.Duration's "0s" string form, matching the literal the
// root package's resetSchemaLockTimeout already sends.
func LockTimeoutSetting(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	return d.String()
}

// RunWithConcurrentIndexBuildLogging invokes run, and when
// concurrentIndexBuild is true logs its start and finish (with duration_ms
// and whether it failed) as
// bootstrap.postgres.migration.concurrent_index_build.starting/finished
// (#7004). A concurrent index build has no lock_timeout to retry after, so
// it never gets the generic migration.lock_wait/lock_recovered pair; without
// this, an operator watching the bootstrap Job would see it stall with no
// signal that a specific statement is legitimately running long. Non-CIC
// statements pass through unlogged, unchanged from before #7004.
func RunWithConcurrentIndexBuildLogging(
	ctx context.Context,
	logger *slog.Logger,
	concurrentIndexBuild bool,
	run func() (sql.Result, error),
) (sql.Result, error) {
	if !concurrentIndexBuild {
		return run()
	}
	started := time.Now()
	logger.InfoContext(ctx, "postgres schema migration concurrent index build starting without a lock_timeout",
		telemetry.EventAttr("bootstrap.postgres.migration.concurrent_index_build.starting"),
	)
	result, err := run()
	logger.InfoContext(ctx, "postgres schema migration concurrent index build finished",
		telemetry.EventAttr("bootstrap.postgres.migration.concurrent_index_build.finished"),
		"duration_ms", time.Since(started).Milliseconds(),
		"failed", err != nil,
	)
	return result, err
}
