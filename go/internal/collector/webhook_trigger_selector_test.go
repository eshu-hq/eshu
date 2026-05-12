package collector

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

func TestWebhookTriggerRepositorySelectorSyncsOnlyClaimedRepositories(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC)
	store := &stubWebhookTriggerStore{
		claimed: []webhook.StoredTrigger{
			{
				TriggerID: "trigger-1",
				Trigger: webhook.Trigger{
					Provider:             webhook.ProviderGitHub,
					Decision:             webhook.DecisionAccepted,
					RepositoryExternalID: "42",
					RepositoryFullName:   "eshu-hq/eshu",
					DefaultBranch:        "main",
					TargetSHA:            "2222222222222222222222222222222222222222",
				},
			},
			{
				TriggerID: "trigger-2",
				Trigger: webhook.Trigger{
					Provider:             webhook.ProviderGitHub,
					Decision:             webhook.DecisionAccepted,
					RepositoryExternalID: "42",
					RepositoryFullName:   "eshu-hq/eshu",
					DefaultBranch:        "main",
					TargetSHA:            "3333333333333333333333333333333333333333",
				},
			},
		},
	}
	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:      t.TempDir(),
			SourceMode:    "explicit",
			GitAuthMethod: "token",
			CloneDepth:    1,
		},
		Store:      store,
		Owner:      "collector-git",
		ClaimLimit: 10,
		Now:        func() time.Time { return now },
		SyncGit: func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
			if !reflect.DeepEqual(repositoryIDs, []string{"eshu-hq/eshu"}) {
				t.Fatalf("repositoryIDs = %#v, want one targeted repo", repositoryIDs)
			}
			return GitSyncSelection{SelectedRepoPaths: []string{t.TempDir()}}, nil
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if len(batch.Repositories) != 1 {
		t.Fatalf("len(batch.Repositories) = %d, want 1", len(batch.Repositories))
	}
	if !reflect.DeepEqual(store.handedOff, []string{"trigger-1", "trigger-2"}) {
		t.Fatalf("handedOff = %#v, want claimed trigger IDs", store.handedOff)
	}
}

func TestWebhookTriggerRepositorySelectorReturnsEmptyBatchWhenNoTriggers(t *testing.T) {
	t.Parallel()

	store := &stubWebhookTriggerStore{}
	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:  store,
		Owner:  "collector-git",
		Now:    func() time.Time { return time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC) },
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			t.Fatal("SyncGit called, want no call without triggers")
			return GitSyncSelection{}, nil
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if len(batch.Repositories) != 0 {
		t.Fatalf("len(batch.Repositories) = %d, want 0", len(batch.Repositories))
	}
}

func TestWebhookTriggerRepositorySelectorMarksTriggersFailedWhenSyncFails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC)
	store := &stubWebhookTriggerStore{
		claimed: []webhook.StoredTrigger{{
			TriggerID: "trigger-1",
			Trigger: webhook.Trigger{
				Provider:           webhook.ProviderGitHub,
				Decision:           webhook.DecisionAccepted,
				RepositoryFullName: "eshu-hq/eshu",
			},
		}},
	}
	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:  store,
		Owner:  "collector-git",
		Now:    func() time.Time { return now },
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			return GitSyncSelection{}, errors.New("git unavailable")
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err == nil {
		t.Fatal("SelectRepositories() error = nil, want sync error")
	}
	if !reflect.DeepEqual(store.failed, []string{"trigger-1"}) {
		t.Fatalf("failed = %#v, want claimed trigger IDs", store.failed)
	}
	if got := store.failedCall("sync_git_failed"); !reflect.DeepEqual(got, []string{"trigger-1"}) {
		t.Fatalf("failed sync_git_failed = %#v, want claimed trigger IDs", got)
	}
}

