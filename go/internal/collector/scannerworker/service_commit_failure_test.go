// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestServiceRecordsRetryableCommitFailure(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	store := &recordingClaimStore{}
	committer := &recordingClaimCommitter{
		err: errors.New("upsert fact batch (2 facts): sqlstate 23505"),
	}
	analyzer := &recordingAnalyzer{
		result: AnalyzerResult{
			Output: FactOutput{
				TargetCount: 1,
				ResultCount: 1,
				Facts:       []facts.Envelope{testScannerFact(t, item, claim, facts.SBOMDocumentFactKind)},
			},
			Usage: ResourceUsage{CPUSeconds: 0.25, PeakMemoryBytes: 128 << 20},
		},
	}
	var logs bytes.Buffer
	service := testScannerService(store, committer, analyzer)
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	if err := service.processClaimed(context.Background(), item, claim); err != nil {
		t.Fatalf("processClaimed() error = %v, want nil after retry record", err)
	}
	if !store.retryable {
		t.Fatal("retryable = false, want true")
	}
	if store.retryMutation.FailureClass != string(FailureClassCommitFailed) {
		t.Fatalf("FailureClass = %q, want %q", store.retryMutation.FailureClass, FailureClassCommitFailed)
	}
	if !strings.Contains(store.retryMutation.FailureMessage, `"failure_class":"commit_failed"`) {
		t.Fatalf("FailureMessage = %q, want commit_failed payload", store.retryMutation.FailureMessage)
	}
	output := logs.String()
	for _, want := range []string{
		`"msg":"scanner-worker commit failed"`,
		`"failure_class":"commit_failed"`,
		`"commit_failure_class":"fact_persistence"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("commit failure log missing %s:\n%s", want, output)
		}
	}
	if store.completed || store.terminal {
		t.Fatalf("completed=%v terminal=%v, want false,false", store.completed, store.terminal)
	}
}

func TestScopeAndGenerationForInputOmitsSelfParent(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	item.AcceptanceUnitID = item.ScopeID
	claim := testScannerClaim(item)
	input, err := NewClaimInput(item, claim, AnalyzerSBOMGeneration, testTargetScope(item), testResourceLimits())
	if err != nil {
		t.Fatalf("NewClaimInput() error = %v, want nil", err)
	}

	scopeValue, generation := scopeAndGenerationForInput(input, input.ObservedAt)
	if scopeValue.ParentScopeID != "" {
		t.Fatalf("ParentScopeID = %q, want blank when acceptance unit equals scope", scopeValue.ParentScopeID)
	}
	if err := generation.ValidateForScope(scopeValue); err != nil {
		t.Fatalf("generation.ValidateForScope() error = %v, want nil", err)
	}
}

func TestScopeAndGenerationForInputOmitsWhitespaceSelfParent(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	item.AcceptanceUnitID = " " + item.ScopeID + " "
	item.ScopeID = "\t" + item.ScopeID + "\n"
	claim := testScannerClaim(item)
	input, err := NewClaimInput(item, claim, AnalyzerSBOMGeneration, testTargetScope(item), testResourceLimits())
	if err != nil {
		t.Fatalf("NewClaimInput() error = %v, want nil", err)
	}

	scopeValue, _ := scopeAndGenerationForInput(input, input.ObservedAt)
	if scopeValue.ParentScopeID != "" {
		t.Fatalf("ParentScopeID = %q, want blank when acceptance unit equals trimmed scope", scopeValue.ParentScopeID)
	}
}

func TestScopeAndGenerationForInputStartsPendingForProjectorActivation(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	input, err := NewClaimInput(item, claim, AnalyzerSBOMGeneration, testTargetScope(item), testResourceLimits())
	if err != nil {
		t.Fatalf("NewClaimInput() error = %v, want nil", err)
	}

	_, generation := scopeAndGenerationForInput(input, input.ObservedAt)
	if generation.Status != scope.GenerationStatusPending {
		t.Fatalf("generation.Status = %q, want %q", generation.Status, scope.GenerationStatusPending)
	}
}

func TestClassifyCommitFailureKeepsPostgresDetailsBounded(t *testing.T) {
	t.Parallel()

	info := classifyCommitFailure(&pgconn.PgError{
		Code:           "23503",
		TableName:      "fact_records",
		ConstraintName: "fact_records_generation_id_fkey",
		Detail:         `Key (generation_id)=(scanner_worker:private-repo-name) is not present in table "scope_generations".`,
	})

	if info.Class != "database_foreign_key" {
		t.Fatalf("Class = %q, want database_foreign_key", info.Class)
	}
	if info.SQLState != "23503" {
		t.Fatalf("SQLState = %q, want 23503", info.SQLState)
	}
	if info.Table != "fact_records" {
		t.Fatalf("Table = %q, want fact_records", info.Table)
	}
	if info.Constraint != "fact_records_generation_id_fkey" {
		t.Fatalf("Constraint = %q, want fact_records_generation_id_fkey", info.Constraint)
	}
	if strings.Contains(info.Class+info.SQLState+info.Table+info.Constraint, "private-repo-name") {
		t.Fatalf("classification leaked raw pg detail: %#v", info)
	}
}

func TestClassifyCommitFailureIdentifiesPreTransactionStages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "freshness check",
			err:  errors.New("check active generation freshness: query timeout"),
			want: "freshness_check",
		},
		{
			name: "generation validation",
			err:  errors.New("ingested_at must not be before observed_at"),
			want: "generation_validation",
		},
		{
			name: "self parent validation",
			err:  errors.New("parent_scope_id must differ from scope_id"),
			want: "generation_validation",
		},
		{
			name: "fact validation",
			err:  errors.New(`fact "fact-1" generation_id "old" does not match generation "new"`),
			want: "fact_validation",
		},
		{
			name: "repository catalog",
			err:  errors.New("load repository catalog: context deadline exceeded"),
			want: "repository_catalog",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := classifyCommitFailure(tc.err).Class; got != tc.want {
				t.Fatalf("Class = %q, want %q", got, tc.want)
			}
		})
	}
}
