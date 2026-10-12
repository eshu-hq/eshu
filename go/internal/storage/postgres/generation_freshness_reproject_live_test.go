// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	preparseIdentityA = "preparse:v1:aaaa"
	preparseIdentityB = "preparse:v1:bbbb"
)

// identityGeneration is p.generation with a pre-parse identity.
func (p *reprojectProof) identityGeneration(id, hint, sha, reason, identity string) scope.ScopeGeneration {
	generation := p.generation(id, hint, sha, reason)
	generation.PreparseIdentity = identity
	return generation
}

// preparseStamp reads a generation's stored identity and skip count.
func (p *reprojectProof) preparseStamp(generation string) (identity string, skips int) {
	p.t.Helper()
	if err := p.db.QueryRowContext(context.Background(), `
SELECT COALESCE(payload ->> 'preparse_identity', ''), COALESCE((payload ->> 'preparse_skips')::int, 0)
FROM scope_generations WHERE generation_id = $1`, generation).Scan(&identity, &skips); err != nil {
		p.t.Fatalf("read %s preparse stamp: %v", generation, err)
	}
	return identity, skips
}

// cheapReproject runs the skipped-parse re-open for gen-a at sha-a.
func (p *reprojectProof) cheapReproject(generation, sha, identity string) string {
	p.t.Helper()
	outcome, err := p.store.ReprojectActiveGeneration(context.Background(), scope.ActiveReprojection{
		ScopeID: p.scope.ScopeID, GenerationID: generation, CommitSHA: sha,
		PreparseIdentity: identity, Reason: "interval_elapsed",
	})
	if err != nil {
		p.t.Fatalf("ReprojectActiveGeneration: %v", err)
	}
	return outcome
}

// TestPreparseIdentityStampsAndActiveStateLive (#7978 follow-up, ruling
// a9-143 item 3, bootstrap): a stored generation keeps its pre-parse identity;
// an active generation stored before the identity existed gets it from the
// parse-path re-open (the first sweep after deploy), with the skip count reset;
// FullReconcileState reports the active pointer's id, commit, identity, skips,
// and delta flag.
func TestPreparseIdentityStampsAndActiveStateLive(t *testing.T) {
	p := openReprojectProof(t)
	ctx := context.Background()
	p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
	p.project()
	if identity, _ := p.preparseStamp("gen-a"); identity != "" {
		t.Fatalf("gen-a identity = %q before any stamp, want empty", identity)
	}
	state, err := p.store.FullReconcileState(ctx, p.scope.ScopeID)
	if err != nil || state.ActiveGenerationID != "gen-a" || state.ActiveCommitSHA != "sha-a" ||
		state.ActiveIsDelta || state.ActivePreparseIdentity != "" {
		t.Fatalf("active state = %+v (%v), want gen-a sha-a full with no identity", state, err)
	}
	// First sweep after deploy: the parse ran, the hint matched, the store
	// re-opens gen-a and stamps the identity the parse computed.
	p.commit(p.identityGeneration("gen-r", "hint-a", "sha-a", "interval_elapsed", preparseIdentityA))
	if identity, skips := p.preparseStamp("gen-a"); identity != preparseIdentityA || skips != 0 {
		t.Fatalf("gen-a stamp after the parse-path re-open = %q/%d, want %q/0", identity, skips, preparseIdentityA)
	}
	if state, err = p.store.FullReconcileState(ctx, p.scope.ScopeID); err != nil || state.ActivePreparseIdentity != preparseIdentityA {
		t.Fatalf("active identity = %q (%v), want %q", state.ActivePreparseIdentity, err, preparseIdentityA)
	}
	// A stored generation keeps the identity its snapshot carried.
	p.project()
	p.commit(p.identityGeneration("gen-b", "hint-b", "sha-b", "", preparseIdentityB))
	if identity, _ := p.preparseStamp("gen-b"); identity != preparseIdentityB {
		t.Fatalf("stored gen-b identity = %q, want %q", identity, preparseIdentityB)
	}
}

