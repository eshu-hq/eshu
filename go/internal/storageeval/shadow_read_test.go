// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"strings"
	"testing"
	"time"
)

func TestValidateShadowReadComparisonAcceptsMatchingBoundedEvidence(t *testing.T) {
	comparison := validShadowReadComparison()

	if err := ValidateShadowReadComparison(comparison); err != nil {
		t.Fatalf("ValidateShadowReadComparison() error = %v, want nil", err)
	}
}

func TestValidateShadowReadComparisonRejectsInvalidEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ShadowReadComparison)
		want   string
	}{
		{
			name: "unsupported read model",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.ReadModel = ReadModel("dashboard_payload")
			},
			want: "unsupported read model",
		},
		{
			name: "missing scope",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Scope.ID = ""
			},
			want: "scope id is required",
		},
		{
			name: "unsupported scope kind",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Scope.Kind = ScopeKind("organization")
			},
			want: "unsupported scope kind",
		},
		{
			name: "unbounded comparison",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Limit = 0
			},
			want: "limit must be positive",
		},
		{
			name: "missing baseline truth label",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Baseline.TruthLabel = TruthLabel{}
			},
			want: "baseline truth label is required",
		},
		{
			name: "missing shadow truth label",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Shadow.TruthLabel = TruthLabel{}
			},
			want: "shadow truth label is required",
		},
		{
			name: "missing shadow result",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Shadow.Digest = ""
			},
			want: "shadow result digest is required",
		},
		{
			name: "stale shadow result",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Shadow.Freshness.State = FreshnessStale
			},
			want: "shadow freshness must be fresh",
		},
		{
			name: "divergent digest",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Shadow.Digest = "sha256:different"
			},
			want: "shadow digest differs from baseline",
		},
		{
			name: "truth downgrade",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Shadow.TruthLabel.Level = TruthLevelFallback
			},
			want: "shadow truth level fallback is not accepted",
		},
		{
			name: "truth mismatch",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Shadow.TruthLabel.Basis = TruthBasisReadModel
			},
			want: "truth labels must match",
		},
		{
			name: "truncated shadow result",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Shadow.Truncated = true
			},
			want: "shadow result must not be truncated",
		},
		{
			name: "unsupported capability",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Shadow.Supported = false
			},
			want: "shadow capability is unsupported",
		},
		{
			name: "missing fallback behavior",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.FallbackBehavior = ""
			},
			want: "fallback behavior is required",
		},
		{
			name: "non-match verdict",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Verdict = VerdictDivergent
				comparison.FailureClass = FailureClassDivergent
			},
			want: "verdict must be match",
		},
		{
			name: "missing failure class",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.FailureClass = ""
			},
			want: "failure class is required",
		},
		{
			name: "negative shadow latency",
			mutate: func(comparison *ShadowReadComparison) {
				comparison.Shadow.Latency = -time.Millisecond
			},
			want: "shadow latency must not be negative",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			comparison := validShadowReadComparison()
			test.mutate(&comparison)

			err := ValidateShadowReadComparison(comparison)
			if err == nil {
				t.Fatalf("ValidateShadowReadComparison() error = nil, want %q", test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateShadowReadComparison() error = %q, want substring %q", err.Error(), test.want)
			}
		})
	}
}

func validShadowReadComparison() ShadowReadComparison {
	observedAt := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	baseline := ReadResult{
		Backend: BackendPostgresReadModel,
		Digest:  "sha256:content-read-model-match",
		TruthLabel: TruthLabel{
			Level: TruthLevelDerived,
			Basis: TruthBasisContentIndex,
		},
		Freshness: Freshness{
			State:      FreshnessFresh,
			ObservedAt: observedAt,
		},
		Latency:   7 * time.Millisecond,
		Supported: true,
	}
	shadow := baseline
	shadow.Backend = BackendNornicDBShadowReadModel
	shadow.Latency = 9 * time.Millisecond

	return ShadowReadComparison{
		ReadModel:  ReadModelRepositoryFile,
		Capability: "storage.shadow_read.repository_file",
		Scope: Scope{
			Kind: ScopeRepository,
			ID:   "repo-123",
		},
		Limit:            100,
		Baseline:         baseline,
		Shadow:           shadow,
		Verdict:          VerdictMatch,
		FallbackBehavior: FallbackKeepPostgres,
		FailureClass:     FailureClassNone,
	}
}
