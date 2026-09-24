// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"time"
)

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