// TestCheapReprojectLifecycleLive pins the skipped-parse re-open (ruling
// a9-143 item 2): it re-opens the active generation's canonical row under the
// same lock and in-flight guard as the store re-open, stamps the request and
// the skip count only on a real re-open, and is fenced on generation, commit,
// and identity.
func TestCheapReprojectLifecycleLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	ctx := context.Background()
	p.commit(p.identityGeneration("gen-a", "hint-a", "sha-a", "", preparseIdentityA))
	p.project()
	before := p.rowCounts()

	if got := p.cheapReproject("gen-a", "sha-a", preparseIdentityA); got != telemetry.RefreshSkipReasonReconcileReprojected {
		t.Fatalf("first cheap re-open = %q, want %q", got, telemetry.RefreshSkipReasonReconcileReprojected)
	}
	if status, reopens, _ := p.workRow("gen-a"); status != "pending" || reopens != 1 {
		t.Fatalf("gen-a work = %s reopens=%d, want pending reopens=1", status, reopens)
	}
	state, err := p.store.FullReconcileState(ctx, p.scope.ScopeID)
	if err != nil || state.ReconcileRequestedAt.IsZero() || state.ActivePreparseSkips != 1 {
		t.Fatalf("after the cheap re-open request=%v skips=%d (%v), want a request and skips 1",
			state.ReconcileRequestedAt, state.ActivePreparseSkips, err)
	}
	// Ack race: the row is pending, so a repeat neither re-opens nor stamps.
	if got := p.cheapReproject("gen-a", "sha-a", preparseIdentityA); got != telemetry.RefreshSkipReasonReconcileReprojectionPending {
		t.Fatalf("repeat while pending = %q, want %q", got, telemetry.RefreshSkipReasonReconcileReprojectionPending)
	}
	if _, skips := p.preparseStamp("gen-a"); skips != 1 {
		t.Fatalf("skips after a pending repeat = %d, want 1", skips)
	}
	p.project()
	if got := p.cheapReproject("gen-a", "sha-a", preparseIdentityA); got != telemetry.RefreshSkipReasonReconcileReprojected {
		t.Fatalf("cheap re-open after the Ack = %q, want reprojected", got)
	}
	if _, skips := p.preparseStamp("gen-a"); skips != 2 {
		t.Fatalf("skips after the second cheap re-open = %d, want 2", skips)
	}
	// Fences: a different identity or commit is a no-op.
	p.project()
	for _, fence := range []struct{ sha, identity string }{{"sha-a", preparseIdentityB}, {"sha-x", preparseIdentityA}} {
		if got := p.cheapReproject("gen-a", fence.sha, fence.identity); got != telemetry.RefreshSkipReasonReconcileSuperseded {
			t.Fatalf("fenced cheap re-open %v = %q, want superseded", fence, got)
		}
	}
	if _, skips := p.preparseStamp("gen-a"); skips != 2 {
		t.Fatalf("skips after fenced calls = %d, want 2", skips)
	}
	// The parse path resets the valve count when it re-opens.
	p.commit(p.identityGeneration("gen-r", "hint-a", "sha-a", "interval_elapsed", preparseIdentityA))
	if _, skips := p.preparseStamp("gen-a"); skips != 0 {
		t.Fatalf("skips after a parse-path re-open = %d, want 0", skips)
	}
	if after := p.rowCounts(); after["scope_generations"] != before["scope_generations"] || after["fact_records"] != before["fact_records"] {
		t.Fatalf("rows %v -> %v, want no stored generation or fact", before, after)
	}
}

// TestCheapReprojectRacesLive: a webhook commit that activated a newer
// generation first fences the cheap re-open to a no-op; a delta-active scope
// refuses it; a dead-lettered cheap re-projection is spared like the store
// re-open's (a9-130) and the stale request stores a generation.
func TestCheapReprojectRacesLive(t *testing.T) {
	t.Run("newer generation activated first", func(t *testing.T) {
		p := openReprojectProof(t)
		p.commit(p.identityGeneration("gen-a", "hint-a", "sha-a", "", preparseIdentityA))
		p.project()
		p.commit(p.identityGeneration("gen-b", "hint-b", "sha-b", "", preparseIdentityB))
		p.project()
		if got := p.cheapReproject("gen-a", "sha-a", preparseIdentityA); got != telemetry.RefreshSkipReasonReconcileSuperseded {
			t.Fatalf("cheap re-open of a superseded generation = %q, want superseded", got)
		}
		if status, reopens, _ := p.workRow("gen-a"); status == "pending" || reopens != 0 {
			t.Fatalf("superseded gen-a work = %s reopens=%d, want untouched", status, reopens)
		}
	})
	t.Run("delta-active scope", func(t *testing.T) {
		p := openReprojectProof(t)
		ctx := context.Background()
		p.commit(p.identityGeneration("gen-a", "hint-a", "sha-a", "", preparseIdentityA))
		p.project()
		if _, err := p.db.ExecContext(ctx, `UPDATE scope_generations SET is_delta = true WHERE generation_id = 'gen-a'`); err != nil {
			t.Fatalf("mark gen-a delta: %v", err)
		}
		state, err := p.store.FullReconcileState(ctx, p.scope.ScopeID)
		if err != nil || !state.ActiveIsDelta {
			t.Fatalf("ActiveIsDelta = %v (%v), want true", state.ActiveIsDelta, err)
		}
		if got := p.cheapReproject("gen-a", "sha-a", preparseIdentityA); got != telemetry.RefreshSkipReasonReconcileSuperseded {
			t.Fatalf("cheap re-open of a delta = %q, want superseded (refused)", got)
		}
	})
	t.Run("dead-lettered cheap re-projection", func(t *testing.T) {
		p := openReprojectProof(t)
		ctx := context.Background()
		p.commit(p.identityGeneration("gen-a", "hint-a", "sha-a", "", preparseIdentityA))
		p.project()
		p.cheapReproject("gen-a", "sha-a", preparseIdentityA)
		p.failNextProjectorWork("gen-a")
		got := p.deadLetterOutcomeFor("gen-a")
		if got.status != "active" || got.pointer.String != "gen-a" || !got.deadLettered {
			t.Fatalf("outcome = %+v, want spared with a dead-letter stamp", got)
		}
		state, err := p.store.FullReconcileState(ctx, p.scope.ScopeID)
		if err != nil || state.ReconcileDeadLetteredAt.Before(state.ReconcileRequestedAt) {
			t.Fatalf("request=%v dead letter=%v (%v), want the stale-request input", state.ReconcileRequestedAt, state.ReconcileDeadLetteredAt, err)
		}
		before := p.rowCounts()
		p.commit(p.identityGeneration("gen-stale", "hint-a", "sha-a", scope.ReconcileReasonRequestStale, preparseIdentityA))
		if after := p.rowCounts(); after["scope_generations"] != before["scope_generations"]+1 {
			t.Fatalf("stale request rows %v -> %v, want one stored generation", before, after)
		}
	})
}
