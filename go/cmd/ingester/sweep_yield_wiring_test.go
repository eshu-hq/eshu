// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildIngesterCollectorServiceWiresSweepYield fails when the
// ingester's native sweep cannot yield to a queued webhook trigger
// (#8123): with scheduled sync and the handoff both on, the native
// selector behind the priority selector probes the same trigger store
// the webhook selector claims from, with the configured bounds.
func TestBuildIngesterCollectorServiceWiresSweepYield(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	build := func(env map[string]string) git.RepositorySelector {
		t.Helper()
		service, err := buildIngesterCollectorService(postgres.SQLDB{}, mapGetenv(env),
			func() (string, error) { return t.TempDir(), nil },
			func() []string { return []string{"PATH=/usr/bin"} }, nil, nil, logger)
		if err != nil {
			t.Fatalf("buildIngesterCollectorService() error = %v", err)
		}
		return service.Source.(*git.GitSource).Selector
	}

	priority := build(map[string]string{
		"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED":         "true",
		"ESHU_WEBHOOK_TRIGGER_SWEEP_MAX_YIELDS":        "3",
		"ESHU_WEBHOOK_TRIGGER_SWEEP_YIELD_CHECK_REPOS": "9",
	}).(git.PriorityRepositorySelector)
	webhookSelector := priority.Selectors[0].(git.WebhookTriggerRepositorySelector)
	nativeSelector := priority.Selectors[1].(*git.NativeRepositorySelector)
	if nativeSelector.SweepYieldProbe == nil {
		t.Fatal("priority native selector SweepYieldProbe = nil, want the trigger store")
	}
	if probe, ok := nativeSelector.SweepYieldProbe.(git.WebhookTriggerStore); !ok || probe != webhookSelector.Store {
		t.Fatalf("SweepYieldProbe = %T, want the webhook selector's store", nativeSelector.SweepYieldProbe)
	}
	if nativeSelector.SweepYieldMaxPerSweep != 3 || nativeSelector.SweepYieldCheckEvery != 9 {
		t.Fatalf("sweep yield bounds = (max %d, check %d), want (3, 9)",
			nativeSelector.SweepYieldMaxPerSweep, nativeSelector.SweepYieldCheckEvery)
	}

	// Without the handoff there is no trigger queue to yield to.
	scheduledOnly := build(map[string]string{}).(*git.NativeRepositorySelector)
	if scheduledOnly.SweepYieldProbe != nil {
		t.Fatalf("scheduled-only SweepYieldProbe = %T, want nil", scheduledOnly.SweepYieldProbe)
	}
}
