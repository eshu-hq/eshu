// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"go.opentelemetry.io/otel/metric"
)

const defaultWebhookTriggerClaimLimit = 100

// defaultWebhookTriggerClaimLeaseWindow bounds how long a claim may sit
// without a handoff before the next tick reaps it (#7661). It must cover
// a slow full-batch git sync (100 repositories) with headroom: a live
// holder past the window only wastes one duplicate sync — the fencing
// token keeps the handoff correct — while a dead holder's rows wait at
// most one tick plus this window.
const defaultWebhookTriggerClaimLeaseWindow = 15 * time.Minute

// defaultWebhookTriggerMaxClaimAttempts caps claim attempts per row: the
// first claim plus two lease recoveries. A row still failing then is
// poison and lands in failed with the claim_lease_exhausted reason
// instead of looping through the lease forever (#7661).
const defaultWebhookTriggerMaxClaimAttempts = 3

const (
	webhookTriggerFailureNoRepositoryID      = "no_repository_id"
	webhookTriggerFailureSyncGit             = "sync_git_failed"
	webhookTriggerFailureUnsupportedProvider = "unsupported_provider"
)

const (
	// webhookTriggerClaimReapOutcomeRequeued labels reaped claims that
	// returned to queued for another attempt (#7661).
	webhookTriggerClaimReapOutcomeRequeued = "requeued"
	// webhookTriggerClaimReapOutcomeExhausted labels reaped claims past
	// the attempt cap that failed with claim_lease_exhausted (#7661).
	webhookTriggerClaimReapOutcomeExhausted = "exhausted"
)

// WebhookTriggerStore is the durable trigger surface needed by the Git
// collector compatibility selector.
type WebhookTriggerStore interface {
	ClaimQueuedTriggers(context.Context, string, time.Time, int) ([]webhook.StoredTrigger, error)
	MarkTriggersHandedOff(context.Context, []webhook.StoredTrigger, time.Time) error
	MarkTriggersFailed(context.Context, []webhook.StoredTrigger, time.Time, string, string) error
	ReapExpiredTriggerClaims(context.Context, time.Time, int, int, time.Time) ([]webhook.StoredTrigger, []webhook.StoredTrigger, error)
	CountStaleClaims(context.Context, time.Time) (int64, error)
}

