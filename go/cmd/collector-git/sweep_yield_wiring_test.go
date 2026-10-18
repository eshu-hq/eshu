// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildCollectorServiceWiresSweepYield fails when collector-git's
// native sweep cannot yield to a queued webhook trigger (#8123): the
// native selector behind the priority selector probes the same
// trigger store the webhook selector claims from.
func TestBuildCollectorServiceWiresSweepYield(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	env := map[string]string{"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true"}
	service, err := buildCollectorService(postgres.SQLDB{}, func(key string) string { return env[key] }, nil, nil, logger)
	if err != nil {
		t.Fatalf("buildCollectorService() error = %v", err)
	}
	priority := service.Source.(*git.GitSource).Selector.(git.PriorityRepositorySelector)
	webhookSelector := priority.Selectors[0].(git.WebhookTriggerRepositorySelector)
	nativeSelector := priority.Selectors[1].(*git.NativeRepositorySelector)
	if nativeSelector.SweepYieldProbe == nil {
		t.Fatal("native selector SweepYieldProbe = nil, want the trigger store")
	}
	if probe, ok := nativeSelector.SweepYieldProbe.(git.WebhookTriggerStore); !ok || probe != webhookSelector.Store {
		t.Fatalf("SweepYieldProbe = %T, want the webhook selector's store", nativeSelector.SweepYieldProbe)
	}
}
