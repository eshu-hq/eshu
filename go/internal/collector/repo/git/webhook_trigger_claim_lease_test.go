// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestWebhookTriggerRepositorySelectorReapsExpiredClaimsBeforeClaiming(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC)
	claimed := []webhook.StoredTrigger{
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
	}
	newSelector := func(store *stubWebhookTriggerStore) WebhookTriggerRepositorySelector {
		return WebhookTriggerRepositorySelector{
			Config:     RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
			Store:      store,
			Owner:      "collector-git",
			ClaimLimit: 10,
			Now:        func() time.Time { return now },
			SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
				return GitSyncSelection{SelectedRepoPaths: []string{t.TempDir()}}, nil
			},
		}
	}

	t.Run("reaps once per tick", func(t *testing.T) {
		t.Parallel()
		store := &stubWebhookTriggerStore{claimed: claimed}
		if _, err := newSelector(store).SelectRepositories(context.Background()); err != nil {
			t.Fatalf("SelectRepositories() error = %v, want nil", err)
		}
		if store.reapCalls != 1 {
			t.Fatalf("reapCalls = %d, want 1", store.reapCalls)
		}
	})

	t.Run("reap error aborts before claim", func(t *testing.T) {
		t.Parallel()
		store := &stubWebhookTriggerStore{claimed: claimed, reapErr: errors.New("reap boom")}
		if _, err := newSelector(store).SelectRepositories(context.Background()); err == nil {
			t.Fatal("SelectRepositories() error = nil, want reap error")
		}
		if len(store.handedOff) != 0 || len(store.failed) != 0 {
			t.Fatalf("handedOff/failed = %#v/%#v, want no handoff after reap failure", store.handedOff, store.failed)
		}
	})

	t.Run("gauge error does not fail selection", func(t *testing.T) {
		t.Parallel()
		store := &stubWebhookTriggerStore{claimed: claimed, staleCountErr: errors.New("count boom")}
		selector := newSelector(store)
		reader := sdkmetric.NewManualReader()
		inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
		if err != nil {
			t.Fatalf("NewInstruments() error = %v", err)
		}
		selector.Instruments = inst
		if _, err := selector.SelectRepositories(context.Background()); err != nil {
			t.Fatalf("SelectRepositories() error = %v, want nil despite gauge failure", err)
		}
		if !reflect.DeepEqual(store.handedOff, []string{"trigger-1"}) {
			t.Fatalf("handedOff = %#v, want the claimed trigger", store.handedOff)
		}
	})
}

func TestWebhookTriggerRepositorySelectorRecordsClaimReapMetrics(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC)
	store := &stubWebhookTriggerStore{
		requeued:   []webhook.StoredTrigger{{TriggerID: "r1"}, {TriggerID: "r2"}},
		exhausted:  []webhook.StoredTrigger{{TriggerID: "e1"}},
		staleCount: 4,
	}
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	selector := WebhookTriggerRepositorySelector{
		Config:      RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:       store,
		Owner:       "collector-git",
		Instruments: inst,
		Now:         func() time.Time { return now },
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			t.Fatal("SyncGit called, want no call without claimed triggers")
			return GitSyncSelection{}, nil
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	wantCounts := map[string]int64{"requeued": 2, "exhausted": 1}
	var gaugeSeen bool
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
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
					want, ok := wantCounts[outcome.AsString()]
					if !ok {
						t.Fatalf("reaps outcome = %q, want requeued or exhausted", outcome.AsString())
					}
					if point.Value != want {
						t.Fatalf("reaps outcome %q = %d, want %d", outcome.AsString(), point.Value, want)
					}
					delete(wantCounts, outcome.AsString())
				}
			case "eshu_dp_webhook_trigger_stale_claims":
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok || len(gauge.DataPoints) != 1 {
					t.Fatalf("stale claims data = %#v, want one int64 point", m.Data)
				}
				if got := gauge.DataPoints[0].Value; got != 4 {
					t.Fatalf("stale claims = %d, want 4", got)
				}
				gaugeSeen = true
			}
		}
	}
	if len(wantCounts) != 0 {
		t.Fatalf("missing reaps outcomes: %v", wantCounts)
	}
	if !gaugeSeen {
		t.Fatal("stale claims gauge not recorded")
	}
}