// WebhookTriggerRepositorySelector converts queued webhook triggers into a
// targeted Git repository selection batch.
type WebhookTriggerRepositorySelector struct {
	Config     RepoSyncConfig
	Store      WebhookTriggerStore
	Owner      string
	ClaimLimit int
	// ClaimLeaseWindow bounds how long a claim may sit without a handoff
	// before the next tick reaps it back to queued (or to failed past
	// MaxClaimAttempts). Zero takes
	// defaultWebhookTriggerClaimLeaseWindow (#7661).
	ClaimLeaseWindow time.Duration
	// MaxClaimAttempts caps claim attempts per row before a reap fails
	// it with the claim_lease_exhausted reason. Zero takes
	// defaultWebhookTriggerMaxClaimAttempts (#7661).
	MaxClaimAttempts int
	Now              func() time.Time
	SyncGit          func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error)
	Logger           *slog.Logger
	// BaselineResolver baselines git delta syncs on the last projected commit
	// per scope instead of the local HEAD (epic #2340). Nil takes a full
	// snapshot on every update.
	BaselineResolver DeltaBaselineResolver
	// Instruments records the delta-baseline fallback rate. Optional.
	Instruments *telemetry.Instruments
	// ReindexWatermark reads the fleet reindex watermark once per cycle that
	// claimed triggers (#7620). It reaches only the triggered repositories;
	// nil disables reindex requests for this selector.
	ReindexWatermark ReindexWatermarkReader
	// RepositoryReindexWatermark reads the per-repository reindex watermarks
	// once per cycle that claimed triggers (#7620). Like ReindexWatermark it
	// reaches only the triggered repositories; nil disables them.
	RepositoryReindexWatermark RepositoryReindexWatermarkReader
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

	// Reap-first: return expired claims from stalled ticks (including this
	// worker's own crash) to queued before claiming, so a tick never loses
	// work its claim loop never reached (#7661).
	leaseCutoff := observedAt.Add(-s.claimLeaseWindow())
	requeued, exhausted, err := s.Store.ReapExpiredTriggerClaims(ctx, leaseCutoff, s.maxClaimAttempts(), limit, observedAt)
	if err != nil {
		return SelectionBatch{}, fmt.Errorf("reap expired webhook trigger claims: %w", err)
	}
	if n := len(requeued) + len(exhausted); n > 0 {
		s.logger().Info("reaped expired webhook trigger claims",
			"requeued", len(requeued), "exhausted", len(exhausted))
	}
	s.recordClaimReapOutcome(ctx, leaseCutoff, len(requeued), len(exhausted))

	triggers, err := s.Store.ClaimQueuedTriggers(ctx, owner, observedAt, limit)
	if err != nil {
		return SelectionBatch{}, fmt.Errorf("claim webhook triggers: %w", err)
	}
	if len(triggers) == 0 {
		return SelectionBatch{ObservedAt: observedAt}, nil
	}

	syncableTriggers, unsupportedTriggers := supportedWebhookRefreshTriggers(triggers)
	if len(unsupportedTriggers) > 0 {
		if err := s.Store.MarkTriggersFailed(ctx, unsupportedTriggers, observedAt, webhookTriggerFailureUnsupportedProvider, "webhook provider is not supported by the git collector handoff"); err != nil {
			return SelectionBatch{}, fmt.Errorf("mark unsupported webhook triggers failed: %w", err)
		}
	}

	repositoryIDs := repositoryIDsFromWebhookTriggers(syncableTriggers)
	if len(repositoryIDs) == 0 {
		if len(syncableTriggers) > 0 {
			if err := s.Store.MarkTriggersFailed(ctx, syncableTriggers, observedAt, webhookTriggerFailureNoRepositoryID, "accepted webhook triggers did not resolve to repository ids"); err != nil {
				return SelectionBatch{}, fmt.Errorf("mark webhook triggers failed: %w", err)
			}
		}
		return SelectionBatch{ObservedAt: observedAt}, nil
	}

	syncGitFn := s.SyncGit
	if syncGitFn == nil {
		reindexRequestedAt := resolveReindexWatermark(ctx, s.ReindexWatermark, observedAt, s.Config, s.Logger)
		repositoryReindexRequestedAt := resolveRepositoryReindexWatermarks(ctx, s.RepositoryReindexWatermark, reindexRequestedAt, observedAt, s.Config, s.Logger)
		syncGitFn = func(ctx context.Context, config RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
			return syncGitRepositoriesWithLogger(ctx, config, repositoryIDs, s.Logger, gitDeltaBaseline{
				Resolver:                     s.BaselineResolver,
				Instruments:                  s.Instruments,
				Reconcile:                    reconcilePolicyFromConfig(config),
				ReindexRequestedAt:           reindexRequestedAt,
				RepositoryReindexRequestedAt: repositoryReindexRequestedAt,
				Now:                          s.Now,
			})
		}
	}
	synced, err := syncGitFn(ctx, s.Config, repositoryIDs)
	if err != nil {
		if len(syncableTriggers) > 0 {
			markErr := s.Store.MarkTriggersFailed(ctx, syncableTriggers, observedAt, webhookTriggerFailureSyncGit, err.Error())
			if markErr != nil {
				return SelectionBatch{}, errors.Join(
					fmt.Errorf("sync webhook-triggered repositories: %w", err),
					fmt.Errorf("mark webhook triggers failed: %w", markErr),
				)
			}
		}
		return SelectionBatch{}, fmt.Errorf("sync webhook-triggered repositories: %w", err)
	}

	if len(syncableTriggers) > 0 {
		if err := s.Store.MarkTriggersHandedOff(ctx, syncableTriggers, observedAt); err != nil {
			return SelectionBatch{}, fmt.Errorf("mark webhook triggers handed off: %w", err)
		}
	}

	return SelectionBatch{
		ObservedAt: observedAt,
		Repositories: buildSelectedRepositories(
			s.Config,
			synced.SelectedRepoPaths,
			synced.DeltaByRepoPath,
			synced.ReconcileByRepoPath,
			synced.SourceCommitSHAByRepoPath,
			synced.RefsByRepoPath,
			synced.RefWorktreesByRepoPath,
		),
	}, nil
}

