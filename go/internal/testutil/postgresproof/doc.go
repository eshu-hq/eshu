// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package postgresproof creates isolated PostgreSQL databases for destructive
// live tests.
//
// OpenDisposableDatabase requires an explicit opt-in and a connection to the
// administrative postgres database. It creates a random database for one test,
// rejects application database DSNs before connecting, and force-drops only
// the generated database during cleanup.
package postgresproof
