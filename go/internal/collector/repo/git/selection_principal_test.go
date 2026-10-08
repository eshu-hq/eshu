// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
)

func TestGitHubOrgSelectionRequestSelectorNamesTheCredentialPrincipal(t *testing.T) {
	t.Parallel()

	const token = "ghp_principaltokenvalue0123456789"
	rules := []membership.Rule{{Kind: "regex", Value: "^acme/(api|old)"}}
	cases := []struct {
		name   string
		config RepoSyncConfig
		want   string
	}{
		{
			name:   "GitHub App",
			config: RepoSyncConfig{GitAuthMethod: "githubApp", GitHubAppID: "123", GitHubAppInstallation: "456", GitToken: token},
			want:   "app:123:456",
		},
		{name: "token", config: RepoSyncConfig{GitAuthMethod: "token", GitToken: token}, want: membership.TokenPrincipal(token)},
		{name: "ssh with a listing token", config: RepoSyncConfig{GitAuthMethod: "ssh", GitToken: token}, want: membership.TokenPrincipal(token)},
		{name: "no credential", config: RepoSyncConfig{GitAuthMethod: "none"}, want: "anonymous"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := tc.config
			config.SourceMode, config.GithubOrg = "githubOrg", "acme"
			config.RepositoryRules = []RepoSyncRepositoryRule{{Kind: "regex", Value: "^acme/(api|old)"}}
			got := githubOrgSelectionRequest(config, RepositorySelection{}, time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)).Selector
			want := membership.NewGitHubOrgSelector("githubOrg", "acme", rules, false, tc.want)
			if got != want || !strings.HasSuffix(got.ID, "@"+tc.want) {
				t.Fatalf("selector = %+v, want %+v ending in @%s", got, want, tc.want)
			}
		})
	}
}

// passthroughObserver records each request before handing it to the real
// observer, so the test sees the selector id the store and logs received.
type passthroughObserver struct {
	next     membership.Observer
	requests []membership.Request
}

func (o *passthroughObserver) Observe(ctx context.Context, request membership.Request) membership.Result {
	o.requests = append(o.requests, request)
	return o.next.Observe(ctx, request)
}

// TestSelectionPrincipalNeverCarriesTheToken runs a token-authenticated
// githubOrg cycle through the real observer, with a failing store so the
// error path logs too, and proves the token value appears in neither the
// selector id nor any log line.
func TestSelectionPrincipalNeverCarriesTheToken(t *testing.T) {
	t.Parallel()

	const token = "ghp_neverloggedtokenvalue0123456789"
	logs := &bytes.Buffer{}
	observer := &passthroughObserver{next: membership.Observer{
		Store:  failingSelectionStore{},
		Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}}
	selector := githubOrgObservationSelector(t, 1, 0, observer)
	selector.Config.GitAuthMethod, selector.Config.GitToken = "token", token
	discover := selector.DiscoverSelection
	selector.DiscoverSelection = func(ctx context.Context, config RepoSyncConfig, listingToken string) (RepositorySelection, error) {
		discovered, err := discover(ctx, config, listingToken)
		discovered.ListingComplete = true
		return discovered, err
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	if len(observer.requests) != 1 {
		t.Fatalf("observations = %d, want 1", len(observer.requests))
	}
	id := observer.requests[0].Selector.ID
	if strings.Contains(id, token) || !strings.HasSuffix(id, "@"+membership.TokenPrincipal(token)) {
		t.Fatalf("selector id = %q, want the salted token hash principal and never the token", id)
	}
	if !strings.Contains(logs.String(), "git_repository_selection_store_failed") {
		t.Fatalf("logs = %s, want the store failure logged", logs.String())
	}
	if strings.Contains(logs.String(), token) {
		t.Fatalf("logs carry the token: %s", logs.String())
	}
}