func TestWebhookTriggerRepositorySelectorSyncsProviderScopedRepositories(t *testing.T) {
	t.Parallel()

	reposDir := t.TempDir()
	store := &stubWebhookTriggerStore{
		claimed: []webhook.StoredTrigger{
			{
				TriggerID: "trigger-gitlab",
				Trigger: webhook.Trigger{
					Provider:           webhook.ProviderGitLab,
					Decision:           webhook.DecisionAccepted,
					RepositoryFullName: "eshu-hq/eshu",
				},
			},
			{
				TriggerID: "trigger-bitbucket",
				Trigger: webhook.Trigger{
					Provider:           webhook.ProviderBitbucket,
					Decision:           webhook.DecisionAccepted,
					RepositoryFullName: "eshu-hq/worker",
				},
			},
		},
	}
	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{ReposDir: reposDir, SourceMode: "explicit", CloneDepth: 1},
		Store:  store,
		Owner:  "collector-git",
		Now:    func() time.Time { return time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC) },
		SyncGit: func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
			want := []string{"bitbucket/eshu-hq/worker", "gitlab/eshu-hq/eshu"}
			if !reflect.DeepEqual(repositoryIDs, want) {
				t.Fatalf("repositoryIDs = %#v, want provider-scoped ids %#v", repositoryIDs, want)
			}
			return GitSyncSelection{SelectedRepoPaths: []string{
				filepath.Join(reposDir, "bitbucket", "eshu-hq", "worker"),
				filepath.Join(reposDir, "gitlab", "eshu-hq", "eshu"),
			}}, nil
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if len(batch.Repositories) != 2 {
		t.Fatalf("len(batch.Repositories) = %d, want 2", len(batch.Repositories))
	}
	gotRemoteURLs := remoteURLsFromSelectedRepositories(batch.Repositories)
	wantRemoteURLs := []string{
		"https://bitbucket.org/eshu-hq/worker.git",
		"https://gitlab.com/eshu-hq/eshu.git",
	}
	if !reflect.DeepEqual(gotRemoteURLs, wantRemoteURLs) {
		t.Fatalf("remote URLs = %#v, want %#v", gotRemoteURLs, wantRemoteURLs)
	}
	if !reflect.DeepEqual(store.handedOff, []string{"trigger-bitbucket", "trigger-gitlab"}) {
		t.Fatalf("handedOff = %#v, want provider trigger IDs", store.handedOff)
	}
	if got := store.failedCall("unsupported_provider"); len(got) != 0 {
		t.Fatalf("failed unsupported_provider = %#v, want none", got)
	}
}

