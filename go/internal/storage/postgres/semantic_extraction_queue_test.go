package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticguard"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

func TestBootstrapDefinitionsIncludeSemanticExtractionQueue(t *testing.T) {
	t.Parallel()

	defs := BootstrapDefinitions()
	if len(defs) != 48 {
		t.Fatalf("BootstrapDefinitions() len = %d, want 48", len(defs))
	}
	var semanticQueue Definition
	for _, def := range defs {
		if def.Name == "semantic_extraction_jobs" {
			semanticQueue = def
			break
		}
	}
	if semanticQueue.Name == "" {
		t.Fatal("semantic_extraction_jobs definition missing")
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS semantic_extraction_jobs",
		"job_id TEXT PRIMARY KEY",
		"work_item_id TEXT NOT NULL",
		"source_id_hash TEXT NOT NULL",
		"chunk_id_hash TEXT NOT NULL",
		"status TEXT NOT NULL",
		"provider_profile_id TEXT NULL",
		"budget_metadata JSONB NOT NULL DEFAULT '{}'::jsonb",
		"semantic_extraction_jobs_scope_generation_status_idx",
		"semantic_extraction_jobs_fingerprint_idx",
		"semantic_extraction_jobs_claim_idx",
		"semantic_extraction_jobs_provider_claim_idx",
	} {
		if !strings.Contains(semanticQueue.SQL, want) {
			t.Fatalf("semantic_extraction_jobs SQL missing %q", want)
		}
	}
	if strings.Contains(semanticQueue.SQL, "source_id TEXT") ||
		strings.Contains(semanticQueue.SQL, "chunk_id TEXT") ||
		strings.Contains(semanticQueue.SQL, "prompt_text") ||
		strings.Contains(semanticQueue.SQL, "response_text") {
		t.Fatalf("semantic queue schema stores raw source identifiers or prompt bodies:\n%s", semanticQueue.SQL)
	}
}

func TestSemanticExtractionQueueStoreApplyPlanUpsertsMetadataOnlyRecords(t *testing.T) {
	t.Parallel()

	plan := semanticQueueStoragePlan(t)
	db := &fakeExecQueryer{}
	store := NewSemanticExtractionQueueStore(db)
	if err := store.ApplyPlan(context.Background(), plan); err != nil {
		t.Fatalf("ApplyPlan() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"INSERT INTO semantic_extraction_jobs",
		"ON CONFLICT (job_id) DO UPDATE",
		"status = EXCLUDED.status",
		"fingerprint = EXCLUDED.fingerprint",
		"budget_metadata",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("ApplyPlan query missing %q:\n%s", want, query)
		}
	}
	for _, arg := range db.execs[0].args {
		if got, ok := arg.(string); ok {
			if strings.Contains(got, "docs:architecture") || strings.Contains(got, "architecture:section-1") {
				t.Fatalf("ApplyPlan leaked raw source identifier in args: %q", got)
			}
		}
	}
}

func TestSemanticExtractionQueueStoreSummaryAggregatesRedactedStatus(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{string(semanticqueue.StatusPending), int64(2)},
				{string(semanticqueue.StatusStale), int64(1)},
				{string(semanticqueue.StatusSkippedNoProvider), int64(3)},
			}},
		},
	}
	store := NewSemanticExtractionQueueStore(db)
	summary, err := store.StatusSummary(context.Background(), "repository:eshu", "generation-1")
	if err != nil {
		t.Fatalf("StatusSummary() error = %v, want nil", err)
	}
	if got, want := summary.Planned, 2; got != want {
		t.Fatalf("summary.Planned = %d, want %d", got, want)
	}
	if got, want := summary.Stale, 1; got != want {
		t.Fatalf("summary.Stale = %d, want %d", got, want)
	}
	if got, want := summary.NoProvider, 3; got != want {
		t.Fatalf("summary.NoProvider = %d, want %d", got, want)
	}
	query := db.queries[0].query
	if strings.Contains(query, "source_id_hash") || strings.Contains(query, "chunk_id_hash") {
		t.Fatalf("status summary query should aggregate by status only:\n%s", query)
	}
}

func TestSemanticExtractionQueueStoreClaimUsesLeaseFencingAndSkipLocked(t *testing.T) {
	t.Parallel()

	now := semanticQueueStorageTime()
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"job-1",
				"work-1",
				"fingerprint-1",
				"repository:eshu",
				"generation-1",
				string(semanticqueue.StatusClaimed),
				int64(1),
				sql.NullString{String: "documentation", Valid: true},
				sql.NullString{String: "semantic-docs", Valid: true},
				sql.NullString{String: "semantic-docs-default", Valid: true},
				sql.NullString{String: "managed", Valid: true},
			}}},
		},
	}
	store := NewSemanticExtractionQueueStore(db)
	record, ok, err := store.ClaimNext(
		context.Background(),
		"repository:eshu",
		"semantic-worker-1",
		now,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("ClaimNext() ok = false, want true")
	}
	if got, want := record.Status, semanticqueue.StatusClaimed; got != want {
		t.Fatalf("record.Status = %q, want %q", got, want)
	}
	if got, want := record.SourceClass, "documentation"; got != want {
		t.Fatalf("record.SourceClass = %q, want %q", got, want)
	}
	if got, want := record.ProviderProfileID, "semantic-docs-default"; got != want {
		t.Fatalf("record.ProviderProfileID = %q, want %q", got, want)
	}
	if got, want := record.ProviderProfileClass, "managed"; got != want {
		t.Fatalf("record.ProviderProfileClass = %q, want %q", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"FOR UPDATE SKIP LOCKED",
		"lease_owner = $",
		"claim_until = $",
		"status IN ('pending', 'retrying')",
		"(claim_until IS NULL OR claim_until <= $",
		"jobs.source_class",
		"jobs.provider_profile_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("ClaimNext query missing %q:\n%s", want, query)
		}
	}
}

