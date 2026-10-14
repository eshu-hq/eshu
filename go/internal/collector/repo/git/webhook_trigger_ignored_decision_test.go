// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestWebhookTriggerRepositorySelectorMarksIgnoredDecisionTerminalInOneTick
// is the #7795 regression: a claimed row whose decision is ignored (the
// queued+ignored row the refresh-key upsert leaves behind when an ignored
// redelivery overwrites an accepted trigger) must reach a terminal outcome
// in the same tick — failed with the ignored_decision class — with zero
// SyncGit calls and without depending on the lease reap.
func TestWebhookTriggerRepositorySelectorMarksIgnoredDecisionTerminalInOneTick(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
	syncCalls := 0
	store := &stubWebhookTriggerStore{
		// Mirrors the claim output for a queued+ignored row: the claim
		// already flipped the status to claimed, the decision stayed
		// ignored from the redelivery overwrite.
		claimed: []webhook.StoredTrigger{{
			TriggerID: "trigger-ignored-tag",
			Trigger: webhook.Trigger{
				Provider:             webhook.ProviderGitHub,
				EventKind:            webhook.EventKindPush,
				Decision:             webhook.DecisionIgnored,
				Reason:               webhook.ReasonTagRef,
				RepositoryExternalID: "42",
				RepositoryFullName:   "eshu-hq/eshu",
				DefaultBranch:        "main",
				Ref:                  "refs/tags/v1.11.0",
				TargetSHA:            "4444444444444444444444444444444444444444",
			},
			Status: webhook.TriggerStatusClaimed,
		}},
	}
	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:  store,
		Owner:  "collector-git",
		Now:    func() time.Time { return now },
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			syncCalls++
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
	if syncCalls != 0 {
		t.Fatalf("SyncGit calls = %d, want 0 for an ignored-only batch", syncCalls)
	}
	if got := store.failedCall("ignored_decision"); !reflect.DeepEqual(got, []string{"trigger-ignored-tag"}) {
		t.Fatalf("failed ignored_decision = %#v, want the ignored trigger", got)
	}
	if len(store.handedOff) != 0 {
		t.Fatalf("handedOff = %#v, want none for an ignored-only batch", store.handedOff)
	}
}

// TestWebhookTriggerSelectorHandsOffAcceptedAlongsideIgnored proves the
// #7795 terminal path leaves accepted rows on the current handoff path:
// the accepted trigger syncs and hands off while the ignored trigger in
// the same claim batch fails terminal without reaching SyncGit.
func TestWebhookTriggerSelectorHandsOffAcceptedAlongsideIgnored(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
	var syncedIDs []string
	store := &stubWebhookTriggerStore{
		claimed: []webhook.StoredTrigger{
			{
				TriggerID: "trigger-accepted",
				Trigger: webhook.Trigger{
					Provider:             webhook.ProviderGitHub,
					Decision:             webhook.DecisionAccepted,
					RepositoryExternalID: "42",
					RepositoryFullName:   "eshu-hq/eshu",
					DefaultBranch:        "main",
					TargetSHA:            "2222222222222222222222222222222222222222",
				},
				Status: webhook.TriggerStatusClaimed,
			},
			{
				TriggerID: "trigger-ignored-branch",
				Trigger: webhook.Trigger{
					Provider:             webhook.ProviderGitHub,
					Decision:             webhook.DecisionIgnored,
					Reason:               webhook.ReasonNonDefaultBranch,
					RepositoryExternalID: "42",
					RepositoryFullName:   "eshu-hq/eshu",
					DefaultBranch:        "main",
					Ref:                  "refs/heads/feature",
					TargetSHA:            "5555555555555555555555555555555555555555",
				},
				Status: webhook.TriggerStatusClaimed,
			},
		},
	}
	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:  store,
		Owner:  "collector-git",
		Now:    func() time.Time { return now },
		SyncGit: func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
			syncedIDs = append([]string(nil), repositoryIDs...)
			return GitSyncSelection{SelectedRepoPaths: []string{t.TempDir()}}, nil
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(syncedIDs, []string{"eshu-hq/eshu"}) {
		t.Fatalf("synced repositoryIDs = %#v, want only the accepted trigger's repo", syncedIDs)
	}
	if !reflect.DeepEqual(store.handedOff, []string{"trigger-accepted"}) {
		t.Fatalf("handedOff = %#v, want only the accepted trigger", store.handedOff)
	}
	if got := store.failedCall("ignored_decision"); !reflect.DeepEqual(got, []string{"trigger-ignored-branch"}) {
		t.Fatalf("failed ignored_decision = %#v, want only the ignored trigger", got)
	}
}

