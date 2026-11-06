// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachability

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
)

const (
	testEvidenceCommit = "1111111111111111111111111111111111111111"
	testActiveCommit   = "2222222222222222222222222222222222222222"
	testRepositoryID   = "repository:r_1a2b3c4d"
	testFenceInterval  = 24 * time.Hour
)

// staleRepository returns a repository that meets every fence condition at
// fenceObservedAt with one rotation slot, so a table row only has to name the one
// field it breaks.
func staleRepository() scannercomponent.GoModuleRepository {
	return scannercomponent.GoModuleRepository{
		RepositoryID:         testRepositoryID,
		SourceCommitSHA:      testActiveCommit,
		HasGoEvidence:        true,
		EvidenceGenerationID: "gen-x",
		EvidenceCommitSHA:    testEvidenceCommit,
		EvidenceLeftAt:       fenceObservedAt.Add(-2*testFenceInterval - time.Second),
	}
}

var fenceObservedAt = time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)

// TestStaleEvidenceFencesOnlySkewPastTwoRotations holds Amendment 1: evidence, skew,
// and skew age together. The age is how long the repository has been off the evidence
// commit, never how old the evidence is.
func TestStaleEvidenceFencesOnlySkewPastTwoRotations(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		mutate       func(*scannercomponent.GoModuleRepository)
		wantFence    bool
		wantNoCommit int
		wantPruned   int
	}{
		{name: "skew past the bound", wantFence: true},
		{
			name: "skew exactly at the bound does not fence",
			mutate: func(r *scannercomponent.GoModuleRepository) {
				r.EvidenceLeftAt = fenceObservedAt.Add(-2 * testFenceInterval)
			},
		},
		{
			name: "a push one hour ago on old evidence does not fence",
			mutate: func(r *scannercomponent.GoModuleRepository) {
				r.EvidenceLeftAt = fenceObservedAt.Add(-time.Hour)
			},
		},
		{
			name: "no skew does not fence, whatever the history says",
			mutate: func(r *scannercomponent.GoModuleRepository) {
				r.SourceCommitSHA = testEvidenceCommit
				r.EvidenceLeftAt = fenceObservedAt.Add(-365 * 24 * time.Hour)
			},
		},
		{
			name: "pruned history fences and is counted",
			mutate: func(r *scannercomponent.GoModuleRepository) {
				r.EvidenceLeftAt = time.Time{}
			},
			wantFence:  true,
			wantPruned: 1,
		},
		{
			name: "no evidence commit is counted, not fenced",
			mutate: func(r *scannercomponent.GoModuleRepository) {
				r.EvidenceCommitSHA = ""
			},
			wantNoCommit: 1,
		},
		{
			name: "warning-only generation is not a candidate",
			mutate: func(r *scannercomponent.GoModuleRepository) {
				r.HasGoEvidence = false
			},
		},
		{
			name: "warning-only generation with no commit is not counted",
			mutate: func(r *scannercomponent.GoModuleRepository) {
				r.HasGoEvidence = false
				r.EvidenceCommitSHA = ""
			},
		},
		{
			name: "a repository with no commit of its own proves no skew",
			mutate: func(r *scannercomponent.GoModuleRepository) {
				r.SourceCommitSHA = ""
			},
		},
		{
			name: "no generation names nothing to fence",
			mutate: func(r *scannercomponent.GoModuleRepository) {
				r.EvidenceGenerationID = ""
			},
		},
		{
			name: "a non-canonical repository id has no derived scope",
			mutate: func(r *scannercomponent.GoModuleRepository) {
				r.RepositoryID = "/srv/local"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repository := staleRepository()
			if tc.mutate != nil {
				tc.mutate(&repository)
			}
			decision := StaleEvidence([]scannercomponent.GoModuleRepository{repository}, fenceObservedAt, testFenceInterval, 1)
			if decision.SkippedNoCommit != tc.wantNoCommit || decision.HistoryPruned != tc.wantPruned {
				t.Fatalf("decision = %+v, want no-commit %d and pruned %d", decision, tc.wantNoCommit, tc.wantPruned)
			}
			if !tc.wantFence {
				if len(decision.Fences) != 0 {
					t.Fatalf("Fences = %+v, want none", decision.Fences)
				}
				return
			}
			want := scannercomponent.StaleEvidenceFence{
				ScopeID:      scannercomponent.GoReachabilityScopePrefix + testRepositoryID,
				GenerationID: "gen-x",
			}
			if len(decision.Fences) != 1 || decision.Fences[0] != want {
				t.Fatalf("Fences = %+v, want [%+v]", decision.Fences, want)
			}
		})
	}
}

// TestStaleEvidenceBoundIsTwoRotationPeriods proves the bound scales with both the
// interval and the slot count: one repository is visited once per slotCount x
// interval, so two intervals would fence a healthy repository on a large fleet.
func TestStaleEvidenceBoundIsTwoRotationPeriods(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		interval  time.Duration
		slotCount int
		skew      time.Duration
		wantFence bool
	}{
		{name: "one slot, past two intervals", interval: time.Hour, slotCount: 1, skew: 3 * time.Hour, wantFence: true},
		{name: "one slot, wider interval", interval: 2 * time.Hour, slotCount: 1, skew: 3 * time.Hour},
		{name: "1000 candidates, past two intervals but inside two rotations", interval: 24 * time.Hour, slotCount: 4, skew: 7 * 24 * time.Hour},
		{name: "1000 candidates, past two rotations", interval: 24 * time.Hour, slotCount: 4, skew: 8*24*time.Hour + time.Second, wantFence: true},
		{name: "a slot count that is not positive counts as one", interval: time.Hour, slotCount: 0, skew: 3 * time.Hour, wantFence: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repository := staleRepository()
			repository.EvidenceLeftAt = fenceObservedAt.Add(-tc.skew)
			got := StaleEvidence([]scannercomponent.GoModuleRepository{repository}, fenceObservedAt, tc.interval, tc.slotCount)
			if (len(got.Fences) == 1) != tc.wantFence {
				t.Fatalf("Fences = %+v, want fence %v", got.Fences, tc.wantFence)
			}
		})
	}
}
