// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
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

// stampGeneration runs the first-sweep stamp re-open (#8095) for generation
// at sha with the proven identity.
func (p *reprojectProof) stampGeneration(generation, sha, identity string) string {
	p.t.Helper()
	outcome, err := p.store.StampActiveGeneration(context.Background(), scope.ActiveReprojection{
		ScopeID: p.scope.ScopeID, GenerationID: generation, CommitSHA: sha,
		PreparseIdentity: identity, Reason: "interval_elapsed",
	})
	if err != nil {
		p.t.Fatalf("StampActiveGeneration: %v", err)
	}
	return outcome
}

// TestStampActiveGenerationLifecycleLive (#8095 P0): the stamp re-open matches
// an empty stored identity at the same commit on a full active generation,
// stamps the proven identity with the skip count reset (a stamp counts as a
// parse for the valve), and re-opens the canonical projector row under the
// same lock and in-flight guard as the store re-open. A repeat after a
// successful stamp is superseded by the one-shot fence (the stored identity
// is non-empty, so the target cannot match again) — while pending and after
// the Ack alike; neither re-opens nor re-stamps. A moved commit or an empty
// request is superseded too.
func TestStampActiveGenerationLifecycleLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	ctx := context.Background()
	// First sweep after deploy: the active generation carries no identity.
	p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
	p.project()
	if identity, _ := p.preparseStamp("gen-a"); identity != "" {
		t.Fatalf("gen-a identity = %q before any stamp, want empty", identity)
	}
	if got := p.stampGeneration("gen-a", "sha-a", preparseIdentityA); got != telemetry.RefreshSkipReasonReconcileReprojected {
		t.Fatalf("first stamp = %q, want %q", got, telemetry.RefreshSkipReasonReconcileReprojected)
	}
	if identity, skips := p.preparseStamp("gen-a"); identity != preparseIdentityA || skips != 0 {
		t.Fatalf("gen-a stamp = %q/%d, want %q/0 (identity stamped, valve count reset)", identity, skips, preparseIdentityA)
	}
	if status, reopens, _ := p.workRow("gen-a"); status != "pending" || reopens != 1 {
		t.Fatalf("gen-a work = %s reopens=%d, want pending reopens=1", status, reopens)
	}
	state, err := p.store.FullReconcileState(ctx, p.scope.ScopeID)
	if err != nil || state.ActivePreparseIdentity != preparseIdentityA || state.ActivePreparseSkips != 0 {
		t.Fatalf("active identity = %q skips = %d (%v), want %q and 0",
			state.ActivePreparseIdentity, state.ActivePreparseSkips, err, preparseIdentityA)
	}
	// Repeat after a successful stamp: the one-shot fence wins over the
	// in-flight guard (the stored identity is non-empty, so the target cannot
	// match again) — superseded, nothing re-stamped. Production reaches a
	// repeat stamp only via a concurrent double-decide race, whose loser
	// parsing is the fail-safe direction.
	if got := p.stampGeneration("gen-a", "sha-a", preparseIdentityA); got != telemetry.RefreshSkipReasonReconcileSuperseded {
		t.Fatalf("repeat while pending = %q, want %q (one-shot fence: an identity is already stored)", got, telemetry.RefreshSkipReasonReconcileSuperseded)
	}
	if identity, skips := p.preparseStamp("gen-a"); identity != preparseIdentityA || skips != 0 {
		t.Fatalf("gen-a stamp after a pending repeat = %q/%d, want %q/0", identity, skips, preparseIdentityA)
	}
	p.project()
	// One-shot fence: an identity is stored now, so a second stamp parses.
	if got := p.stampGeneration("gen-a", "sha-a", preparseIdentityA); got != telemetry.RefreshSkipReasonReconcileSuperseded {
		t.Fatalf("second stamp = %q, want %q (an identity is already stored)", got, telemetry.RefreshSkipReasonReconcileSuperseded)
	}
	if identity, skips := p.preparseStamp("gen-a"); identity != preparseIdentityA || skips != 0 {
		t.Fatalf("gen-a stamp after a refused second stamp = %q/%d, want %q/0", identity, skips, preparseIdentityA)
	}
	// A fresh active generation without an identity fences on commit and on
	// the non-empty request, then stamps.
	p.commit(p.generation("gen-b", "hint-b", "sha-b", ""))
	p.project()
	for _, fence := range []struct {
		name, sha, identity string
	}{{"moved commit", "sha-x", preparseIdentityB}, {"empty request", "sha-b", ""}} {
		if got := p.stampGeneration("gen-b", fence.sha, fence.identity); got != telemetry.RefreshSkipReasonReconcileSuperseded {
			t.Fatalf("fenced stamp %s = %q, want superseded", fence.name, got)
		}
	}
	if identity, _ := p.preparseStamp("gen-b"); identity != "" {
		t.Fatalf("gen-b identity after fenced stamps = %q, want empty", identity)
	}
	if got := p.stampGeneration("gen-b", "sha-b", preparseIdentityB); got != telemetry.RefreshSkipReasonReconcileReprojected {
		t.Fatalf("gen-b stamp = %q, want %q", got, telemetry.RefreshSkipReasonReconcileReprojected)
	}
	if identity, skips := p.preparseStamp("gen-b"); identity != preparseIdentityB || skips != 0 {
		t.Fatalf("gen-b stamp = %q/%d, want %q/0", identity, skips, preparseIdentityB)
	}
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

