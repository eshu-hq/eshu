// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// Regression for #7389: a delta generation could activate over a graph that a
// superseded or refused, never-activated generation wrote. These tests keep
// the reproduction's gated interleavings and assert the write-through
// contract: a generation that started writing runs to Ack, the stale delta is
// refused at preflight, and an Ack-refused writer is reported by
// UncoveredProjectionWriters until a full generation covers it. Opt-in: live
// Postgres (ESHU_7389_PROOF_POSTGRES_DSN, a disposable database the test
// resets) and live Neo4j (ESHU_NEO4J_URI/USERNAME/PASSWORD/DATABASE).
//
// Every subtest drives the production projector Service from
// buildProjectorService (ProjectorQueue claim, heartbeat supersede,
// AckWhenScopeFree, the #7319 fence, Postgres FactStore, projector runtime,
// content writer) over the production Neo4j canonical writer from
// openProjectorCanonicalWriter. Only the collector is replaced (see
// superseded_delta_overlay_helpers_test.go). Each subtest sequences one race
// with a wrapper that runs the real writer or heartbeat and adds only an
// ordering step.
//
// Trees: A = {keep.go, w.go}; B = {keep.go, x.go} (x.go new, w.go deleted);
// D = {keep.go, w.go, y.go} (y.go new; x.go absent from A and D; w.go as at
// A). The collector diffs both B and D from the active commit A, so
// diff(A,D) names only y.go. E is the delta the collector emits once B is
// active: diff(B,D) names w.go, y.go and the deleted x.go.
//
// Skills active: eshu-diagnostic-rigor, eshu-correlation-truth,
// concurrency-deadlock-rigor, eshu-postgres-rigor, cypher-query-rigor,
// golang-engineering.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	overlayCommitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	overlayCommitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	overlayCommitC = "cccccccccccccccccccccccccccccccccccccccc"
	overlayCommitD = "dddddddddddddddddddddddddddddddddddddddd"
)

var (
	overlayTreeA = []overlayFile{{"keep.go", "package p\n"}, {"w.go", "package p\n// w\n"}}

	overlayGenA = overlayGen{id: "gen-7389-a", commit: overlayCommitA, files: overlayTreeA}
	overlayGenB = overlayGen{
		id: "gen-7389-b", commit: overlayCommitB, baseline: overlayCommitA, delta: true,
		files: []overlayFile{{"x.go", "package p\n// x\n"}}, changed: []string{"x.go", "w.go"}, deleted: []string{"w.go"},
	}
	overlayGenD = overlayGen{
		id: "gen-7389-d", commit: overlayCommitD, baseline: overlayCommitA, delta: true,
		files: []overlayFile{{"y.go", "package p\n// y\n"}}, changed: []string{"y.go"},
	}
	overlayGenE = overlayGen{
		id: "gen-7389-e", commit: overlayCommitD, baseline: overlayCommitB, delta: true,
		files:   []overlayFile{{"w.go", "package p\n// w\n"}, {"y.go", "package p\n// y\n"}},
		changed: []string{"w.go", "y.go", "x.go"}, deleted: []string{"x.go"},
	}
)

func overlayProofEnv(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_7389_PROOF_POSTGRES_DSN"))
	if dsn == "" || strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI")) == "" {
		t.Skip("set ESHU_7389_PROOF_POSTGRES_DSN (disposable database) and ESHU_NEO4J_URI/USERNAME/PASSWORD/DATABASE")
	}
	return dsn
}