func (s WebhookTriggerRepositorySelector) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// claimLeaseWindow returns the configured stale-claim lease, or the default
// when unset. A non-positive value keeps the default: a zero or negative
// window would reap live claims from the in-flight sync (#7661).
func (s WebhookTriggerRepositorySelector) claimLeaseWindow() time.Duration {
	if s.ClaimLeaseWindow > 0 {
		return s.ClaimLeaseWindow
	}
	return defaultWebhookTriggerClaimLeaseWindow
}

// maxClaimAttempts returns the configured claim-attempt cap, or the default
// when unset. A value below 1 keeps the default so a misconfigured selector
// cannot fail every row on its first reap (#7661).
func (s WebhookTriggerRepositorySelector) maxClaimAttempts() int {
	if s.MaxClaimAttempts > 0 {
		return s.MaxClaimAttempts
	}
	return defaultWebhookTriggerMaxClaimAttempts
}

func (s WebhookTriggerRepositorySelector) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// recordClaimReapOutcome emits the lease-recovery signals for one tick: the
// reaped-row counter by outcome and the residual stuck-claim gauge (#7661).
// It is a no-op without instruments, and a gauge-query failure only logs:
// metric collection must never fail selection.
func (s WebhookTriggerRepositorySelector) recordClaimReapOutcome(ctx context.Context, leaseCutoff time.Time, requeued, exhausted int) {
	if s.Instruments == nil {
		return
	}
	if requeued > 0 {
		s.Instruments.WebhookTriggerClaimReaps.Add(ctx, int64(requeued),
			metric.WithAttributes(telemetry.AttrOutcome(webhookTriggerClaimReapOutcomeRequeued)))
	}
	if exhausted > 0 {
		s.Instruments.WebhookTriggerClaimReaps.Add(ctx, int64(exhausted),
			metric.WithAttributes(telemetry.AttrOutcome(webhookTriggerClaimReapOutcomeExhausted)))
	}
	stuck, err := s.Store.CountStaleClaims(ctx, leaseCutoff)
	if err != nil {
		s.logger().Warn("count stale webhook trigger claims for gauge",
			"error", err)
		return
	}
	s.Instruments.WebhookTriggerStaleClaims.Record(ctx, stuck)
}

func supportedWebhookRefreshTriggers(triggers []webhook.StoredTrigger) ([]webhook.StoredTrigger, []webhook.StoredTrigger) {
	syncable := make([]webhook.StoredTrigger, 0, len(triggers))
	unsupported := make([]webhook.StoredTrigger, 0)
	for _, trigger := range triggers {
		if trigger.Decision != webhook.DecisionAccepted {
			continue
		}
		switch trigger.Provider {
		case webhook.ProviderGitHub, webhook.ProviderGitLab, webhook.ProviderBitbucket:
			syncable = append(syncable, trigger)
		default:
			unsupported = append(unsupported, trigger)
		}
	}
	return syncable, unsupported
}

func repositoryIDsFromWebhookTriggers(triggers []webhook.StoredTrigger) []string {
	seen := make(map[string]struct{}, len(triggers))
	repositoryIDs := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		if trigger.Decision != webhook.DecisionAccepted {
			continue
		}
		repositoryID := repositoryIDFromWebhookTrigger(trigger)
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

func repositoryIDFromWebhookTrigger(trigger webhook.StoredTrigger) string {
	repositoryID := normalizeRepositoryID(trigger.RepositoryFullName)
	if repositoryID == "" {
		return ""
	}
	switch trigger.Provider {
	case webhook.ProviderGitHub:
		return repositoryID
	case webhook.ProviderGitLab, webhook.ProviderBitbucket:
		return string(trigger.Provider) + "/" + repositoryID
	default:
		return ""
	}
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
