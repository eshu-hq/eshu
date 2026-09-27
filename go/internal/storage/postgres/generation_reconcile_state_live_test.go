// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestFullReconcileStateLive proves fullReconcileStateQuery on real Postgres
// (#7288): last_projected_full_at counts only an activated full generation,
// and latest_full_* reports the newest full generation of any status while
// ignoring delta generations.
func TestFullReconcileStateLive(t *testing.T) {
	base := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	projectedAt := base.Add(-48 * time.Hour)
	projected := proofGeneration{id: "gen-projected", status: "active", sha: "sha-p", ingestedAt: projectedAt, activatedAt: proofTime(projectedAt)}
	withProjected := func(state scope.FullReconcileState) scope.FullReconcileState {
		state.HasProjectedFull = true
		state.LastProjectedFullAt = projectedAt
		state.HasLatestFull = true
		return state
	}
	cases := []struct {
		name   string
		latest *proofGeneration
		want   scope.FullReconcileState
	}{
		{
			name: "latest full is projected",
			want: withProjected(scope.FullReconcileState{LatestFullAt: projectedAt, LatestFullStatus: scope.GenerationStatusActive, LatestFullProjected: true}),
		},
		{
			name:   "latest full pending 30m",
			latest: &proofGeneration{id: "gen-latest", status: "pending", ingestedAt: base.Add(-30 * time.Minute)},
			want:   withProjected(scope.FullReconcileState{LatestFullAt: base.Add(-30 * time.Minute), LatestFullStatus: scope.GenerationStatusPending}),
		},
		{
			name:   "latest full pending 25h",
			latest: &proofGeneration{id: "gen-latest", status: "pending", ingestedAt: base.Add(-25 * time.Hour)},
			want:   withProjected(scope.FullReconcileState{LatestFullAt: base.Add(-25 * time.Hour), LatestFullStatus: scope.GenerationStatusPending}),
		},
		{
			name:   "latest full failed 1h",
			latest: &proofGeneration{id: "gen-latest", status: "failed", ingestedAt: base.Add(-time.Hour)},
			want:   withProjected(scope.FullReconcileState{LatestFullAt: base.Add(-time.Hour), LatestFullStatus: scope.GenerationStatusFailed}),
		},
		{
			name:   "latest full failed 7h",
			latest: &proofGeneration{id: "gen-latest", status: "failed", ingestedAt: base.Add(-7 * time.Hour)},
			want:   withProjected(scope.FullReconcileState{LatestFullAt: base.Add(-7 * time.Hour), LatestFullStatus: scope.GenerationStatusFailed}),
		},
		{
			name:   "latest full superseded before activation 1h",
			latest: &proofGeneration{id: "gen-latest", status: "superseded", ingestedAt: base.Add(-time.Hour)},
			want:   withProjected(scope.FullReconcileState{LatestFullAt: base.Add(-time.Hour), LatestFullStatus: scope.GenerationStatusSuperseded}),
		},
		{
			name:   "pending delta newer than the projected full is ignored",
			latest: &proofGeneration{id: "gen-delta", isDelta: true, status: "pending", ingestedAt: base.Add(-10 * time.Minute)},
			want:   withProjected(scope.FullReconcileState{LatestFullAt: projectedAt, LatestFullStatus: scope.GenerationStatusActive, LatestFullProjected: true}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scopeID := "git-repository-scope:proof/reconcile"
			generations := []proofGeneration{projected}
			if tc.latest != nil {
				generations = append(generations, *tc.latest)
			}
			store, _ := openProjectedCommitProof(t, scopeID, generations)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			got, err := store.FullReconcileState(ctx, scopeID)
			if err != nil {
				t.Fatalf("FullReconcileState() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("FullReconcileState() = %+v, want %+v", got, tc.want)
			}
		})
	}

	t.Run("scope with no generation is the zero state", func(t *testing.T) {
		scopeID := "git-repository-scope:proof/empty"
		store, _ := openProjectedCommitProof(t, scopeID, nil)
		got, err := store.FullReconcileState(t.Context(), scopeID)
		if err != nil {
			t.Fatalf("FullReconcileState() error = %v", err)
		}
		if got != (scope.FullReconcileState{}) {
			t.Fatalf("FullReconcileState() = %+v, want zero state", got)
		}
	})
}
