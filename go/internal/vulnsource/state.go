// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import "time"

// FreshnessState names the operator-facing collection state for one bounded
// vulnerability source target.
type FreshnessState string

const (
	// FreshnessNotConfigured means no durable source target state exists.
	FreshnessNotConfigured FreshnessState = "not_configured"
	// FreshnessPending means a target has been attempted but no terminal
	// observation has been recorded yet.
	FreshnessPending FreshnessState = "pending"
	// FreshnessFresh means the source target completed within its freshness
	// window, including zero-result successful observations.
	FreshnessFresh FreshnessState = "fresh"
	// FreshnessStale means the latest successful source observation is older
	// than the configured freshness window.
	FreshnessStale FreshnessState = "stale"
	// FreshnessRateLimited means upstream returned a throttling response and
	// the collector recorded a bounded next retry.
	FreshnessRateLimited FreshnessState = "rate_limited"
	// FreshnessFailed means the latest attempt failed without a successful
	// partial payload.
	FreshnessFailed FreshnessState = "failed"
	// FreshnessPartial means the source produced incomplete data.
	FreshnessPartial FreshnessState = "partial"
)

// TerminalStatus names the latest collection terminality for one source
// target. Retryable failures may still have a next_retry_at timestamp.
type TerminalStatus string

const (
	// TerminalPending means collection is in flight or queued for first proof.
	TerminalPending TerminalStatus = "pending"
	// TerminalSucceeded means source collection completed successfully.
	TerminalSucceeded TerminalStatus = "succeeded"
	// TerminalPartial means collection completed with incomplete source data.
	TerminalPartial TerminalStatus = "partial"
	// TerminalFailedRetryable means collection failed and may retry later.
	TerminalFailedRetryable TerminalStatus = "failed_retryable"
	// TerminalFailedTerminal means collection failed in a way that should not
	// create a retry loop for the same target state.
	TerminalFailedTerminal TerminalStatus = "failed_terminal"
)

const (
	// ErrorClassRateLimited classifies upstream throttling responses.
	ErrorClassRateLimited = "rate_limited"
	// ErrorClassRetryable classifies transient source or transport failures.
	ErrorClassRetryable = "retryable"
	// ErrorClassNonRetryable classifies source errors that should not retry.
	ErrorClassNonRetryable = "non_retryable"
)

// State is the durable checkpoint and freshness row for one configured or
// derived vulnerability source target.
type State struct {
	CollectorInstanceID string
	ScopeID             string
	Source              string
	Ecosystem           string
	WindowStart         time.Time
	WindowEnd           time.Time
	LastAttemptAt       time.Time
	LastSuccessAt       time.Time
	NextRetryAt         time.Time
	LastErrorClass      string
	FreshnessState      FreshnessState
	TerminalStatus      TerminalStatus
	ResultCount         int
	WarningCount        int
	UpdatedAt           time.Time
}
