// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// fakeReindexWatermark answers ReindexWatermark and counts reads. Its only
// method is a read: the collector never claims or completes the request.
type fakeReindexWatermark struct {
	at    time.Time
	err   error
	calls int
}

func (f *fakeReindexWatermark) ReindexWatermark(context.Context) (time.Time, error) {
	f.calls++
	return f.at, f.err
}

// writeFreshScopesFakeGit creates managed checkouts for repos under reposDir
// and points the fake git at an unchanged remote head.
func writeFreshScopesFakeGit(t *testing.T, reposDir string, repos []string) {
	t.Helper()
	for _, repo := range repos {
		if err := os.MkdirAll(filepath.Join(reposDir, filepath.FromSlash(repo), ".git"), 0o755); err != nil {
			t.Fatalf("create .git marker: %v", err)
		}
	}
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "samesha\n"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)
}

// TestResolveReindexWatermark pins the per-cycle read: an active watermark is
// returned in UTC and logged at DEBUG, since the watermark persists and the
// read repeats every cycle; a watermark later than the cycle's
// observedAt is deferred, so a forced full is never stamped before the
// watermark it answers; a read failure is ignored for the cycle with a WARN; an
// unset watermark or a nil reader is inactive.
func TestResolveReindexWatermark(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	config := RepoSyncConfig{RepoShardCount: 2, RepoShardIndex: 1}
	cases := []struct {
		name    string
		reader  ReindexWatermarkReader
		want    time.Time
		wantLog []string
	}{
		{
			"active", &fakeReindexWatermark{at: observedAt.Add(-time.Minute).In(time.FixedZone("EDT", -4*3600))},
			observedAt.Add(-time.Minute),
			[]string{`"level":"DEBUG"`, `"msg":"git_reindex_watermark_active"`, `"reindex_requested_at":"2026-10-05T11:59:00Z"`, `"repo_shard_index":1`, `"repo_shard_count":2`},
		},
		{
			"equal to observedAt is active", &fakeReindexWatermark{at: observedAt}, observedAt,
			[]string{`"level":"DEBUG"`, `"msg":"git_reindex_watermark_active"`},
		},
		{
			"later than observedAt is deferred", &fakeReindexWatermark{at: observedAt.Add(time.Second)},
			time.Time{},
			[]string{`"level":"INFO"`, `"msg":"git_reindex_watermark_deferred"`, `"reindex_requested_at":"2026-10-05T12:00:01Z"`, `"observed_at":"2026-10-05T12:00:00Z"`},
		},
		{
			"read failure is ignored", &fakeReindexWatermark{err: errors.New("postgres down")},
			time.Time{},
			[]string{`"level":"WARN"`, `"msg":"git_reindex_watermark_read_failed"`, `postgres down`},
		},
		{"unset", &fakeReindexWatermark{}, time.Time{}, nil},
		{"nil reader", nil, time.Time{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			got := resolveReindexWatermark(context.Background(), tc.reader, observedAt, config,
				slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			if !got.Equal(tc.want) || (!got.IsZero() && got.Location() != time.UTC) {
				t.Fatalf("resolveReindexWatermark() = %v, want %v in UTC", got, tc.want)
			}
			for _, want := range tc.wantLog {
				if !strings.Contains(logs.String(), want) {
					t.Errorf("logs lack %s:\n%s", want, logs.String())
				}
			}
			if tc.wantLog == nil && logs.Len() != 0 {
				t.Errorf("logs = %s, want none", logs.String())
			}
		})
	}
}

// TestReindexWatermarkTwoShardsForceOnlyTheirOwnScopes: every shard reads the
// fleet watermark once per cycle and forces only the scopes it owns, so the
// shards together reindex every scope exactly once.
func TestReindexWatermarkTwoShardsForceOnlyTheirOwnScopes(t *testing.T) {
	reposDir := t.TempDir()
	repos := []string{"github/org/a", "github/org/b", "github/org/c", "github/org/d", "github/org/e", "github/org/f"}
	writeFreshScopesFakeGit(t, reposDir, repos)
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	reconciledBy := map[string]int{}
	for shard := 0; shard < 2; shard++ {
		config := reconcileTestConfig(reposDir)
		config.RepoShardCount, config.RepoShardIndex = 2, shard
		owned := filterRepositoryIDsByShard(repos, config)
		if len(owned) == 0 || len(owned) == len(repos) {
			t.Fatalf("shard %d owns %v; the fixture must split across both shards", shard, owned)
		}
		reader := &fakeReindexWatermark{at: now.Add(-30 * time.Minute)}
		selector := NativeRepositorySelector{
			Config: config,
			Now:    func() time.Time { return now },
			DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
				return RepositorySelection{RepositoryIDs: repos}, nil
			},
			Logger:           discardLogger(),
			BaselineResolver: &dirtyScopes{now: now, dirty: map[string]bool{}},
			ReindexWatermark: reader,
		}
		batch, err := selector.SelectRepositories(context.Background())
		if err != nil {
			t.Fatalf("shard %d SelectRepositories() error = %v", shard, err)
		}
		if reader.calls != 1 {
			t.Fatalf("shard %d watermark reads = %d, want 1 per cycle", shard, reader.calls)
		}
		ownedPaths := map[string]bool{}
		for _, repo := range owned {
			ownedPaths[filepath.Join(reposDir, filepath.FromSlash(repo))] = true
		}
		if len(batch.Repositories) != len(owned) {
			t.Fatalf("shard %d selected %d repositories, want its %d owned scopes", shard, len(batch.Repositories), len(owned))
		}
		for _, repository := range batch.Repositories {
			if !repository.Reconcile || !ownedPaths[repository.RepoPath] {
				t.Fatalf("shard %d selected %s (reconcile=%t), want only its owned scopes, each forced", shard, repository.RepoPath, repository.Reconcile)
			}
			reconciledBy[repository.RepoPath]++
		}
	}
	if len(reconciledBy) != len(repos) {
		t.Fatalf("reindexed %d scopes across shards, want all %d", len(reconciledBy), len(repos))
	}
	for path, count := range reconciledBy {
		if count != 1 {
			t.Fatalf("%s reindexed by %d shards, want exactly 1", path, count)
		}
	}
}

