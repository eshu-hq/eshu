// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Pinned Preparse-bearing selector counts for the ingester (#8038). Each
// counts the selector structs with a Preparse field that preparseWiringGaps
// reaches in the built selector tree: the native scheduled selector, the
// webhook trigger selector, or both. A selector the walk misses, or a new one
// added without updating the pin, fails the guard.
const (
	ingesterScheduledOnlyPreparseSelectors       = 1
	ingesterScheduledAndWebhookPreparseSelectors = 2
	ingesterWebhookOnlyPreparseSelectors         = 1
)

// TestIngesterSelectorsCarryPreparse (ruling a9-143.3, review F-1): the
// ingester is the runtime that syncs repositories in a deployment, so every
// selector it builds that can force a reconcile must carry the pre-parse
// configuration, or the skipped parse and the identity stamp never run.
func TestIngesterSelectorsCarryPreparse(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		env       map[string]string
		wantFound int
	}{
		"scheduled only":        {env: map[string]string{}, wantFound: ingesterScheduledOnlyPreparseSelectors},
		"scheduled and webhook": {env: map[string]string{"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true"}, wantFound: ingesterScheduledAndWebhookPreparseSelectors},
		"webhook only":          {env: map[string]string{"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true", "ESHU_REPO_SCHEDULED_SYNC_ENABLED": "false"}, wantFound: ingesterWebhookOnlyPreparseSelectors},
	} {
		env := tc.env
		service, err := buildIngesterCollectorService(postgres.SQLDB{}, mapGetenv(env),
			func() (string, error) { return t.TempDir(), nil },
			func() []string { return []string{"PATH=/usr/bin"} }, nil, nil, nil)
		if err != nil {
			t.Fatalf("%s: buildIngesterCollectorService() error = %v", name, err)
		}
		problems := checkPreparseWiring(service.Source.(*git.GitSource).Selector, tc.wantFound)
		if len(problems) > 0 {
			t.Errorf("%s: preparse wiring problems %v", name, problems)
		}
	}
}
