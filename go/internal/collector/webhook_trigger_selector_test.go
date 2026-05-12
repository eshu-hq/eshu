package collector

import (
	"context"
	"errors"
	"reflect"
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
	if store.failureClass != "sync_git_failed" {
		t.Fatalf("failureClass = %q, want sync_git_failed", store.failureClass)
	}
}

type stubWebhookTriggerStore struct {
	claimed      []webhook.StoredTrigger
	handedOff    []string
	failed       []string
	failureClass string
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
	s.failureClass = failureClass
	return nil
}