// commitStreamed commits generation with a fact stream fed the way streamFacts
// feeds one (#7978 review F7-3): a goroutine sends every envelope, then, when
// drop is true, sets the generation's PreparseIdentityDropped flag, and only
// then closes the channel. A store that reads the flag before it has drained
// the stream sees false. hook false leaves the flag nil (another collector).
func (p *reprojectProof) commitStreamed(generation scope.ScopeGeneration, hook, drop bool) {
	p.t.Helper()
	flag := new(atomic.Bool)
	if hook {
		generation.PreparseIdentityDropped = flag
	}
	stream := make(chan facts.Envelope)
	go func() {
		for i := 0; i < p.facts; i++ {
			stream <- facts.Envelope{
				FactID:        fmt.Sprintf("%s-fact-%04d", generation.GenerationID, i),
				ScopeID:       generation.ScopeID,
				GenerationID:  generation.GenerationID,
				FactKind:      "file",
				StableFactKey: fmt.Sprintf("file:proof/7978:f%04d.go", i),
				ObservedAt:    generation.ObservedAt,
				Payload:       map[string]any{"relative_path": fmt.Sprintf("f%04d.go", i)},
			}
		}
		if drop {
			flag.Store(true)
		}
		close(stream)
	}()
	if err := p.store.CommitScopeGeneration(context.Background(), p.scope, generation, stream); err != nil {
		p.t.Fatalf("commit %s: %v", generation.GenerationID, err)
	}
}

// streamDroppedMark reports whether a generation row carries stream_dropped,
// and whether the row exists at all.
func (p *reprojectProof) streamDroppedMark(generation string) (marked, exists bool) {
	p.t.Helper()
	err := p.db.QueryRowContext(context.Background(), `
SELECT COALESCE((payload ->> 'stream_dropped')::boolean, false) FROM scope_generations WHERE generation_id = $1`,
		generation).Scan(&marked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false
	}
	if err != nil {
		p.t.Fatalf("read %s stream_dropped: %v", generation, err)
	}
	return marked, true
}

// TestPreparseIdentityDroppedByTheStreamLive (#7978 review F-7, F7-3, rulings
// a9-143.18 and a9-143.19): the producer sets the drop flag after its last
// send, so the store must read it after the drain. The stored path then keeps
// the identity off its own row and marks it stream_dropped; the equal-hint
// re-open stamps nothing and keeps the skip count. No hook, or a flag that
// stays false, changes nothing.
func TestPreparseIdentityDroppedByTheStreamLive(t *testing.T) {
	t.Run("stored generation", func(t *testing.T) {
		for _, cell := range []struct {
			name       string
			hook, drop bool
		}{{"drop", true, true}, {"no drop", true, false}, {"no hook", false, false}} {
			p := openReprojectProof(t)
			p.commitStreamed(p.identityGeneration("gen-b", "hint-b", "sha-b", "", preparseIdentityB), cell.hook, cell.drop)
			identity, _ := p.preparseStamp("gen-b")
			marked, _ := p.streamDroppedMark("gen-b")
			if cell.drop && (identity != "" || !marked) {
				t.Fatalf("%s: gen-b identity %q marked %v after a stream drop, want no identity and stream_dropped", cell.name, identity, marked)
			}
			if !cell.drop && (identity != preparseIdentityB || marked) {
				t.Fatalf("%s: gen-b identity %q marked %v, want %q and unmarked", cell.name, identity, marked, preparseIdentityB)
			}
		}
	})
	t.Run("equal-hint re-open stamps nothing", func(t *testing.T) {
		p := openReprojectProof(t)
		p.commit(p.identityGeneration("gen-a", "hint-a", "sha-a", "", preparseIdentityA))
		p.project()
		// One skipped parse first, so the skip count is 1: a stamp resets it.
		if outcome := p.cheapReproject("gen-a", "sha-a", preparseIdentityA); outcome != telemetry.RefreshSkipReasonReconcileReprojected {
			t.Fatalf("cheap re-open = %q, want reprojected", outcome)
		}
		p.project()
		p.commitStreamed(p.identityGeneration("gen-r1", "hint-a", "sha-a", "interval_elapsed", preparseIdentityA), true, true)
		if identity, skips := p.preparseStamp("gen-a"); identity != preparseIdentityA || skips != 1 {
			t.Fatalf("gen-a stamp after a re-open with a stream drop = %q/%d, want %q/1 (no stamp)", identity, skips, preparseIdentityA)
		}
	})
}