func TestWebhookTriggerRepositorySelectorSyncsMixedProviders(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC)
	reposDir := t.TempDir()
	store := &stubWebhookTriggerStore{
		claimed: []webhook.StoredTrigger{
			{
				TriggerID: "trigger-github",
				Trigger: webhook.Trigger{
					Provider:           webhook.ProviderGitHub,
					Decision:           webhook.DecisionAccepted,
					RepositoryFullName: "eshu-hq/eshu",
				},
			},
			{
				TriggerID: "trigger-gitlab",
				Trigger: webhook.Trigger{
					Provider:           webhook.ProviderGitLab,
					Decision:           webhook.DecisionAccepted,
					RepositoryFullName: "eshu-hq/eshu",
				},
			},
			{
				TriggerID: "trigger-bitbucket",
				Trigger: webhook.Trigger{
					Provider:           webhook.ProviderBitbucket,
					Decision:           webhook.DecisionAccepted,
					RepositoryFullName: "eshu-hq/worker",
				},
			},
		},
	}
	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:      reposDir,
			SourceMode:    "explicit",
			GitAuthMethod: "token",
			CloneDepth:    1,
		},
		Store: store,
		Owner: "collector-git",
		Now:   func() time.Time { return now },
		SyncGit: func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
			want := []string{"bitbucket/eshu-hq/worker", "eshu-hq/eshu", "gitlab/eshu-hq/eshu"}
			if !reflect.DeepEqual(repositoryIDs, want) {
				t.Fatalf("repositoryIDs = %#v, want %#v", repositoryIDs, want)
			}
			return GitSyncSelection{SelectedRepoPaths: []string{
				filepath.Join(reposDir, "bitbucket", "eshu-hq", "worker"),
				filepath.Join(reposDir, "eshu-hq", "eshu"),
				filepath.Join(reposDir, "gitlab", "eshu-hq", "eshu"),
			}}, nil
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if len(batch.Repositories) != 3 {
		t.Fatalf("len(batch.Repositories) = %d, want 3", len(batch.Repositories))
	}
	gotRemoteURLs := remoteURLsFromSelectedRepositories(batch.Repositories)
	wantRemoteURLs := []string{
		"https://bitbucket.org/eshu-hq/worker.git",
		"https://github.com/eshu-hq/eshu.git",
		"https://gitlab.com/eshu-hq/eshu.git",
	}
	if !reflect.DeepEqual(gotRemoteURLs, wantRemoteURLs) {
		t.Fatalf("remote URLs = %#v, want %#v", gotRemoteURLs, wantRemoteURLs)
	}
	if !reflect.DeepEqual(store.handedOff, []string{"trigger-bitbucket", "trigger-github", "trigger-gitlab"}) {
		t.Fatalf("handedOff = %#v, want all provider triggers", store.handedOff)
	}
	if got := store.failedCall("unsupported_provider"); len(got) != 0 {
		t.Fatalf("failed unsupported_provider = %#v, want none", got)
	}
}

func TestLoadWebhookTriggerHandoffConfig(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "yes",
		"ESHU_WEBHOOK_TRIGGER_HANDOFF_OWNER":   "custom-owner",
		"ESHU_WEBHOOK_TRIGGER_CLAIM_LIMIT":     "25",
	}
	config := LoadWebhookTriggerHandoffConfig("collector-git", func(key string) string {
		return env[key]
	})

	if !config.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if config.Owner != "custom-owner" {
		t.Fatalf("Owner = %q, want custom-owner", config.Owner)
	}
	if config.ClaimLimit != 25 {
		t.Fatalf("ClaimLimit = %d, want 25", config.ClaimLimit)
	}
}

type stubWebhookTriggerStore struct {
	claimed     []webhook.StoredTrigger
	handedOff   []string
	failed      []string
	failedCalls []webhookTriggerFailureCall
}

func remoteURLsFromSelectedRepositories(repositories []SelectedRepository) []string {
	remoteURLs := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		remoteURLs = append(remoteURLs, repository.RemoteURL)
	}
	sort.Strings(remoteURLs)
	return remoteURLs
}

func (s *stubWebhookTriggerStore) ClaimQueuedTriggers(
	context.Context,
	string,
	time.Time,
	int,
) ([]webhook.StoredTrigger, error) {
	return append([]webhook.StoredTrigger(nil), s.claimed...), nil
}

func (s *stubWebhookTriggerStore) MarkTriggersHandedOff(
	_ context.Context,
	triggerIDs []string,
	_ time.Time,
) error {
	s.handedOff = append([]string(nil), triggerIDs...)
	return nil
}

func (s *stubWebhookTriggerStore) MarkTriggersFailed(
	_ context.Context,
	triggerIDs []string,
	_ time.Time,
	failureClass string,
	_ string,
) error {
	s.failed = append([]string(nil), triggerIDs...)
	s.failedCalls = append(s.failedCalls, webhookTriggerFailureCall{
		triggerIDs:   append([]string(nil), triggerIDs...),
		failureClass: failureClass,
	})
	return nil
}

func (s *stubWebhookTriggerStore) failedCall(failureClass string) []string {
	for _, call := range s.failedCalls {
		if call.failureClass == failureClass {
			return append([]string(nil), call.triggerIDs...)
		}
	}
	return nil
}

type webhookTriggerFailureCall struct {
	triggerIDs   []string
	failureClass string
}
