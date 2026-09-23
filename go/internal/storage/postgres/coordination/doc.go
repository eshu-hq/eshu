// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package coordination holds the bounded wait and retry loops the Postgres
// schema migrator uses when another session is in its way (#6956): waiting
// for a bootstrapper that owns the schema advisory lock, and re-running a
// migration statement that lost a lock race (SQLSTATE 55P03).
//
// The loops are pure: they take a Locker, a Sleeper, and a clock, and log
// through the caller's logger with the bootstrap.postgres.ownership.* and
// bootstrap.postgres.migration.lock_* events. The advisory lock itself, the
// bootstrap session, and the migration ledger stay in the postgres root
// package with the types they guard, so this package imports nothing from
// it and the root's package-private locker contract is unchanged.
//
// It also classifies one statement shape that must NOT go through the retry
// loop above: a bare CREATE/DROP INDEX CONCURRENTLY statement is exempt from
// lock_timeout entirely rather than retried, because retrying restarts its
// table scan from zero every attempt (#7004). ConcurrentIndexBuildLockTimeout
// and IsSoleConcurrentIndexStatement make that call; RunWithConcurrentIndexBuildLogging
// gives the exempted statement its own start/finish log pair since it has no
// lock_timeout left to retry after.
package coordination