// TestSupersededDeltaOverlaySurvivesNextDeltaLive: G_B (delta A->B) writes its
// overlay and G_D (delta A->D) is committed while G_B is still in flight. The
// pre-#7389 heartbeat superseded G_B and G_D activated over its overlay. Now
// G_B's write-start marker keeps the heartbeat from superseding it, so G_B
// activates at B; G_D is refused at preflight (its baseline A is no longer
// active); and the collector's next delta G_E = diff(B,D) converges on D.
func TestSupersededDeltaOverlaySurvivesNextDeltaLive(t *testing.T) {
	dsn := overlayProofEnv(t)

	// Step (a): heartbeat ticks during projection, after G_B's canonical and
	// content writes committed and G_D was committed, must not supersede G_B.
	t.Run("heartbeat_during_projection", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		h := newOverlayHarness(ctx, t, dsn)
		svc, _ := h.buildService(ctx)
		// Production is lease/3 = 20s; shortened so the proof does not wait.
		svc.HeartbeatInterval = 200 * time.Millisecond
		rt := svc.Runner.(runtime.Runtime)
		gate := &gatedContentWriter{
			inner: rt.ContentWriter, gatedID: overlayGenB.id,
			after: func(gctx context.Context) error {
				if err := h.commit(context.WithoutCancel(gctx), overlayGenD); err != nil {
					return err
				}
				return survivesHeartbeats(gctx, 5*svc.HeartbeatInterval)
			},
		}
		rt.ContentWriter = gate
		svc.Runner = rt
		runDeltaOverlayScenario(ctx, t, h, svc, func() bool { return gate.fired.Load() })
	})

	// Step (b): G_B's projection finished; its Ack defers on the scope row an
	// ingestion commit holds, and the ingestion commit of G_D lands before
	// AckWhenScopeFree's renewal heartbeat, which must not supersede G_B.
	t.Run("heartbeat_during_ack_wait", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		h := newOverlayHarness(ctx, t, dsn)
		svc, _ := h.buildService(ctx)
		svc.HeartbeatInterval = time.Hour // only AckWhenScopeFree renews
		hb := &hookedHeartbeater{inner: svc.Heartbeater}
		rt := svc.Runner.(runtime.Runtime)
		gate := &gatedContentWriter{
			inner: rt.ContentWriter, gatedID: overlayGenB.id,
			after: func(gctx context.Context) error {
				// Stand-in for the G_D ingestion transaction's scope-row lock.
				holder, err := h.sqlDB.BeginTx(context.WithoutCancel(gctx), nil)
				if err != nil {
					return err
				}
				if _, err := holder.ExecContext(gctx, `SELECT 1 FROM ingestion_scopes WHERE scope_id = $1 FOR NO KEY UPDATE`,
					h.scope.ScopeID); err != nil {
					_ = holder.Rollback()
					return err
				}
				hb.before = func(bctx context.Context) error {
					_ = holder.Rollback()
					return h.commit(context.WithoutCancel(bctx), overlayGenD)
				}
				hb.armed.Store(true)
				return nil
			},
		}
		rt.ContentWriter = gate
		svc.Runner = rt
		svc.Heartbeater = hb
		runDeltaOverlayScenario(ctx, t, h, svc, func() bool { return gate.fired.Load() && !hb.armed.Load() })
	})
}

func runDeltaOverlayScenario(ctx context.Context, t *testing.T, h *overlayHarness, svc projector.Service, fired func() bool) {
	t.Helper()
	h.run(ctx, svc)
	if err := h.commit(ctx, overlayGenA); err != nil {
		t.FailNow()
	}
	h.waitGeneration(ctx, overlayGenA.id, "active")
	h.logState(ctx, "after G_A (full @A) activated")
	h.assertFiles(ctx, "baseline G_A", []string{"keep.go", "w.go"})

	if err := h.commit(ctx, overlayGenB); err != nil {
		t.FailNow()
	}
	h.waitGeneration(ctx, overlayGenB.id, "active")
	h.waitGeneration(ctx, overlayGenD.id, "superseded")
	if !fired() {
		t.Fatal("the race was not sequenced: gate did not fire")
	}
	h.logGenerations(ctx)
	h.logState(ctx, "after G_B activated and G_D was refused at preflight")
	if class := h.workFailureClass(ctx, overlayGenD.id); class != projector.DeltaBaselineMismatchClass {
		t.Fatalf("G_D failure_class = %q, want %q (refused at preflight, before any write)",
			class, projector.DeltaBaselineMismatchClass)
	}
	if !h.writeStarted(ctx, overlayGenB.id) || h.writeStarted(ctx, overlayGenD.id) {
		t.Fatal("want G_B's write-start marker set and G_D's unset")
	}
	h.assertFiles(ctx, "after G_B activated (active commit B)", []string{"keep.go", "x.go"})

	// The collector's next delta is diffed from the active commit B.
	if err := h.commit(ctx, overlayGenE); err != nil {
		t.FailNow()
	}
	h.waitGeneration(ctx, overlayGenE.id, "active")
	h.logState(ctx, "after G_E (delta B->D) activated")
	h.assertFiles(ctx, "after G_E activated (active commit D)", []string{"keep.go", "w.go", "y.go"})
}

