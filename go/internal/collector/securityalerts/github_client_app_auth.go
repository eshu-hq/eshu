// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerts

import (
	"context"
	"errors"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/github/app"
)

// resolveToken returns the bearer credential for one listing call. A
// configured provider mints it fresh on every call so sequential claims never
// reuse an hourly installation token; otherwise the static configured token
// is used.
func (c GitHubDependabotClient) resolveToken(ctx context.Context, operation string) (string, error) {
	if c.tokenProvider == nil {
		return c.token, nil
	}
	token, err := c.tokenProvider(ctx)
	if err != nil {
		return "", classifyMintError(err, operation)
	}
	if strings.TrimSpace(token) == "" {
		return "", GitHubDependabotError{
			Message:              "github app token provider returned no token",
			Operation:            operation,
			FailureClassOverride: GitHubDependabotFailureAuthDenied,
		}
	}
	return token, nil
}

// classifyMintError maps a token-provider failure onto the bounded alert
// failure vocabulary without carrying credentials: an unknown installation
// or bad key is a terminal denial, an outlasted transient or rate limit
// stays retryable, and a cancelled context stays retryable.
func classifyMintError(err error, operation string) GitHubDependabotError {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return GitHubDependabotError{
			Message:              "mint github app installation token",
			Operation:            operation,
			FailureClassOverride: GitHubDependabotFailureRetryable,
			Cause:                err,
		}
	}
	var retryable *app.RetryableError
	if errors.As(err, &retryable) {
		class := GitHubDependabotFailureRetryable
		if retryable.Class == app.ClassRateLimited {
			class = GitHubDependabotFailureRateLimited
		}
		return GitHubDependabotError{
			Message:              "mint github app installation token",
			Operation:            operation,
			FailureClassOverride: class,
			RateLimit:            GitHubRateLimitInfo{RetryAfter: retryable.RetryAfter},
			Cause:                err,
		}
	}
	return GitHubDependabotError{
		Message:              "mint github app installation token",
		Operation:            operation,
		FailureClassOverride: GitHubDependabotFailureAuthDenied,
		Cause:                err,
	}
}
