// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"testing"
	"time"
)

// livenessProgressWindowDefault mirrors the documented ProgressWindow default.
// The fixtures below use policies without an explicit ProgressWindow so they
// also pin that Normalize fills the default.
const livenessProgressWindowDefault = 10 * time.Minute

// TestGenerationLivenessProgressWindow proves the #7265 contract against a real
// Postgres: an aged generation with actionable outstanding shared intents is
// only wedged when none of its actionable outstanding intents sits in a
// projection_domain that completed any intent inside the progress window.
// Otherwise it is draining. It is counted in the draining gauge bucket and
// never re-driven, and it spends no liveness_recovery_attempts budget.
func TestGenerationLivenessProgressWindow(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_LIVENESS_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_LIVENESS_PROOF_DSN to run the generation liveness progress-window proof")
	}
	policy := GenerationLivenessPolicy{
		ActivationDeadline: 30 * time.Minute,
		MaxRecoverAttempts: 5,
		BatchLimit:         100,
	}
	ctx := context.Background()

	// Single-generation cases: the latest completion in the pending intent's
	// domain decides draining (not re-driven) versus wedged (re-driven).
	cases := []struct {
		name        string
		seed        func(f *progressFixture)
		wantRedrive bool
	}{
		{
			name: "domain completed a minute ago is draining",
			seed: func(f *progressFixture) {
				f.pending("g", "code_calls", "run-g")
				f.completed("code_calls", "run-other", time.Minute)
			},
		},
		{
			name: "domain completed window-1s ago is draining",
			seed: func(f *progressFixture) {
				f.pending("g", "code_calls", "run-g")
				f.completed("code_calls", "run-other", livenessProgressWindowDefault-time.Second)
			},
		},
		{
			name: "domain completed window+1s ago is wedged",
			seed: func(f *progressFixture) {
				f.pending("g", "code_calls", "run-g")
				f.completed("code_calls", "run-other", livenessProgressWindowDefault+time.Second)
			},
			wantRedrive: true,
		},
		{
			name: "mixed domains with one still progressing is draining",
			seed: func(f *progressFixture) {
				f.pending("g", "sql_relationships", "run-g")
				f.pending("g", "code_calls", "run-g")
				f.completed("sql_relationships", "run-other", 2*time.Hour)
				f.completed("code_calls", "run-other", time.Minute)
			},
		},
		{
			name: "quiet code_calls domain while exact repo_dependency flows is wedged",
			seed: func(f *progressFixture) {
				f.pending("g", "code_calls", "run-g")
				f.completed("code_calls", "run-other", 2*time.Hour)
				f.completed("repo_dependency", "repo_dependency:scope-other", time.Minute)
				f.completed("repo_dependency", "repo_dependency", time.Minute)
			},
			wantRedrive: true,
		},
		{
			name: "lookalike repo_dependency intent while exact completions flow is draining",
			seed: func(f *progressFixture) {
				f.pending("g", "repo_dependency", "code_import_repo_dependency:scope-g")
				f.completed("repo_dependency", "repo_dependency:scope-other", time.Minute)
			},
		},
		{
			// The exact repo_dependency family is never actionable, so its
			// outstanding intent cannot make the generation draining even
			// while the repo_dependency queue is completing work in-window.
			name: "quiet intent plus exact repo_dependency intent in a flowing queue is wedged",
			seed: func(f *progressFixture) {
				f.pending("g", "sql_relationships", "run-g")
				f.pending("g", "repo_dependency", "repo_dependency:scope-g")
				f.completed("sql_relationships", "run-other", 2*time.Hour)
				f.completed("repo_dependency", "repo_dependency:scope-other", time.Minute)
			},
			wantRedrive: true,
		},
		{
			// Only outstanding intents block: the generation's own completed
			// code_calls intent makes code_calls progressing but is not
			// outstanding work, so the quiet intent still wedges it.
			name: "quiet intent plus own completed intent in a flowing queue is wedged",
			seed: func(f *progressFixture) {
				f.pending("g", "sql_relationships", "run-g")
				f.completed("sql_relationships", "run-other", 2*time.Hour)
				f.ownCompleted("g", "code_calls", "run-g", time.Minute)
			},
			wantRedrive: true,
		},
		{
			name: "domain with no completion at all is wedged",
			seed: func(f *progressFixture) {
				f.pending("g", "code_calls", "run-g")
			},
			wantRedrive: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openLivenessProofDB(t, dsn)
			f := newProgressFixture()
			f.generation("g", 2*time.Hour)
			tc.seed(f)
			f.provision(t, db)
			store := NewGenerationLivenessStore(SQLDB{DB: db})

			counts, err := store.CountActiveGenerationsByAge(ctx, policy, f.anchor)
			if err != nil {
				t.Fatalf("CountActiveGenerationsByAge() error = %v", err)
			}
			wantStuck, wantDraining := int64(0), int64(1)
			if tc.wantRedrive {
				wantStuck, wantDraining = 1, 0
			}
			if counts["stuck"] != wantStuck || counts["draining"] != wantDraining {
				t.Fatalf("counts = %v, want stuck=%d draining=%d", counts, wantStuck, wantDraining)
			}

			result, err := store.RecoverWedgedGenerations(ctx, policy, f.anchor)
			if err != nil {
				t.Fatalf("RecoverWedgedGenerations() error = %v", err)
			}
			attempts, status := projectorRecoveryState(t, db, "g")
			if tc.wantRedrive {
				if result.Recovered != 1 || attempts.Int64 != 1 || status != "pending" {
					t.Fatalf("wedged: Recovered=%d attempts=%v status=%q, want 1/1/pending", result.Recovered, attempts, status)
				}
				return
			}
			if result.Recovered != 0 {
				t.Fatalf("draining: Recovered = %d, want 0", result.Recovered)
			}
			if attempts.Valid || status != "succeeded" {
				t.Fatalf("draining: attempts=%v status=%q, want budget untouched and succeeded", attempts, status)
			}
		})
	}

	// Transition: skipped while draining, re-driven exactly once after the
	// domain goes quiet past the window, then a no-op while that re-drive is
	// pending.
	t.Run("draining then quiet re-drives exactly once", func(t *testing.T) {
		db := openLivenessProofDB(t, dsn)
		f := newProgressFixture()
		f.generation("g", 2*time.Hour)
		f.pending("g", "code_calls", "run-g")
		f.completed("code_calls", "run-other", time.Minute)
		f.provision(t, db)
		store := NewGenerationLivenessStore(SQLDB{DB: db})

		first, err := store.RecoverWedgedGenerations(ctx, policy, f.anchor)
		if err != nil {
			t.Fatalf("first sweep: %v", err)
		}
		if first.Recovered != 0 {
			t.Fatalf("first sweep Recovered = %d, want 0 (draining)", first.Recovered)
		}
		later := f.anchor.Add(livenessProgressWindowDefault + time.Minute)
		second, err := store.RecoverWedgedGenerations(ctx, policy, later)
		if err != nil {
			t.Fatalf("second sweep: %v", err)
		}
		if second.Recovered != 1 {
			t.Fatalf("second sweep Recovered = %d, want 1 (domain quiet past window)", second.Recovered)
		}
		third, err := store.RecoverWedgedGenerations(ctx, policy, later.Add(time.Minute))
		if err != nil {
			t.Fatalf("third sweep: %v", err)
		}
		if third.Recovered != 0 {
			t.Fatalf("third sweep Recovered = %d, want 0 (re-drive pending)", third.Recovered)
		}
		if attempts, status := projectorRecoveryState(t, db, "g"); attempts.Int64 != 1 || status != "pending" {
			t.Fatalf("after transition attempts=%v status=%q, want 1/pending", attempts, status)
		}
	})

	// Gauge: one generation in each closed bucket.
	t.Run("gauge counts all four buckets", func(t *testing.T) {
		db := openLivenessProofDB(t, dsn)
		f := newProgressFixture()
		f.generation("fresh", 5*time.Minute)
		f.generation("aging", 20*time.Minute)
		f.generation("draining", 2*time.Hour)
		f.pending("draining", "code_calls", "run-draining")
		f.completed("code_calls", "run-other", time.Minute)
		f.generation("stuck", 2*time.Hour)
		f.pending("stuck", "sql_relationships", "run-stuck")
		f.completed("sql_relationships", "run-other", 2*time.Hour)
		f.provision(t, db)
		store := NewGenerationLivenessStore(SQLDB{DB: db})

		counts, err := store.CountActiveGenerationsByAge(ctx, policy, f.anchor)
		if err != nil {
			t.Fatalf("CountActiveGenerationsByAge() error = %v", err)
		}
		for _, bucket := range []string{"fresh", "aging", "draining", "stuck"} {
			if counts[bucket] != 1 {
				t.Fatalf("counts = %v, want 1 in every bucket", counts)
			}
		}
	})
}
