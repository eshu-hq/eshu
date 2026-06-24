// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const defaultGitHubRateLimitDelay = time.Minute

// RateLimitError carries bounded GitHub rate-limit retry guidance without
// repository names, URLs, response bodies, or credential values.
type RateLimitError struct {
	StatusCode int
	RetryAfter time.Duration
	Reset      time.Time
}

func (e RateLimitError) Error() string {
	return ErrRateLimited.Error()
}

// Is lets callers use errors.Is(err, ErrRateLimited).
func (e RateLimitError) Is(target error) bool {
	return target == ErrRateLimited
}

// RetryAfterDelay returns provider retry guidance for shared retry handling.
func (e RateLimitError) RetryAfterDelay() time.Duration {
	return e.RetryAfter
}

func rateLimitErrorFromResponse(response *http.Response, now time.Time) (RateLimitError, bool) {
	if response == nil || !githubRateLimited(response) {
		return RateLimitError{}, false
	}
	retryAfter, reset := githubRetryAfter(response.Header, now)
	if retryAfter <= 0 {
		retryAfter = defaultGitHubRateLimitDelay
	}
	return RateLimitError{
		StatusCode: response.StatusCode,
		RetryAfter: retryAfter,
		Reset:      reset,
	}, true
}

func githubRateLimited(response *http.Response) bool {
	if response.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if response.StatusCode != http.StatusForbidden {
		return false
	}
	return strings.TrimSpace(response.Header.Get("X-RateLimit-Remaining")) == "0" ||
		strings.TrimSpace(response.Header.Get("Retry-After")) != ""
}

func githubRetryAfter(header http.Header, now time.Time) (time.Duration, time.Time) {
	reset, _ := parseGitHubRateLimitReset(header.Get("X-RateLimit-Reset"))
	if retryAfter := parseGitHubRetryAfter(header.Get("Retry-After"), now); retryAfter > 0 {
		return retryAfter, reset
	}
	if reset.IsZero() {
		return 0, time.Time{}
	}
	delay := reset.Sub(now)
	if delay <= 0 {
		return 0, reset
	}
	return delay, reset
}

func parseGitHubRetryAfter(value string, now time.Time) time.Duration {
	return sdk.ParseRetryAfter(value, now)
}

func parseGitHubRateLimitReset(value string) (time.Time, bool) {
	epochSeconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || epochSeconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(epochSeconds, 0).UTC(), true
}
