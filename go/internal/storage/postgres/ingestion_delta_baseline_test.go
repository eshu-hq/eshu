// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func deltaBaselineInputScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID: "scope-7319", SourceSystem: "git", ScopeKind: scope.KindRepository,
		CollectorKind: scope.CollectorGit, PartitionKey: "repo-7319",
	}
}

func deltaBaselineInputGeneration(isDelta bool, baseline string) scope.ScopeGeneration {
	at := time.Date(2026, time.September, 28, 1, 0, 0, 0, time.UTC)
	return scope.ScopeGeneration{
		GenerationID: "gen-d", ScopeID: "scope-7319", ObservedAt: at, IngestedAt: at,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		SourceCommitSHA: "D", IsDelta: isDelta, DeltaBaselineCommitSHA: baseline,
	}
}

// TestValidateGenerationInputRequiresDeltaBaseline pins the one write choke
// point (#7319): every writer of a delta generation must record the commit it
// diffed from, and only a delta may carry one.
func TestValidateGenerationInputRequiresDeltaBaseline(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		isDelta  bool
		baseline string
		wantErr  string
	}{
		{name: "delta with baseline", isDelta: true, baseline: "A"},
		{name: "full without baseline"},
		{name: "delta without baseline", isDelta: true, wantErr: "delta_baseline_commit_sha"},
		{name: "delta with blank baseline", isDelta: true, baseline: "  ", wantErr: "delta_baseline_commit_sha"},
		{name: "full with baseline", baseline: "A", wantErr: "delta_baseline_commit_sha"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateGenerationInput(deltaBaselineInputScope(), deltaBaselineInputGeneration(tc.isDelta, tc.baseline))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateGenerationInput() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateGenerationInput() = %v, want error naming %q", err, tc.wantErr)
			}
		})
	}
}

// TestUpsertScopeGenerationWritesDeltaBaseline proves the baseline reaches the
// row on insert and on the pending-row conflict update, and a full generation
// writes NULL.
func TestUpsertScopeGenerationWritesDeltaBaseline(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"delta_baseline_commit_sha", "delta_baseline_commit_sha = EXCLUDED.delta_baseline_commit_sha"} {
		if !strings.Contains(upsertScopeGenerationQuery, want) {
			t.Fatalf("upsertScopeGenerationQuery missing %q:\n%s", want, upsertScopeGenerationQuery)
		}
	}
	for _, tc := range []struct {
		name     string
		isDelta  bool
		baseline string
		want     any
	}{
		{name: "delta", isDelta: true, baseline: "A", want: "A"},
		{name: "full", want: nil},
	} {
		fake := &recordingExecQueryer{result: projectorRowsAffectedResult{rowsAffected: 1}}
		if err := upsertScopeGeneration(context.Background(), fake, deltaBaselineInputGeneration(tc.isDelta, tc.baseline)); err != nil {
			t.Fatalf("%s: upsertScopeGeneration() = %v", tc.name, err)
		}
		args := fake.execs[0].args
		if got := args[len(args)-1]; got != tc.want {
			t.Fatalf("%s: baseline arg = %#v, want %#v", tc.name, got, tc.want)
		}
	}
}