func TestSemanticExtractionQueueStoreRetryAndDeadLetterUseLeaseFence(t *testing.T) {
	t.Parallel()

	plan := semanticQueueStoragePlan(t)
	record := plan.Jobs[0]
	db := &fakeExecQueryer{}
	store := NewSemanticExtractionQueueStore(db)
	now := semanticQueueStorageTime().Add(time.Minute)
	if err := store.RetryClaim(
		context.Background(),
		record,
		"semantic-worker-1",
		now,
		now.Add(5*time.Minute),
		semanticqueue.Failure{Class: semanticqueue.FailureClassProviderUnavailable},
	); err != nil {
		t.Fatalf("RetryClaim() error = %v, want nil", err)
	}
	if err := store.DeadLetterClaim(
		context.Background(),
		record,
		"semantic-worker-1",
		now.Add(time.Minute),
		semanticqueue.Failure{Class: semanticqueue.FailureClassRetryExhausted},
	); err != nil {
		t.Fatalf("DeadLetterClaim() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	for _, exec := range db.execs {
		for _, want := range []string{
			"WHERE job_id = $",
			"lease_owner = $",
			"fingerprint = $",
		} {
			if !strings.Contains(exec.query, want) {
				t.Fatalf("claim update missing lease fence %q:\n%s", want, exec.query)
			}
		}
	}
	if !strings.Contains(db.execs[0].query, "status = 'retrying'") {
		t.Fatalf("retry query missing retrying status:\n%s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[1].query, "status = 'dead_letter'") {
		t.Fatalf("dead-letter query missing terminal status:\n%s", db.execs[1].query)
	}
}

func TestSemanticExtractionQueueStoreSkipByPolicyUsesLeaseFenceAndTerminalStatus(t *testing.T) {
	t.Parallel()

	plan := semanticQueueStoragePlan(t)
	record := plan.Jobs[0]
	db := &fakeExecQueryer{}
	store := NewSemanticExtractionQueueStore(db)
	now := semanticQueueStorageTime().Add(time.Minute)
	if err := store.SkipClaimByPolicy(
		context.Background(),
		record,
		"semantic-worker-1",
		now,
		semanticpolicy.ReasonEgressProviderDenied,
	); err != nil {
		t.Fatalf("SkipClaimByPolicy() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"status = 'skipped_policy'",
		"provider_job = false",
		"retryable = false",
		"WHERE job_id = $",
		"lease_owner = $",
		"fingerprint = $",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("skip-by-policy query missing %q:\n%s", want, query)
		}
	}
}

func TestSemanticExtractionQueueStoreSkipByPolicyRequiresLeaseOwner(t *testing.T) {
	t.Parallel()

	plan := semanticQueueStoragePlan(t)
	record := plan.Jobs[0]
	store := NewSemanticExtractionQueueStore(&fakeExecQueryer{})
	if err := store.SkipClaimByPolicy(
		context.Background(),
		record,
		"  ",
		semanticQueueStorageTime(),
		semanticpolicy.ReasonEgressProviderDenied,
	); err == nil {
		t.Fatal("SkipClaimByPolicy() error = nil, want lease owner error")
	}
}

func TestSemanticExtractionQueueStoreSucceedUsesLeaseFence(t *testing.T) {
	t.Parallel()

	plan := semanticQueueStoragePlan(t)
	record := plan.Jobs[0]
	db := &fakeExecQueryer{}
	store := NewSemanticExtractionQueueStore(db)
	now := semanticQueueStorageTime().Add(time.Minute)
	if err := store.SucceedClaim(
		context.Background(),
		record,
		"semantic-worker-1",
		now,
		"response-hash-v1",
		semanticqueue.BudgetDecision{
			Allowed:            true,
			State:              semanticqueue.BudgetStateAllowed,
			Reason:             semanticqueue.BudgetReasonAllowed,
			ActualInputTokens:  80,
			ActualOutputTokens: 20,
			ActualCostMicros:   130,
		},
	); err != nil {
		t.Fatalf("SucceedClaim() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"status = 'succeeded'",
		"provider_job = false",
		"retryable = false",
		"claim_until = NULL",
		"lease_owner = NULL",
		"failure_class = NULL",
		"response_hash = $",
		"budget_metadata = $",
		"WHERE job_id = $",
		"lease_owner = $",
		"fingerprint = $",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("success query missing %q:\n%s", want, query)
		}
	}
}

func TestSemanticExtractionQueueStoreClaimMutationsRejectStaleLease(t *testing.T) {
	t.Parallel()

	plan := semanticQueueStoragePlan(t)
	record := plan.Jobs[0]
	now := semanticQueueStorageTime().Add(time.Minute)
	db := &fakeExecQueryer{
		execResults: []sql.Result{
			rowsAffectedResult{rowsAffected: 0},
			rowsAffectedResult{rowsAffected: 0},
			rowsAffectedResult{rowsAffected: 0},
		},
	}
	store := NewSemanticExtractionQueueStore(db)

	err := store.RetryClaim(
		context.Background(),
		record,
		"stale-worker",
		now,
		now.Add(time.Minute),
		semanticqueue.Failure{Class: semanticqueue.FailureClassProviderUnavailable},
	)
	if !errors.Is(err, ErrSemanticExtractionClaimRejected) {
		t.Fatalf("RetryClaim() error = %v, want %v", err, ErrSemanticExtractionClaimRejected)
	}
	err = store.DeadLetterClaim(
		context.Background(),
		record,
		"stale-worker",
		now,
		semanticqueue.Failure{Class: semanticqueue.FailureClassRetryExhausted},
	)
	if !errors.Is(err, ErrSemanticExtractionClaimRejected) {
		t.Fatalf("DeadLetterClaim() error = %v, want %v", err, ErrSemanticExtractionClaimRejected)
	}
	err = store.SucceedClaim(
		context.Background(),
		record,
		"stale-worker",
		now,
		"response-hash-v1",
		semanticqueue.BudgetDecision{Allowed: true, State: semanticqueue.BudgetStateAllowed},
	)
	if !errors.Is(err, ErrSemanticExtractionClaimRejected) {
		t.Fatalf("SucceedClaim() error = %v, want %v", err, ErrSemanticExtractionClaimRejected)
	}
}

func TestSemanticExtractionQueueStoreClaimSkipsBackoffAndNonProviderRows(t *testing.T) {
	t.Parallel()

	now := semanticQueueStorageTime()
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewSemanticExtractionQueueStore(db)
	_, _, err := store.ClaimNext(
		context.Background(),
		"repository:eshu",
		"semantic-worker-1",
		now,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v, want nil", err)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"provider_job = true",
		"(next_attempt_at IS NULL OR next_attempt_at <= $2)",
		"FOR UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("ClaimNext query missing %q:\n%s", want, query)
		}
	}
}

func semanticQueueStoragePlan(t *testing.T) semanticqueue.Plan {
	t.Helper()

	plan, err := semanticqueue.BuildPlan(semanticqueue.PlanRequest{
		ScopeID:      "repository:eshu",
		GenerationID: "generation-1",
		Provider: semanticqueue.Provider{
			State:             semanticqueue.ProviderStateReady,
			ProviderKind:      "deepseek",
			ProviderProfileID: "semantic-docs-default",
			ProfileClass:      "hosted",
		},
		Now: semanticQueueStorageTime(),
		Chunks: []semanticqueue.SourceChunk{
			{
				SourceID:              "docs:architecture",
				SourceClass:           semanticguard.SourceDocumentation,
				SourceHash:            "source-hash-v1",
				SourceVersion:         "generation-1",
				ChunkID:               "architecture:section-1",
				ChunkHash:             "chunk-hash-v1",
				NormalizedContentHash: "normalized-content-v1",
				PromptVersion:         "semantic-docs-prompt-v1",
				RedactionVersion:      "redaction-v1",
				ExtractorVersion:      "doctruth-v1",
				ExtractionMode:        "hosted",
				Policy: semanticpolicy.Decision{
					Allowed:           true,
					State:             "allowed",
					Reason:            semanticpolicy.ReasonAllowed,
					PolicyID:          "policy-1",
					RuleID:            "docs-rule",
					ProviderProfileID: "semantic-docs-default",
					SourceClass:       semanticguard.SourceDocumentation,
				},
				Guard: semanticguard.Decision{
					Allowed:           true,
					State:             semanticguard.StateAllowed,
					Reason:            semanticguard.ReasonAllowed,
					PolicyID:          "policy-1",
					RuleID:            "docs-rule",
					ProviderProfileID: "semantic-docs-default",
					SourceClass:       semanticguard.SourceDocumentation,
					ActorClass:        "hosted_worker",
					ACLState:          semanticguard.ACLAllowed,
					ClassifierVersion: "classifier-v1",
					SourceHash:        "source-hash-v1",
					ChunkHash:         "chunk-hash-v1",
				},
				Budget: semanticqueue.BudgetDecision{
					Allowed:              true,
					State:                semanticqueue.BudgetStateAllowed,
					Reason:               semanticqueue.BudgetReasonAllowed,
					EstimatedInputTokens: 120,
					BudgetUnit:           "daily_tokens",
					BudgetWindow:         "2026-06-09",
					RemainingTokens:      1000,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v, want nil", err)
	}
	return plan
}

func semanticQueueStorageTime() time.Time {
	return time.Date(2026, time.June, 9, 4, 0, 0, 0, time.UTC)
}
