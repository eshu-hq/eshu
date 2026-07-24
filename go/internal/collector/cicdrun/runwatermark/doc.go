// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runwatermark defines the durable cross-cycle run-watermark
// contract for the GitHub Actions runs poller (#5429).
//
// A watermark is a claim-fenced marker recording the newest provider run ID
// a claim cycle observed for one (scope_id, repository) target. Callers use
// it to DETECT, not resume, a cross-cycle collection gap: ghactionsruntime
// fetches a bounded, stateless window of the most recent runs on every
// claim, so when more than max_runs runs land between two cycles, the
// fetched window's oldest run can be newer than the stored watermark --
// meaning every run between them was never fetched by either cycle. See
// ghactionsruntime's run_watermark.go for the detection logic.
package runwatermark
