// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildIngesterCollectorServiceSharesOneDiscoveryFailureCounter pins
// ruling 3 of #8084: the webhook and native selectors of one process hold the
// same *rest.Failures, in the priority wiring and in webhook-only wiring.
// Without it the webhook sync path would skip with no cap.
func TestBuildIngesterCollectorServiceSharesOneDiscoveryFailureCounter(t *testing.T) {
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

	priority := build(map[string]string{"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true"}).(git.PriorityRepositorySelector)
	webhookSelector := priority.Selectors[0].(git.WebhookTriggerRepositorySelector)
	native := priority.Selectors[1].(*git.NativeRepositorySelector)
	if native.DiscoveryFailures == nil {
		t.Fatal("native selector has no shared DiscoveryFailures")
	}
	if webhookSelector.DiscoveryFailures != native.DiscoveryFailures {
		t.Fatal("priority wiring: webhook and native selectors hold different failure counters")
	}

	only := build(map[string]string{
		"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true",
		"ESHU_REPO_SCHEDULED_SYNC_ENABLED":     "false",
	}).(git.WebhookTriggerRepositorySelector)
	if only.DiscoveryFailures == nil {
		t.Fatal("webhook-only wiring: the webhook selector has no DiscoveryFailures, so its sync failures could not be capped")
	}
}
