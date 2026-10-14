// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rest

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimitHint reads a rate-limit wait from response headers. It returns the
// wait and whether one was named, and whether the headers signal a rate limit
// at all: retry-after present, or x-ratelimit-remaining 0. retry-after
// (seconds or an HTTP date) wins over x-ratelimit-reset (a Unix timestamp,
// used only when remaining is 0).
func RateLimitHint(headers http.Header, now time.Time) (wait time.Duration, known bool, signalled bool) {
	retryAfter := strings.TrimSpace(headers.Get("Retry-After"))
	exhausted := strings.TrimSpace(headers.Get("X-RateLimit-Remaining")) == "0"
	signalled = retryAfter != "" || exhausted
	if retryAfter != "" {
		if seconds, err := strconv.ParseInt(retryAfter, 10, 64); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second, true, signalled
		}
		if at, err := http.ParseTime(retryAfter); err == nil {
			return max(at.Sub(now), 0), true, signalled
		}
	}
	if exhausted {
		if reset, err := strconv.ParseInt(strings.TrimSpace(headers.Get("X-RateLimit-Reset")), 10, 64); err == nil {
			return max(time.Unix(reset, 0).Sub(now), 0), true, signalled
		}
	}
	return 0, false, signalled
}