// TestStreamDroppedGenerationHealsLive (#7978 review F7-4, ruling a9-143.20):
// a stored generation whose stream dropped a file is marked, so the next equal
// snapshot stores a NEW generation that carries the facts and the identity
// instead of re-opening the marked one; the run after that re-opens the new
// generation.
func TestStreamDroppedGenerationHealsLive(t *testing.T) {
	p := openReprojectProof(t)
	p.commitStreamed(p.identityGeneration("gen-1", "hint-b", "sha-b", "", preparseIdentityB), true, true)
	if identity, _ := p.preparseStamp("gen-1"); identity != "" {
		t.Fatalf("run 1: gen-1 identity = %q, want none after a drop", identity)
	}
	if marked, _ := p.streamDroppedMark("gen-1"); !marked {
		t.Fatal("run 1: gen-1 is not marked stream_dropped")
	}
	p.project()
	p.commitStreamed(p.identityGeneration("gen-2", "hint-b", "sha-b", "interval_elapsed", preparseIdentityB), true, false)
	if _, exists := p.streamDroppedMark("gen-2"); !exists {
		t.Fatal("run 2: no new generation gen-2 was stored; the marked gen-1 was re-opened instead")
	}
	if identity, _ := p.preparseStamp("gen-2"); identity != preparseIdentityB {
		t.Fatalf("run 2: gen-2 identity = %q, want %q", identity, preparseIdentityB)
	}
	p.project()
	p.commitStreamed(p.identityGeneration("gen-3", "hint-b", "sha-b", "interval_elapsed", preparseIdentityB), true, false)
	if _, exists := p.streamDroppedMark("gen-3"); exists {
		t.Fatal("run 3: gen-3 was stored; want a re-open of gen-2")
	}
	if identity, _ := p.preparseStamp("gen-2"); identity != preparseIdentityB {
		t.Fatalf("run 3: gen-2 identity = %q, want %q after the re-open", identity, preparseIdentityB)
	}
}

// TestIdentitylessStreamDropIsMarkedLive (#7978 review F7-5, ruling
// a9-143.23): a generation that had no pre-parse identity (the first
// generation of a new clone, a dirty checkout, a failed tree check) and whose
// stream dropped a file is still marked stream_dropped, so the next equal
// snapshot stores a NEW generation instead of re-opening it and stamping an
// identity onto incomplete facts.
func TestIdentitylessStreamDropIsMarkedLive(t *testing.T) {
	p := openReprojectProof(t)
	p.commitStreamed(p.identityGeneration("gen-1", "hint-b", "sha-b", "", ""), true, true)
	if marked, _ := p.streamDroppedMark("gen-1"); !marked {
		t.Fatal("run 1: gen-1 had no identity and its stream dropped a file, but it is not marked stream_dropped")
	}
	p.project()
	p.commitStreamed(p.identityGeneration("gen-2", "hint-b", "sha-b", "interval_elapsed", preparseIdentityB), true, false)
	if _, exists := p.streamDroppedMark("gen-2"); !exists {
		t.Fatal("run 2: no new generation gen-2 was stored; the marked gen-1 was re-opened instead")
	}
	if identity, _ := p.preparseStamp("gen-1"); identity != "" {
		t.Fatalf("gen-1 identity = %q, want none: an incomplete generation must never get an identity", identity)
	}
}
