// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildCollectorServiceSharesOneDiscoveryFailureCounter pins ruling 3 of
// #8084 for collector-git: the webhook and native selectors hold one shared
// *rest.Failures.
func TestBuildCollectorServiceSharesOneDiscoveryFailureCounter(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	env := map[string]string{"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true"}
	service, err := buildCollectorService(postgres.SQLDB{}, func(key string) string { return env[key] }, nil, nil, logger)
	if err != nil {
		t.Fatalf("buildCollectorService() error = %v", err)
	}
	priority := service.Source.(*git.GitSource).Selector.(git.PriorityRepositorySelector)
	webhookSelector := priority.Selectors[0].(git.WebhookTriggerRepositorySelector)
	native := priority.Selectors[1].(*git.NativeRepositorySelector)
	if native.DiscoveryFailures == nil {
		t.Fatal("native selector has no shared DiscoveryFailures")
	}
	if webhookSelector.DiscoveryFailures != native.DiscoveryFailures {
		t.Fatal("webhook and native selectors hold different failure counters")
	}
}
