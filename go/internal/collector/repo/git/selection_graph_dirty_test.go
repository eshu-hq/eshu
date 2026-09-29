// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fallbackCountsByReason returns eshu_dp_collector_delta_baseline_fallback_total
// by skip_reason.
func fallbackCountsByReason(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("reader.Collect: %v", err)
	}
	counts := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_collector_delta_baseline_fallback_total" {
				continue
			}
			data, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q: unexpected data type %T", m.Name, m.Data)
			}
			for _, dp := range data.DataPoints {
				reason, _ := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionSkipReason))
				counts[reason.AsString()] += dp.Value
			}
		}
	}
	return counts
}

// TestGraphDirtyLookupErrorKeepsTheNormalPath: a failed uncovered-writer
// lookup is logged and the scope keeps its normal path this cycle (a no-op at
// the active head); a transient outage must not force a fleet of fulls.
func TestGraphDirtyLookupErrorKeepsTheNormalPath(t *testing.T) {
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("create .git marker: %v", err)
	}
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "activesha\n"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)
	now := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	resolver := &stubBaselineResolver{sha: "activesha", writersErr: errStubResolver, state: scope.FullReconcileState{
		HasProjectedFull: true, LastProjectedFullAt: now.Add(-time.Hour), HasLatestFull: true,
		LatestFullAt: now.Add(-time.Hour), LatestFullStatus: scope.GenerationStatusActive, LatestFullProjected: true,
	}}
	instruments, reader := newCollisionTestInstruments(t)
	var logs bytes.Buffer
	config := RepoSyncConfig{
		SourceMode: "explicit", ReposDir: reposDir, GitAuthMethod: "none", CloneDepth: 1,
		ReconcileInterval: 24 * time.Hour, ReconcileMaxPerCycle: 10,
	}
	synced, err := syncGitRepositoriesWithLogger(context.Background(), config, []string{"github/org/repo"},
		slog.New(slog.NewJSONHandler(&logs, nil)), gitDeltaBaseline{
			Resolver: resolver, Instruments: instruments, Reconcile: reconcilePolicyFromConfig(config),
			Now: func() time.Time { return now },
		})
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	if len(synced.SelectedRepoPaths) != 0 || len(synced.ReconcileByRepoPath) != 0 {
		t.Fatalf("selected %v reconciled %v, want the normal no-op at the active head", synced.SelectedRepoPaths, synced.ReconcileByRepoPath)
	}
	if got := fallbackCountsByReason(t, reader); len(got) != 0 {
		t.Fatalf("fallback counts = %v, want none", got)
	}
	if !strings.Contains(logs.String(), `"msg":"git_delta_baseline_graph_dirty_lookup_failed"`) {
		t.Fatalf("logs lack the lookup failure:\n%s", logs.String())
	}
}
