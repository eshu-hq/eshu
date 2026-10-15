// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// TestWebhookSelectorGitLabGroupDropsNonGitLabTriggers proves the
// credential-safety guard: in gitlabGroup mode a GitHub or Bitbucket
// trigger would clone another host with the GitLab token, so it is marked
// failed with provider_not_selected before it builds a repository ID, while
// a GitLab trigger in the same batch still syncs as gitlab/<path>.
func TestWebhookSelectorGitLabGroupDropsNonGitLabTriggers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC)
	mkTrigger := func(id string, provider webhook.Provider, name string) webhook.StoredTrigger {
		return webhook.StoredTrigger{
			TriggerID: id,
			Trigger: webhook.Trigger{
				Provider:             provider,
				Decision:             webhook.DecisionAccepted,
				RepositoryExternalID: "42",
				RepositoryFullName:   name,
				DefaultBranch:        "main",
				TargetSHA:            "2222222222222222222222222222222222222222",
			},
		}
	}
	store := &stubWebhookTriggerStore{
		claimed: []webhook.StoredTrigger{
			mkTrigger("trigger-github", webhook.ProviderGitHub, "eshu-hq/eshu"),
			mkTrigger("trigger-bitbucket", webhook.ProviderBitbucket, "eshu-hq/worker"),
			mkTrigger("trigger-gitlab", webhook.ProviderGitLab, "acme/platform/svc"),
		},
	}
	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir: t.TempDir(), SourceMode: "gitlabGroup", GitlabGroup: "acme/platform",
			GitAuthMethod: "token", CloneDepth: 1,
		},
		Store:      store,
		Owner:      "collector-git",
		ClaimLimit: 10,
		Now:        func() time.Time { return now },
		SyncGit: func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
			if !reflect.DeepEqual(repositoryIDs, []string{"gitlab/acme/platform/svc"}) {
				t.Fatalf("repositoryIDs = %#v, want only the gitlab trigger's repository", repositoryIDs)
			}
			return GitSyncSelection{SelectedRepoPaths: []string{t.TempDir()}}, nil
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got := store.failedCall("provider_not_selected"); !reflect.DeepEqual(got, []string{"trigger-bitbucket", "trigger-github"}) {
		t.Fatalf("failed provider_not_selected = %#v, want the non-gitlab trigger IDs", got)
	}
	if !reflect.DeepEqual(store.handedOff, []string{"trigger-gitlab"}) {
		t.Fatalf("handedOff = %#v, want only the gitlab trigger", store.handedOff)
	}
}

// TestWebhookSelectorGitHubModeKeepsGitHubTriggers proves the guard is
// gitlabGroup-only: in githubOrg mode the same GitHub trigger syncs as
// before.
func TestWebhookSelectorGitHubModeKeepsGitHubTriggers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC)
	store := &stubWebhookTriggerStore{
		claimed: []webhook.StoredTrigger{{
			TriggerID: "trigger-1",
			Trigger: webhook.Trigger{
				Provider:             webhook.ProviderGitHub,
				Decision:             webhook.DecisionAccepted,
				RepositoryExternalID: "42",
				RepositoryFullName:   "eshu-hq/eshu",
				DefaultBranch:        "main",
				TargetSHA:            "2222222222222222222222222222222222222222",
			},
		}},
	}
	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir: t.TempDir(), SourceMode: "githubOrg", GithubOrg: "eshu-hq",
			GitAuthMethod: "token", CloneDepth: 1,
		},
		Store:      store,
		Owner:      "collector-git",
		ClaimLimit: 10,
		Now:        func() time.Time { return now },
		SyncGit: func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
			if !reflect.DeepEqual(repositoryIDs, []string{"eshu-hq/eshu"}) {
				t.Fatalf("repositoryIDs = %#v, want the github trigger's repository", repositoryIDs)
			}
			return GitSyncSelection{SelectedRepoPaths: []string{t.TempDir()}}, nil
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got := store.failedCall("provider_not_selected"); len(got) != 0 {
		t.Fatalf("failed provider_not_selected = %#v, want none in githubOrg mode", got)
	}
}