// TestReindexWatermarkReachesWebhookTriggeredScopes: the webhook selector
// threads the same watermark, so a triggered repository with an unchanged
// head and a full older than the watermark is re-parsed in full.
func TestReindexWatermarkReachesWebhookTriggeredScopes(t *testing.T) {
	reposDir := t.TempDir()
	writeFreshScopesFakeGit(t, reposDir, []string{"eshu-hq/eshu"})
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	store := &stubWebhookTriggerStore{claimed: []webhook.StoredTrigger{{
		TriggerID: "trigger-1",
		Trigger: webhook.Trigger{
			Provider: webhook.ProviderGitHub, Decision: webhook.DecisionAccepted,
			RepositoryExternalID: "42", RepositoryFullName: "eshu-hq/eshu", DefaultBranch: "main",
		},
	}}}
	reader := &fakeReindexWatermark{at: now.Add(-30 * time.Minute)}
	selector := WebhookTriggerRepositorySelector{
		Config:           reconcileTestConfig(reposDir),
		Store:            store,
		Owner:            "collector-git",
		Now:              func() time.Time { return now },
		Logger:           discardLogger(),
		BaselineResolver: &dirtyScopes{now: now, dirty: map[string]bool{}},
		ReindexWatermark: reader,
	}
	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	if len(batch.Repositories) != 1 || !batch.Repositories[0].Reconcile {
		t.Fatalf("repositories = %+v, want the triggered scope forced to a full re-parse", batch.Repositories)
	}
	if reader.calls != 1 {
		t.Fatalf("watermark reads = %d, want 1", reader.calls)
	}
}

// TestReconcileSweepDecisionHonorsReindexWatermark: the exported decision the
// end-to-end proofs use runs the same watermark rule, including Interval 0.
func TestReconcileSweepDecisionHonorsReindexWatermark(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	resolver := &stubBaselineResolver{state: reindexState(now.Add(-3*time.Hour), "", time.Time{})}
	for _, interval := range []time.Duration{24 * time.Hour, 0} {
		due, reason := ReconcileSweepDecision(context.Background(), resolver, interval, now.Add(-time.Hour), now, "scope-1", discardLogger())
		if !due || reason != reconcileReasonReindexRequested {
			t.Fatalf("interval %v: ReconcileSweepDecision() = (%t, %q), want (true, reindex_requested)", interval, due, reason)
		}
	}
	if due, reason := ReconcileSweepDecision(context.Background(), resolver, 0, time.Time{}, now, "scope-1", discardLogger()); due || reason != "" {
		t.Fatalf("interval 0 without a watermark = (%t, %q), want disabled", due, reason)
	}
}
