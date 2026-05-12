package collector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

const defaultWebhookTriggerClaimLimit = 100

// WebhookTriggerStore is the durable trigger surface needed by the Git
// collector compatibility selector.
type WebhookTriggerStore interface {
	ClaimQueuedTriggers(context.Context, string, time.Time, int) ([]webhook.StoredTrigger, error)
	MarkTriggersHandedOff(context.Context, []string, time.Time) error
	MarkTriggersFailed(context.Context, []string, time.Time, string, string) error
}

// WebhookTriggerRepositorySelector converts queued webhook triggers into a
// targeted Git repository selection batch.
type WebhookTriggerRepositorySelector struct {
	Config     RepoSyncConfig
	Store      WebhookTriggerStore
	Owner      string
	ClaimLimit int
	Now        func() time.Time
	SyncGit    func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error)
}

// SelectRepositories claims queued webhook triggers, syncs only the referenced
// repositories, and returns the changed repositories through the normal Git
// collector snapshot path.
func (s WebhookTriggerRepositorySelector) SelectRepositories(ctx context.Context) (SelectionBatch, error) {
	if s.Store == nil {
		return SelectionBatch{}, fmt.Errorf("webhook trigger store is required")
	}
	owner := strings.TrimSpace(s.Owner)
	if owner == "" {
		return SelectionBatch{}, fmt.Errorf("webhook trigger selector owner is required")
	}
	observedAt := s.now().UTC()
	limit := s.ClaimLimit
	if limit <= 0 {
		limit = defaultWebhookTriggerClaimLimit
	}

	triggers, err := s.Store.ClaimQueuedTriggers(ctx, owner, observedAt, limit)
	if err != nil {
		return SelectionBatch{}, fmt.Errorf("claim webhook triggers: %w", err)
	}
	if len(triggers) == 0 {
		return SelectionBatch{ObservedAt: observedAt}, nil
	}

	repositoryIDs := repositoryIDsFromWebhookTriggers(triggers)
	triggerIDs := triggerIDsFromWebhookTriggers(triggers)
	if len(repositoryIDs) == 0 {
		if len(triggerIDs) > 0 {
			if err := s.Store.MarkTriggersFailed(ctx, triggerIDs, observedAt, "no_repository_id", "accepted webhook triggers did not resolve to repository ids"); err != nil {
				return SelectionBatch{}, fmt.Errorf("mark webhook triggers failed: %w", err)
			}
		}
		return SelectionBatch{ObservedAt: observedAt}, nil
	}

	syncGitFn := s.SyncGit
	if syncGitFn == nil {
		syncGitFn = syncGitRepositories
	}
	synced, err := syncGitFn(ctx, s.Config, repositoryIDs)
	if err != nil {
		if len(triggerIDs) > 0 {
			markErr := s.Store.MarkTriggersFailed(ctx, triggerIDs, observedAt, "sync_git_failed", err.Error())
			if markErr != nil {
				return SelectionBatch{}, errors.Join(
					fmt.Errorf("sync webhook-triggered repositories: %w", err),
					fmt.Errorf("mark webhook triggers failed: %w", markErr),
				)
			}
		}
		return SelectionBatch{}, fmt.Errorf("sync webhook-triggered repositories: %w", err)
	}

	if len(triggerIDs) > 0 {
		if err := s.Store.MarkTriggersHandedOff(ctx, triggerIDs, observedAt); err != nil {
			return SelectionBatch{}, fmt.Errorf("mark webhook triggers handed off: %w", err)
		}
	}

	return SelectionBatch{
		ObservedAt:   observedAt,
		Repositories: buildSelectedRepositories(s.Config, synced.SelectedRepoPaths),
	}, nil
}

func (s WebhookTriggerRepositorySelector) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func repositoryIDsFromWebhookTriggers(triggers []webhook.StoredTrigger) []string {
	seen := make(map[string]struct{}, len(triggers))
	repositoryIDs := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		if trigger.Decision != webhook.DecisionAccepted {
			continue
		}
		repositoryID := normalizeRepositoryID(trigger.RepositoryFullName)
		if repositoryID == "" {
			continue
		}
		if _, ok := seen[repositoryID]; ok {
			continue
		}
		seen[repositoryID] = struct{}{}
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	sort.Strings(repositoryIDs)
	return repositoryIDs
}

func triggerIDsFromWebhookTriggers(triggers []webhook.StoredTrigger) []string {
	ids := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		id := strings.TrimSpace(trigger.TriggerID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