// TestWebhookTriggerSelectorEmitsIgnoredDecisionCounter proves the #7795
// operator signal: the same-tick ignored terminal increments a dedicated
// counter, and the tick emits no claim_lease_exhausted-adjacent reap
// outcome for it.
func TestWebhookTriggerSelectorEmitsIgnoredDecisionCounter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
	store := &stubWebhookTriggerStore{
		claimed: []webhook.StoredTrigger{{
			TriggerID: "trigger-ignored-tag",
			Trigger: webhook.Trigger{
				Provider:           webhook.ProviderGitHub,
				Decision:           webhook.DecisionIgnored,
				Reason:             webhook.ReasonTagRef,
				RepositoryFullName: "eshu-hq/eshu",
			},
			Status: webhook.TriggerStatusClaimed,
		}},
	}
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	selector := WebhookTriggerRepositorySelector{
		Config:      RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:       store,
		Owner:       "collector-git",
		Instruments: inst,
		Now:         func() time.Time { return now },
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			t.Fatal("SyncGit called, want no call for an ignored-only batch")
			return GitSyncSelection{}, nil
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var ignoredTotal int64
	var ignoredSeen bool
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_webhook_trigger_ignored_decisions_total":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("ignored decisions data = %T, want Sum[int64]", m.Data)
				}
				for _, point := range sum.DataPoints {
					ignoredTotal += point.Value
				}
				ignoredSeen = true
			case "eshu_dp_webhook_trigger_claim_reaps_total":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("reaps data = %T, want Sum[int64]", m.Data)
				}
				for _, point := range sum.DataPoints {
					outcome, ok := point.Attributes.Value("outcome")
					if !ok {
						t.Fatal("reaps point has no outcome attribute")
					}
					if outcome.AsString() == "exhausted" {
						t.Fatalf("reaps outcome exhausted = %d, want no lease-exhaust signal for an ignored terminal", point.Value)
					}
				}
			}
		}
	}
	if !ignoredSeen {
		t.Fatal("eshu_dp_webhook_trigger_ignored_decisions_total not recorded, want the ignored terminal counter")
	}
	if ignoredTotal != 1 {
		t.Fatalf("ignored decisions total = %d, want 1", ignoredTotal)
	}
}

