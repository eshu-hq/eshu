// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"strings"
	"testing"
	"time"
)

func TestValidateFactWriteComparisonAcceptsDuplicateReplayEvidence(t *testing.T) {
	comparison := validFactWriteComparison()
	comparison.ReplayCount = 2

	if err := ValidateFactWriteComparison(comparison); err != nil {
		t.Fatalf("ValidateFactWriteComparison() error = %v, want nil", err)
	}
}

func TestValidateFactWriteComparisonAcceptsMatchingTombstoneEvidence(t *testing.T) {
	comparison := validFactWriteComparison()
	comparison.Baseline.RecordState = FactRecordTombstone
	comparison.Shadow.RecordState = FactRecordTombstone
	comparison.Baseline.Digest = "sha256:tombstone-match"
	comparison.Shadow.Digest = "sha256:tombstone-match"

	if err := ValidateFactWriteComparison(comparison); err != nil {
		t.Fatalf("ValidateFactWriteComparison() error = %v, want nil", err)
	}
}

func TestValidateFactWriteComparisonRejectsInvalidEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*FactWriteComparison)
		want   string
	}{
		{
			name: "missing fact family",
			mutate: func(comparison *FactWriteComparison) {
				comparison.FactFamily = ""
			},
			want: "fact family is required",
		},
		{
			name: "missing idempotency key",
			mutate: func(comparison *FactWriteComparison) {
				comparison.IdempotencyKey = ""
			},
			want: "idempotency key is required",
		},
		{
			name: "missing scope",
			mutate: func(comparison *FactWriteComparison) {
				comparison.ScopeID = ""
			},
			want: "scope id is required",
		},
		{
			name: "missing generation",
			mutate: func(comparison *FactWriteComparison) {
				comparison.GenerationID = ""
			},
			want: "generation id is required",
		},
		{
			name: "unbounded fact scan",
			mutate: func(comparison *FactWriteComparison) {
				comparison.Limit = 0
			},
			want: "limit must be positive",
		},
		{
			name: "missing fallback behavior",
			mutate: func(comparison *FactWriteComparison) {
				comparison.FallbackBehavior = ""
			},
			want: "fallback behavior is required",
		},
		{
			name: "missing rollback behavior",
			mutate: func(comparison *FactWriteComparison) {
				comparison.RollbackBehavior = ""
			},
			want: "rollback behavior is required",
		},
		{
			name: "missing shadow write",
			mutate: func(comparison *FactWriteComparison) {
				comparison.Shadow.Digest = ""
			},
			want: "shadow write digest is required",
		},
		{
			name: "stale generation",
			mutate: func(comparison *FactWriteComparison) {
				comparison.Shadow.GenerationState = FactGenerationStale
			},
			want: "shadow generation state must be active",
		},
		{
			name: "superseded shadow",
			mutate: func(comparison *FactWriteComparison) {
				comparison.Shadow.SupersessionState = FactSupersessionSuperseded
			},
			want: "shadow supersession state must be current",
		},
		{
			name: "schema version mismatch",
			mutate: func(comparison *FactWriteComparison) {
				comparison.Shadow.SchemaVersion = "2.0.0"
			},
			want: "schema versions must match",
		},
		{
			name: "invalid schema version",
			mutate: func(comparison *FactWriteComparison) {
				comparison.Shadow.SchemaVersion = "not-semver"
			},
			want: "shadow schema_version must be semantic version",
		},
		{
			name: "divergent active generation",
			mutate: func(comparison *FactWriteComparison) {
				comparison.Shadow.GenerationID = "generation-older"
			},
			want: "shadow generation_id must match comparison generation_id",
		},
		{
			name: "tombstone mismatch",
			mutate: func(comparison *FactWriteComparison) {
				comparison.Baseline.RecordState = FactRecordTombstone
			},
			want: "record states must match",
		},
		{
			name: "unsupported shadow capability",
			mutate: func(comparison *FactWriteComparison) {
				comparison.Shadow.Supported = false
			},
			want: "shadow capability is unsupported",
		},
		{
			name: "divergent shadow digest",
			mutate: func(comparison *FactWriteComparison) {
				comparison.Shadow.Digest = "sha256:different"
			},
			want: "shadow write digest differs from baseline",
		},
		{
			name: "non-match verdict",
			mutate: func(comparison *FactWriteComparison) {
				comparison.Verdict = FactWriteVerdictSchemaMismatch
				comparison.FailureClass = FactWriteFailureSchemaMismatch
			},
			want: "verdict must be match",
		},
		{
			name: "missing failure class",
			mutate: func(comparison *FactWriteComparison) {
				comparison.FailureClass = ""
			},
			want: "failure class is required",
		},
		{
			name: "negative replay count",
			mutate: func(comparison *FactWriteComparison) {
				comparison.ReplayCount = -1
			},
			want: "replay count must not be negative",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			comparison := validFactWriteComparison()
			test.mutate(&comparison)

			err := ValidateFactWriteComparison(comparison)
			if err == nil {
				t.Fatalf("ValidateFactWriteComparison() error = nil, want %q", test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateFactWriteComparison() error = %q, want substring %q", err.Error(), test.want)
			}
		})
	}
}

func validFactWriteComparison() FactWriteComparison {
	observedAt := time.Date(2026, 6, 2, 13, 0, 0, 0, time.UTC)
	baseline := FactWriteResult{
		Backend:            BackendPostgresFactStore,
		FactID:             "fact-documentation-source-1",
		StableFactKey:      "documentation_source:docs/public/reference/truth-label-protocol.md",
		ScopeID:            "scope-docs",
		GenerationID:       "generation-docs-1",
		FactKind:           "documentation_source",
		SchemaVersion:      "1.0.0",
		RecordState:        FactRecordActive,
		GenerationState:    FactGenerationActive,
		SupersessionState:  FactSupersessionCurrent,
		Digest:             "sha256:fact-ledger-match",
		ObservedAt:         observedAt,
		Latency:            5 * time.Millisecond,
		Supported:          true,
		BoundedResultCount: 1,
	}
	shadow := baseline
	shadow.Backend = BackendNornicDBShadowFactStore
	shadow.Latency = 8 * time.Millisecond

	return FactWriteComparison{
		FactFamily:       FactFamily("documentation"),
		FactKind:         "documentation_source",
		ScopeID:          "scope-docs",
		GenerationID:     "generation-docs-1",
		IdempotencyKey:   "documentation_source:docs/public/reference/truth-label-protocol.md",
		Limit:            10,
		ReplayCount:      0,
		Baseline:         baseline,
		Shadow:           shadow,
		Verdict:          FactWriteVerdictMatch,
		FallbackBehavior: FallbackKeepPostgres,
		RollbackBehavior: RollbackDropShadowWrites,
		FailureClass:     FactWriteFailureNone,
	}
}
