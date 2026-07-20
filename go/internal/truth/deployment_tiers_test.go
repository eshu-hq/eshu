// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package truth

import (
	"slices"
	"testing"
)

func TestDeploymentTruthTierConstantsExhaustive(t *testing.T) {
	t.Parallel()

	all := AllDeploymentTruthTiers()
	if len(all) != 4 {
		t.Fatalf("AllDeploymentTruthTiers() len = %d, want 4", len(all))
	}
	seen := make(map[DeploymentTruthTier]bool)
	for _, tier := range all {
		if seen[tier] {
			t.Fatalf("AllDeploymentTruthTiers() duplicates tier %q", tier)
		}
		seen[tier] = true
	}
	for _, tier := range all {
		if tier != TierRuntimeConfirmed &&
			tier != TierProvenanceCIDeclared &&
			tier != TierDeclaredRef &&
			tier != TierConfigOnly {
			t.Fatalf("AllDeploymentTruthTiers() returned unknown tier %q", tier)
		}
	}
}

func TestParseDeploymentTruthTierRoundTrip(t *testing.T) {
	t.Parallel()

	for _, tier := range AllDeploymentTruthTiers() {
		parsed, err := ParseDeploymentTruthTier(string(tier))
		if err != nil {
			t.Fatalf("ParseDeploymentTruthTier(%q) error = %v", tier, err)
		}
		if parsed != tier {
			t.Fatalf("ParseDeploymentTruthTier(%q) = %q, want %q", tier, parsed, tier)
		}
	}
}

func TestParseDeploymentTruthTierRejectsUnknown(t *testing.T) {
	t.Parallel()

	tests := []string{"", "unknown", "runtime_confirmed_typo", "RUNTIME_CONFIRMED"}
	for _, input := range tests {
		if _, err := ParseDeploymentTruthTier(input); err == nil {
			t.Fatalf("ParseDeploymentTruthTier(%q) error = nil, want non-nil", input)
		}
	}
}

func TestDeploymentTruthTierRankMonotonic(t *testing.T) {
	t.Parallel()

	all := AllDeploymentTruthTiers()
	ranks := make([]int, len(all))
	for i, tier := range all {
		ranks[i] = tier.Rank()
	}
	// Higher rank = stronger evidence. Verify strictly descending from first to last.
	for i := 1; i < len(ranks); i++ {
		if ranks[i] >= ranks[i-1] {
			t.Fatalf("rank[%d] = %d, rank[%d] = %d; want strictly descending", i-1, ranks[i-1], i, ranks[i])
		}
	}
}

func TestDeploymentTruthTierCompare(t *testing.T) {
	t.Parallel()

	all := AllDeploymentTruthTiers()
	for i := 0; i < len(all); i++ {
		for j := 0; j < len(all); j++ {
			cmp := all[i].Compare(all[j])
			switch {
			case i < j:
				if cmp != 1 {
					t.Fatalf("Compare(%q, %q) = %d, want 1 (stronger)", all[i], all[j], cmp)
				}
			case i > j:
				if cmp != -1 {
					t.Fatalf("Compare(%q, %q) = %d, want -1 (weaker)", all[i], all[j], cmp)
				}
			default:
				if cmp != 0 {
					t.Fatalf("Compare(%q, %q) = %d, want 0 (equal)", all[i], all[j], cmp)
				}
			}
		}
	}
}

func TestAllDeploymentTruthTiersOrder(t *testing.T) {
	t.Parallel()

	all := AllDeploymentTruthTiers()
	expected := []DeploymentTruthTier{
		TierRuntimeConfirmed,
		TierProvenanceCIDeclared,
		TierDeclaredRef,
		TierConfigOnly,
	}
	if !slices.Equal(all, expected) {
		t.Fatalf("AllDeploymentTruthTiers() = %v, want %v", all, expected)
	}
}

func TestParseDeploymentTruthTierWhitespace(t *testing.T) {
	t.Parallel()

	parsed, err := ParseDeploymentTruthTier("  runtime_confirmed  ")
	if err != nil {
		t.Fatalf("ParseDeploymentTruthTier() error = %v", err)
	}
	if parsed != TierRuntimeConfirmed {
		t.Fatalf("ParseDeploymentTruthTier() = %q, want %q", parsed, TierRuntimeConfirmed)
	}
}

func TestClassifyDeploymentTruthTierLiveEvidenceWins(t *testing.T) {
	t.Parallel()

	// Live evidence must produce runtime_confirmed even when config signals are present.
	tier := ClassifyDeploymentTruthTier(true, true, true, true)
	if tier != TierRuntimeConfirmed {
		t.Fatalf("ClassifyDeploymentTruthTier(live=true,...) = %q, want %q", tier, TierRuntimeConfirmed)
	}
}

func TestClassifyDeploymentTruthTierInstancesAreConfigOnly(t *testing.T) {
	t.Parallel()

	// Config-materialized instances are config_only despite the legacy
	// "materialized_runtime_instances" reason name.
	tests := []struct {
		name              string
		hasInstances      bool
		hasDeploymentSrcs bool
		hasConfigEnvs     bool
	}{
		{"instances only", true, false, false},
		{"deployment sources only", false, true, false},
		{"config environments only", false, false, true},
		{"instances + deployment sources", true, true, false},
		{"instances + config environments", true, false, true},
		{"deployment sources + config environments", false, true, true},
		{"all three config signals", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier := ClassifyDeploymentTruthTier(false, tt.hasInstances, tt.hasDeploymentSrcs, tt.hasConfigEnvs)
			if tier != TierConfigOnly {
				t.Fatalf("ClassifyDeploymentTruthTier(live=false, instances=%v, sources=%v, envs=%v) = %q, want %q",
					tt.hasInstances, tt.hasDeploymentSrcs, tt.hasConfigEnvs, tier, TierConfigOnly)
			}
		})
	}
}

func TestClassifyDeploymentTruthTierNoEvidence(t *testing.T) {
	t.Parallel()

	tier := ClassifyDeploymentTruthTier(false, false, false, false)
	if tier != "" {
		t.Fatalf("ClassifyDeploymentTruthTier(no evidence) = %q, want empty", tier)
	}

	// Also test with live=false, no config signals
	tier = ClassifyDeploymentTruthTier(false, false, false, false)
	if tier != "" {
		t.Fatalf("ClassifyDeploymentTruthTier(no evidence) = %q, want empty", tier)
	}
}
