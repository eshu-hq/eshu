// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// githubListingRetryConfig bounds retries of one org-listing page request.
// Only 5xx responses retry: a transient GitHub 502 must not end the whole
// ingester the way the 2026-10-13 outage did (#8076). A 4xx, a transport
// error, or a decode failure is final at once, because retrying bad
// credentials or a broken body cannot help.
type githubListingRetryConfig struct {
	// maxAttempts is the total page requests before the listing fails,
	// including the first attempt. It must be at least 1.
	maxAttempts int
	// baseBackoff is the wait before the first retry; each later wait
	// doubles. It must be positive.
	baseBackoff time.Duration
	// sleep waits out a backoff and reports the context error when the
	// context ends first. Nil means a context-aware timer. A test injects
	// its clock here so the suite never waits out a real backoff.
	sleep func(ctx context.Context, d time.Duration) error
}

// defaultGitHubListingRetry is the production retry budget: four attempts per
// page with 1s, 2s, 4s waits, so a listing survives a short GitHub outage
// without stretching a cycle by more than seconds.
var defaultGitHubListingRetry = githubListingRetryConfig{
	maxAttempts: 4,
	baseBackoff: time.Second,
}

// githubListingPage is one decoded org-listing page with the headers the
// completeness check reads.
type githubListingPage struct {
	items    []githubListingRecord
	linkNext []string
}

// githubListingRecord is one repository row of the org-listing payload.
type githubListingRecord struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Archived bool   `json:"archived"`
}

// sleepGitHubListingRetry waits out a listing backoff and returns the context
// error when the context ends first, so shutdown never waits out a retry.
func sleepGitHubListingRetry(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// fetchGitHubListingPage issues one org-listing page request, retrying a 5xx
// response with exponential backoff under retry. It reports the final status
// after maxAttempts 5xx responses, and a cancelled context ends the backoff
// wait without another attempt.
func fetchGitHubListingPage(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	org string,
	page int,
	token string,
	retry githubListingRetryConfig,
) (githubListingPage, error) {
	sleep := retry.sleep
	if sleep == nil {
		sleep = sleepGitHubListingRetry
	}
	attempts := retry.maxAttempts
	if attempts < 1 {
		attempts = 1
	}
	backoff := retry.baseBackoff
	if backoff <= 0 {
		backoff = time.Second
	}
	wait := backoff
	var lastStatus int
	for attempt := 1; ; attempt++ {
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			fmt.Sprintf("%s/orgs/%s/repos?per_page=%d&page=%d&type=all", baseURL, org, githubListingPageSize, page),
			nil,
		)
		if err != nil {
			return githubListingPage{}, fmt.Errorf("build GitHub org repos request: %w", err)
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		request.Header.Set("User-Agent", "eshu-go")

		response, err := client.Do(request)
		if err != nil {
			return githubListingPage{}, fmt.Errorf("list GitHub org repositories: %w", err)
		}
		var payload []githubListingRecord
		decodeErr := json.NewDecoder(response.Body).Decode(&payload)
		linkNext := response.Header.Values("Link")
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode < 500 {
			if response.StatusCode >= 300 {
				return githubListingPage{}, fmt.Errorf("list GitHub org repositories: status %d", response.StatusCode)
			}
			if decodeErr != nil {
				return githubListingPage{}, fmt.Errorf("decode GitHub org repositories: %w", decodeErr)
			}
			return githubListingPage{items: payload, linkNext: linkNext}, nil
		}
		lastStatus = response.StatusCode
		if attempt >= attempts {
			break
		}
		if sleepErr := sleep(ctx, wait); sleepErr != nil {
			return githubListingPage{}, fmt.Errorf("list GitHub org repositories: status %d", lastStatus)
		}
		wait *= 2
	}
	return githubListingPage{}, fmt.Errorf("list GitHub org repositories: status %d after %d attempts", lastStatus, attempts)
}
