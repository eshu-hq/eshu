// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitlabciruntime

import (
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const defaultGitLabRateLimitDelay = time.Minute

// RateLimitError carries bounded GitLab rate-limit retry guidance without
// project paths, URLs, response bodies, or credential values.
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

// rateLimitErrorFromResponse classifies a GitLab response as rate-limited.
// GitLab signals throttling with HTTP 429 and, on the throttled response,
// the RateLimit-ResetTime (RFC1123 date) and/or Retry-After (seconds)
// headers (https://docs.gitlab.com/administration/settings/user_and_ip_rate_limits/#response-headers) --
// a different status code and header pair than GitHub Actions' 403-with-
// X-RateLimit-Remaining-0-or-Retry-After convention (rate_limit.go's
// GitHub counterpart), so this is not shared code between the two provider
// runtimes despite the similar shape.
func rateLimitErrorFromResponse(response *http.Response, now time.Time) (RateLimitError, bool) {
	if response == nil || response.StatusCode != http.StatusTooManyRequests {
		return RateLimitError{}, false
	}
	retryAfter, reset := gitlabRetryAfter(response.Header, now)
	if retryAfter <= 0 {
		retryAfter = defaultGitLabRateLimitDelay
	}
	return RateLimitError{
		StatusCode: response.StatusCode,
		RetryAfter: retryAfter,
		Reset:      reset,
	}, true
}

func gitlabRetryAfter(header http.Header, now time.Time) (time.Duration, time.Time) {
	reset, hasReset := parseGitLabRateLimitResetTime(header.Get("RateLimit-ResetTime"))
	if retryAfter := sdk.ParseRetryAfter(header.Get("Retry-After"), now); retryAfter > 0 {
		return retryAfter, reset
	}
	if !hasReset {
		return 0, time.Time{}
	}
	delay := reset.Sub(now)
	if delay <= 0 {
		return 0, reset
	}
	return delay, reset
}

// parseGitLabRateLimitResetTime parses the RFC1123-formatted RateLimit-ResetTime
// header GitLab documents for throttled (429) responses.
func parseGitLabRateLimitResetTime(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	parsed, err := http.ParseTime(trimmed)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}
