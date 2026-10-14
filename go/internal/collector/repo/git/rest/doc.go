// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rest runs the GitHub REST calls that repository discovery makes
// (the org listing page and the GitHub App installation token mint) under one
// bounded retry, and owns the failure contract the git collector builds on
// (#8084).
//
// Do classifies each attempt: a 5xx, a transient transport fault (including a
// body that dies mid-read and an HTTP/2 stream reset), 429, and a 403 that
// carries a retry-after header or x-ratelimit-remaining 0 retry; 401, any
// other 4xx, a 3xx, and invalid JSON on a fully read body are final at once. A
// retryable fault that outlasts the budget returns *RetryableError, which
// implements collector.RetryableError. Failures turns repeated retryable
// cycles into a fatal error at sdk.MaxConsecutiveTransportFailures, so a
// permanent fault still ends the process.
//
// The package emits eshu_dp_github_api_attempts_total and
// eshu_dp_github_api_retries_total, the github.api_retry span event, and a
// WARN log per retry through the observer WithObserver puts on the context.
// It imports no git collector types and no collector package, so the git
// package imports it without a cycle.
package rest