// TestWebhookTriggerSelectorHandsOffFlipToAcceptedDuringTick is the #7795
// P1 interleave regression: an accepted redelivery for the same refresh key
// landing between ClaimQueuedTriggers and the same-tick ignored-decision
// fail flips the row back to accepted under the live claim (the refresh-key
// upsert overwrites the decision without bumping the fencing token). The
// decision-guarded UPDATE must skip that row, and the tick must keep it on
// the handoff path — synced and handed off — instead of failing an accepted
// refresh with ignored_decision. The stub's ignoredDecisionSkipped set
// stands in for the guarded statement skipping a concurrently flipped row.
func TestWebhookTriggerSelectorHandsOffFlipToAcceptedDuringTick(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
	var syncedIDs []string
	store := &stubWebhookTriggerStore{
		claimed: []webhook.StoredTrigger{
			{
				// Claim snapshot says ignored, but an accepted redelivery
				// flips it before the fail runs.
				TriggerID: "trigger-flip",
				Trigger: webhook.Trigger{
					Provider:             webhook.ProviderGitHub,
					Decision:             webhook.DecisionIgnored,
					Reason:               webhook.ReasonTagRef,
					RepositoryExternalID: "43",
					RepositoryFullName:   "eshu-hq/flipped",
					DefaultBranch:        "main",
					Ref:                  "refs/tags/v1.11.0",
					TargetSHA:            "6666666666666666666666666666666666666666",
				},
				Status: webhook.TriggerStatusClaimed,
			},
			{
				TriggerID: "trigger-ignored-stays",
				Trigger: webhook.Trigger{
					Provider:             webhook.ProviderGitHub,
					Decision:             webhook.DecisionIgnored,
					Reason:               webhook.ReasonNonDefaultBranch,
					RepositoryExternalID: "42",
					RepositoryFullName:   "eshu-hq/eshu",
					DefaultBranch:        "main",
					Ref:                  "refs/heads/feature",
					TargetSHA:            "5555555555555555555555555555555555555555",
				},
				Status: webhook.TriggerStatusClaimed,
			},
		},
		ignoredDecisionSkipped: map[string]bool{"trigger-flip": true},
	}
	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:  store,
		Owner:  "collector-git",
		Now:    func() time.Time { return now },
		SyncGit: func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
			syncedIDs = append([]string(nil), repositoryIDs...)
			return GitSyncSelection{SelectedRepoPaths: []string{t.TempDir()}}, nil
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(syncedIDs, []string{"eshu-hq/flipped"}) {
		t.Fatalf("synced repositoryIDs = %#v, want only the flipped trigger's repo", syncedIDs)
	}
	if !reflect.DeepEqual(store.handedOff, []string{"trigger-flip"}) {
		t.Fatalf("handedOff = %#v, want the flipped trigger back on the handoff path", store.handedOff)
	}
	if got := store.failedCall("ignored_decision"); !reflect.DeepEqual(got, []string{"trigger-ignored-stays"}) {
		t.Fatalf("failed ignored_decision = %#v, want only the still-ignored trigger", got)
	}
}

// TestWebhookTriggerSelectorCountsOnlyActuallyFailedIgnoredDecisions is the
// #7795 P2 regression: the ignored-decisions counter must equal the rows
// the guarded statement actually terminated, not the rows the claim
// snapshot marked for termination. A concurrently flipped row stays on the
// handoff path and must not be counted.
func TestWebhookTriggerSelectorCountsOnlyActuallyFailedIgnoredDecisions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
	store := &stubWebhookTriggerStore{
		claimed: []webhook.StoredTrigger{
			{
				TriggerID: "trigger-flip",
				Trigger: webhook.Trigger{
					Provider:             webhook.ProviderGitHub,
					Decision:             webhook.DecisionIgnored,
					Reason:               webhook.ReasonTagRef,
					RepositoryExternalID: "43",
					RepositoryFullName:   "eshu-hq/flipped",
					DefaultBranch:        "main",
					Ref:                  "refs/tags/v1.11.0",
					TargetSHA:            "6666666666666666666666666666666666666666",
				},
				Status: webhook.TriggerStatusClaimed,
			},
			{
				TriggerID: "trigger-ignored-stays",
				Trigger: webhook.Trigger{
					Provider:             webhook.ProviderGitHub,
					Decision:             webhook.DecisionIgnored,
					Reason:               webhook.ReasonNonDefaultBranch,
					RepositoryExternalID: "42",
					RepositoryFullName:   "eshu-hq/eshu",
					DefaultBranch:        "main",
					Ref:                  "refs/heads/feature",
					TargetSHA:            "5555555555555555555555555555555555555555",
				},
				Status: webhook.TriggerStatusClaimed,
			},
		},
		ignoredDecisionSkipped: map[string]bool{"trigger-flip": true},
	}
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	selector := WebhookTriggerRepositorySelector{
		Config:      RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:       store,
		Owner:       "collector-git",
		Instruments: inst,
		Now:         func() time.Time { return now },
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			return GitSyncSelection{SelectedRepoPaths: []string{t.TempDir()}}, nil
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var ignoredTotal int64
	var ignoredSeen bool
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_webhook_trigger_ignored_decisions_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("ignored decisions data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				ignoredTotal += point.Value
			}
			ignoredSeen = true
		}
	}
	if !ignoredSeen {
		t.Fatal("eshu_dp_webhook_trigger_ignored_decisions_total not recorded, want the ignored terminal counter")
	}
	if ignoredTotal != 1 {
		t.Fatalf("ignored decisions total = %d, want 1 (only the row actually failed)", ignoredTotal)
	}
}
