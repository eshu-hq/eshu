// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

// Fixed response bodies for a stats or coverage read that failed after the
// selector resolved (#7626). A backend error quotes SQL, Cypher, and host
// detail, so the client sees only one of these; the error itself goes to the
// request span through tracing.WriteServerFailure.
const (
	repositoryStatsQueryFailedMessage    = "repository stats query failed"
	repositoryCoverageQueryFailedMessage = "repository coverage query failed"
)
