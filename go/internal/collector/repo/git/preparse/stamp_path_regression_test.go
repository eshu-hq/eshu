// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package preparse

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// routedReprojector answers like the production store on each path: the
// skip-path fence (reprojectByIdentityQuery in
// go/internal/storage/postgres/generation_freshness_reproject.go) matches only
// when the stored identity equals the requested one, so it refuses a stamp —
// whose stored identity is empty by definition — with
// reconcile_superseded; the stamp path (stampActiveGenerationQuery, same file)
// matches the empty stored identity and stamps the proven one.
type routedReprojector struct {
	skipCalls  int
	stampCalls int
	stampGot   scope.ActiveReprojection
}

func (r *routedReprojector) ReprojectActiveGeneration(_ context.Context, _ scope.ActiveReprojection) (string, error) {
	r.skipCalls++
	return telemetry.RefreshSkipReasonReconcileSuperseded, nil
}

func (r *routedReprojector) StampActiveGeneration(_ context.Context, request scope.ActiveReprojection) (string, error) {
	r.stampCalls++
	r.stampGot = request
	return telemetry.RefreshSkipReasonReconcileReprojected, nil
}

// TestRunStampReachesTheStoreStampPath (#8095 P0): a proven first-sweep stamp
// must skip the parse. The identity-equality fence refuses it (stored empty
// never equals requested non-empty), so Run must route stamps through the
// store stamp path — never through ReprojectActiveGeneration.
func TestRunStampReachesTheStoreStampPath(t *testing.T) {
	t.Parallel()
	reprojector := &routedReprojector{}
	attempt := Attempt{
		ScopeID: "git-repository-scope:first-sweep", Reason: ReasonIntervalElapsed,
		RemoteCommit: "c", Identity: "preparse:v1:now", MaxSkips: 7,
		StampWithoutParse: true, StampProven: true,
		State: scope.FullReconcileState{ActiveGenerationID: "gen-0", ActiveCommitSHA: "c"},
	}
	if !Run(context.Background(), attempt, func() (bool, error) { return true, nil }, reprojector, Recorder{}) {
		t.Fatalf("Run() = false (skip-path calls=%d stamp-path calls=%d), want the proven stamp to skip the parse",
			reprojector.skipCalls, reprojector.stampCalls)
	}
	if reprojector.stampCalls != 1 {
		t.Fatalf("stamp-path calls = %d, want 1", reprojector.stampCalls)
	}
	if reprojector.skipCalls != 0 {
		t.Fatalf("skip-path calls = %d, want 0 (the stamp must not take the identity-equality path)", reprojector.skipCalls)
	}
	if reprojector.stampGot.GenerationID != "gen-0" || reprojector.stampGot.CommitSHA != "c" ||
		reprojector.stampGot.PreparseIdentity != "preparse:v1:now" {
		t.Fatalf("stamp request = %+v, want gen-0 at c with the computed identity", reprojector.stampGot)
	}
}
