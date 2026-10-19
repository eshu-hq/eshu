// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import "database/sql"

// concurrentClaimerPoolSlack is the headroom above the claimer count a
// raised proof pool keeps for seeding, assertions, and cleanup queries that
// run beside the claim loops.
const concurrentClaimerPoolSlack = 4

// RaisePoolForConcurrentClaimers sizes a proof pool so concurrent claim
// loops never serialize behind one connection: it allows one pinned
// connection per claimer plus concurrentClaimerPoolSlack spare connections,
// and keeps the same count idle so a released claimer connection is not
// closed and reopened between claims. It returns the open-connection limit
// it set. A claimer count below one is treated as one claimer.
func RaisePoolForConcurrentClaimers(database *sql.DB, claimers int) int {
	if claimers < 1 {
		claimers = 1
	}
	limit := claimers + concurrentClaimerPoolSlack
	database.SetMaxOpenConns(limit)
	database.SetMaxIdleConns(limit)
	return limit
}