// TestDeltaRefusedAtAckLeavesOverlayLive covers the #7319 Ack-phase refusal.
// Its precondition is the breach that fence documents: a second valid claim in
// the scope. G_C (full @C, tree == A) is committed, claimed by another worker
// (seeded, since the claim path never grants it while G_B is in flight),
// projected by the production runtime, and Acked by the production queue
// between G_B's canonical write and G_B's Ack. G_B's Ack is then refused
// (active commit C is not its baseline A) and its overlay is still in the
// graph. The write-start marker makes that visible: the production
// UncoveredProjectionWriters reports G_B, which is what forces the collector's
// next sync to a full snapshot, and a full generation at C converges.
func TestDeltaRefusedAtAckLeavesOverlayLive(t *testing.T) {
	dsn := overlayProofEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	h := newOverlayHarness(ctx, t, dsn)
	svc, rawWriter := h.buildService(ctx)
	svc.HeartbeatInterval = time.Hour
	genC := overlayGen{id: "gen-7389-c", commit: overlayCommitC, files: overlayTreeA}
	queueC := postgres.NewProjectorQueue(h.pgDB, "worker-c", time.Minute)

	rt := svc.Runner.(runtime.Runtime)
	rtC := rt // production runtime with the unwrapped writer, for worker-c
	var workC projector.ScopeGenerationWork
	gate := &gatedCanonicalWriter{
		inner: rawWriter, gatedID: overlayGenB.id,
		before: func(gctx context.Context) error {
			bg := context.WithoutCancel(gctx)
			if err := h.commit(bg, genC); err != nil {
				return err
			}
			if _, err := h.sqlDB.ExecContext(bg, `UPDATE fact_work_items
SET status = 'running', lease_owner = 'worker-c', attempt_count = 1,
    claim_until = now() + interval '5 minutes', updated_at = now()
WHERE generation_id = $1 AND stage = 'projector'`, genC.id); err != nil {
				return err
			}
			scopeC := h.scope
			scopeC.PreviousGenerationExists = true
			scopeC.ActiveGenerationID = overlayGenA.id
			workC = projector.ScopeGenerationWork{Scope: scopeC, Generation: h.generation(genC.id), AttemptCount: 1}
			factsC, err := postgres.NewFactStore(h.pgDB).LoadFacts(bg, workC)
			if err != nil {
				return err
			}
			_, err = rtC.Project(bg, scopeC, workC.Generation, factsC)
			return err
		},
		after: func(gctx context.Context) error {
			return queueC.Ack(context.WithoutCancel(gctx), workC, runtime.Result{})
		},
	}
	rt.CanonicalWriter = gate
	svc.Runner = rt
	h.run(ctx, svc)

	if err := h.commit(ctx, overlayGenA); err != nil {
		t.FailNow()
	}
	h.waitGeneration(ctx, overlayGenA.id, "active")
	h.assertFiles(ctx, "baseline G_A", []string{"keep.go", "w.go"})
	if err := h.commit(ctx, overlayGenB); err != nil {
		t.FailNow()
	}
	h.waitGeneration(ctx, genC.id, "active")
	h.waitGeneration(ctx, overlayGenB.id, "superseded")
	h.logGenerations(ctx)
	h.logState(ctx, "after G_B refused at Ack (active G_C @C)")
	var class string
	if err := h.sqlDB.QueryRowContext(ctx, `SELECT COALESCE(failure_class, '') FROM fact_work_items
WHERE generation_id = $1 AND stage = 'projector'`, overlayGenB.id).Scan(&class); err != nil {
		t.Fatalf("read G_B failure_class: %v", err)
	}
	if class != projector.DeltaBaselineMismatchAfterProjectionClass {
		t.Fatalf("G_B failure_class = %q, want %q (the Ack-phase refusal)", class, projector.DeltaBaselineMismatchAfterProjectionClass)
	}
	h.checkFiles(ctx, "after G_B refused at Ack (overlay still present; the breach)", []string{"keep.go", "x.go"})
	if got := h.uncoveredWriters(ctx); strings.Join(got, ",") != overlayGenB.id {
		t.Fatalf("UncoveredProjectionWriters() = %v, want [%s]: the collector must see the refused writer", got, overlayGenB.id)
	}

	// The full snapshot the collector's graph_dirty fallback emits.
	fullC := overlayGen{id: "gen-7389-full-c", commit: overlayCommitC, files: overlayTreeA}
	if err := h.commit(ctx, fullC); err != nil {
		t.FailNow()
	}
	h.waitGeneration(ctx, fullC.id, "active")
	h.logState(ctx, "after the graph_dirty full generation at C activated")
	h.assertFiles(ctx, "after full generation at C", []string{"keep.go", "w.go"})
	if got := h.uncoveredWriters(ctx); len(got) != 0 {
		t.Fatalf("UncoveredProjectionWriters() after the full = %v, want none (covered)", got)
	}
}
