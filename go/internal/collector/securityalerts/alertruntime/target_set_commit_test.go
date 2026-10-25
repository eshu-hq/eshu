// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// decideAgainstActive runs the ingestion store's commit decision for next
// against prior as the scope's active generation.
func decideAgainstActive(next, prior scope.ScopeGeneration) backfill.UnchangedGenerationAction {
	if !backfill.GenerationNeedsDuplicateCheck(next) {
		return backfill.UnchangedActionStore
	}
	action, _ := backfill.DecideUnchangedGeneration(next, backfill.UnchangedGenerationCandidate{
		GenerationID:  prior.GenerationID,
		FreshnessHint: prior.FreshnessHint,
		SourceCommit:  prior.SourceCommitSHA,
		Status:        scope.GenerationStatusActive,
	}, true)
	return action
}

// TestTargetSetDrivesTheUnchangedGenerationDecision feeds collected
// generations through backfill.DecideUnchangedGeneration, the decision the
// ingestion store makes before it stores a snapshot (#8838). An unchanged
// allowlist still skips every cycle, so the target set adds no churn. An
// allowlist entry for a repository with no alerts, a removed entry, and a
// reused repository scope_id each store a new generation that records the new
// set.
func TestTargetSetDrivesTheUnchangedGenerationDecision(t *testing.T) {
	t.Parallel()

	before := collectOrgTarget(t, []string{"example-org/alpha-repo", "example-org/beta-repo"}, "g1")
	t.Run("unchanged allowlist skips every cycle", func(t *testing.T) {
		t.Parallel()
		again := collectOrgTarget(t, []string{"example-org/beta-repo", "example-org/alpha-repo"}, "g2")
		if got := decideAgainstActive(again.Generation, before.Generation); got != backfill.UnchangedActionSkip {
			t.Fatalf("decision = %v, want %v: an unchanged target set must not commit every cycle", got, backfill.UnchangedActionSkip)
		}
	})
	t.Run("allowlist adds a repository with no alerts", func(t *testing.T) {
		t.Parallel()
		added := collectOrgTarget(t, []string{"example-org/alpha-repo", "example-org/beta-repo", "example-org/quiet-repo"}, "g2")
		if got := decideAgainstActive(added.Generation, before.Generation); got != backfill.UnchangedActionStore {
			t.Fatalf("decision = %v, want %v: the added repository would never be recorded", got, backfill.UnchangedActionStore)
		}
	})
	t.Run("allowlist removes a repository with no alerts", func(t *testing.T) {
		t.Parallel()
		removed := collectOrgTarget(t, []string{"example-org/alpha-repo"}, "g2")
		if got := decideAgainstActive(removed.Generation, before.Generation); got != backfill.UnchangedActionStore {
			t.Fatalf("decision = %v, want %v after an allowlist removal", got, backfill.UnchangedActionStore)
		}
	})
}

// TestTargetSetMovesTheHintOnceAtUpgrade pins the one extra generation per
// target after the upgrade: the digest of a generation committed before the
// target set existed differs from the digest of the same provider answer with
// the set, for org and repository targets alike, so the first claim after the
// upgrade stores once and later claims skip.
func TestTargetSetMovesTheHintOnceAtUpgrade(t *testing.T) {
	t.Parallel()

	cases := map[string]securityAlertFreshnessInput{
		"organization": {
			alerts:       []securityalerts.GitHubDependabotAlert{orgAlert(7, "example-org/alpha-repo")},
			pagesFetched: 1,
			orgReadState: true,
		},
		"repository": {},
	}
	for name, legacy := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			upgraded := legacy
			upgraded.targetSetHash = testTargetSetHash("example-org/alpha-repo")
			prior := scope.ScopeGeneration{GenerationID: "g-legacy", FreshnessHint: securityAlertFreshnessHint(legacy)}
			first := scope.ScopeGeneration{GenerationID: "g-upgrade", FreshnessHint: securityAlertFreshnessHint(upgraded)}
			second := scope.ScopeGeneration{GenerationID: "g-next", FreshnessHint: securityAlertFreshnessHint(upgraded)}
			if got := decideAgainstActive(first, prior); got != backfill.UnchangedActionStore {
				t.Fatalf("first claim after the upgrade = %v, want %v", got, backfill.UnchangedActionStore)
			}
			if got := decideAgainstActive(second, first); got != backfill.UnchangedActionSkip {
				t.Fatalf("second claim after the upgrade = %v, want %v", got, backfill.UnchangedActionSkip)
			}
		})
	}
}

// TestRepositoryTargetScopeIDReuseStoresANewGeneration covers a repository
// target whose scope_id is reused for another repository with zero alerts on
// both: only the target set tells the two snapshots apart.
func TestRepositoryTargetScopeIDReuseStoresANewGeneration(t *testing.T) {
	t.Parallel()

	original := securityAlertFreshnessInput{targetSetHash: testTargetSetHash("example-org/example-repo")}
	reused := securityAlertFreshnessInput{targetSetHash: testTargetSetHash("example-org/other-repo")}
	prior := scope.ScopeGeneration{GenerationID: "g1", FreshnessHint: securityAlertFreshnessHint(original)}
	next := scope.ScopeGeneration{GenerationID: "g2", FreshnessHint: securityAlertFreshnessHint(reused)}
	if got := decideAgainstActive(next, prior); got != backfill.UnchangedActionStore {
		t.Fatalf("decision = %v, want %v for a reused scope_id", got, backfill.UnchangedActionStore)
	}
}
