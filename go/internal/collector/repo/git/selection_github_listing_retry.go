// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
)

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

// fetchGitHubListingPage issues one org-listing page request under retry (see
// rest.Do for the fault classes). A transient fault retries up to
// retry.MaxAttempts and then returns *rest.RetryableError; a final fault
// returns a plain error. A cancelled context ends a backoff wait without
// another attempt.
func fetchGitHubListingPage(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	org string,
	page int,
	token string,
	retry rest.Config,
) (githubListingPage, error) {
	var fetched githubListingPage
	call := rest.Call{
		Operation:     rest.OperationList,
		FailurePrefix: "list GitHub org repositories",
		DecodePrefix:  "decode GitHub org repositories",
		NewRequest: func(ctx context.Context) (*http.Request, error) {
			request, err := http.NewRequestWithContext(
				ctx,
				http.MethodGet,
				fmt.Sprintf("%s/orgs/%s/repos?per_page=%d&page=%d&type=all", baseURL, org, githubListingPageSize, page),
				nil,
			)
			if err != nil {
				return nil, fmt.Errorf("build GitHub org repos request: %w", err)
			}
			request.Header.Set("Accept", "application/vnd.github+json")
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
			request.Header.Set("User-Agent", "eshu-go")
			return request, nil
		},
		Read: func(response *http.Response) error {
			var payload []githubListingRecord
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				return err
			}
			fetched = githubListingPage{items: payload, linkNext: response.Header.Values("Link")}
			return nil
		},
	}
	if err := rest.Do(ctx, client, retry, call); err != nil {
		return githubListingPage{}, err
	}
	return fetched, nil
}
