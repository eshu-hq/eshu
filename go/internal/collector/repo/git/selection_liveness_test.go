// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
)

func TestLoadRepoSyncConfigDefaultsTheSelectionLivenessWindow(t *testing.T) {
	t.Parallel()

	config, err := LoadRepoSyncConfig("collector-git", repoSyncTestGetenv(nil))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig() error = %v, want nil", err)
	}
	if config.SelectionLivenessWindow != 48*time.Hour {
		t.Fatalf("SelectionLivenessWindow = %v, want the 48h default", config.SelectionLivenessWindow)
	}
}

func TestLoadRepoSyncConfigRejectsAnInvalidSelectionLivenessWindow(t *testing.T) {
	t.Parallel()

	const token = "ghp_livenesswindowtokenvalue0123"
	for _, raw := range []string{"59m", "garbage"} {
		_, err := LoadRepoSyncConfig("collector-git", repoSyncTestGetenv(map[string]string{
			membership.LivenessWindowEnv: raw,
			"ESHU_GIT_TOKEN":             token,
		}))
		if err == nil {
			t.Fatalf("%s=%q loaded, want a config error", membership.LivenessWindowEnv, raw)
		}
		if !strings.Contains(err.Error(), membership.LivenessWindowEnv) || strings.Contains(err.Error(), token) {
			t.Fatalf("%s=%q error = %v, want it to name the variable and never the token", membership.LivenessWindowEnv, raw, err)
		}
	}
}

// recordingBatchStore is a membership.Store that knows the githubOrg test
// fixture's scopes and records every batch it is asked to write.
type recordingBatchStore struct {
	known   []membership.KnownScope
	batches []membership.Batch
}

func (s *recordingBatchStore) KnownScopes(context.Context, string, string) ([]membership.KnownScope, error) {
	return s.known, nil
}

func (s *recordingBatchStore) Observations(context.Context, string) ([]membership.Observation, error) {
	return nil, nil
}

func (s *recordingBatchStore) UpsertObservations(_ context.Context, batch membership.Batch) error {
	s.batches = append(s.batches, batch)
	return nil
}

// TestConfiguredSelectionLivenessWindowIsWrittenOnEveryRow loads a 72h
// window from the environment and proves the batch the observer writes, and
// so liveness_window_seconds on every stored row, carries it.
func TestConfiguredSelectionLivenessWindowIsWrittenOnEveryRow(t *testing.T) {
	t.Parallel()

	loaded, err := LoadRepoSyncConfig("collector-git", repoSyncTestGetenv(map[string]string{
		membership.LivenessWindowEnv: "72h",
	}))
	if err != nil {
		t.Fatalf("LoadRepoSyncConfig() error = %v, want nil", err)
	}
	store := &recordingBatchStore{}
	selector := githubOrgObservationSelector(t, 1, 0, membership.Observer{Store: store})
	selector.Config.SelectionLivenessWindow = loaded.SelectionLivenessWindow
	for _, slug := range []string{"acme/api", "acme/api-two", "acme/web", "acme/old"} {
		store.known = append(store.known, membership.KnownScope{ScopeID: gitScopeIDForRepositoryID(selector.Config, slug), Slug: slug})
	}
	discover := selector.DiscoverSelection
	selector.DiscoverSelection = func(ctx context.Context, config RepoSyncConfig, token string) (RepositorySelection, error) {
		discovered, err := discover(ctx, config, token)
		discovered.ListingComplete = true
		return discovered, err
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	if len(store.batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(store.batches))
	}
	batch := store.batches[0]
	if batch.LivenessWindow != 72*time.Hour || len(batch.Rows) != 4 {
		t.Fatalf("batch window %v with %d rows, want 72h over the 4 known scopes", batch.LivenessWindow, len(batch.Rows))
	}
}
