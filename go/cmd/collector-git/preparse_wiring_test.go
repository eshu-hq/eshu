// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Pinned Preparse-bearing selector counts for collector-git (#8038). Each
// counts the selector structs with a Preparse field that preparseWiringGaps
// reaches in the built selector tree: the native scheduled selector, plus the
// webhook trigger selector when the handoff is on. A selector the walk misses,
// or a new one added without updating the pin, fails the guard.
const (
	collectorGitScheduledOnlyPreparseSelectors       = 1
	collectorGitScheduledAndWebhookPreparseSelectors = 2
)

// TestCollectorGitSelectorsCarryPreparse (ruling a9-143.3): every selector
// collector-git builds that can force a reconcile carries the pre-parse
// configuration, with and without the webhook handoff.
func TestCollectorGitSelectorsCarryPreparse(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		env       map[string]string
		wantFound int
	}{
		"scheduled only":        {env: map[string]string{}, wantFound: collectorGitScheduledOnlyPreparseSelectors},
		"scheduled and webhook": {env: map[string]string{"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true"}, wantFound: collectorGitScheduledAndWebhookPreparseSelectors},
	} {
		env := tc.env
		service, err := buildCollectorService(postgres.SQLDB{}, func(key string) string { return env[key] }, nil, nil, nil)
		if err != nil {
			t.Fatalf("%s: buildCollectorService() error = %v", name, err)
		}
		problems := checkPreparseWiring(service.Source.(*git.GitSource).Selector, tc.wantFound)
		if len(problems) > 0 {
			t.Errorf("%s: preparse wiring problems %v", name, problems)
		}
	}
}
