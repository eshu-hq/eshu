// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticqueue_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticguard"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

func TestPlanBuildsIdempotentQueueRecordsAndSkipsUnchangedChunks(t *testing.T) {
	t.Parallel()

	request := basePlanRequest()
	request.Chunks = append(request.Chunks, request.Chunks[0])
	first, err := semanticqueue.BuildPlan(request)
	if err != nil {
		t.Fatalf("BuildPlan() error = %v, want nil", err)
	}
	if got, want := len(first.Jobs), 1; got != want {
		t.Fatalf("len(first.Jobs) = %d, want %d", got, want)
	}
	job := first.Jobs[0]
	if got, want := job.Status, semanticqueue.StatusPending; got != want {
		t.Fatalf("job.Status = %q, want %q", got, want)
	}
	if job.WorkItemID == "" || job.JobID == "" || job.Fingerprint == "" {
		t.Fatalf("job identifiers must be populated: %+v", job)
	}
	if !job.ProviderJob {
		t.Fatal("job.ProviderJob = false, want true for an allowed semantic provider job")
	}

	secondRequest := basePlanRequest()
	secondRequest.PreviousRecords = first.Jobs
	second, err := semanticqueue.BuildPlan(secondRequest)
	if err != nil {
		t.Fatalf("BuildPlan() with previous error = %v, want nil", err)
	}
	if got, want := len(second.Jobs), 0; got != want {
		t.Fatalf("len(second.Jobs) = %d, want %d for unchanged chunk", got, want)
	}
	if got, want := len(second.Skipped), 1; got != want {
		t.Fatalf("len(second.Skipped) = %d, want %d", got, want)
	}
	skip := second.Skipped[0]
	if got, want := skip.Status, semanticqueue.StatusSkippedUnchanged; got != want {
		t.Fatalf("skip.Status = %q, want %q", got, want)
	}
	if got, want := skip.WorkItemID, job.WorkItemID; got != want {
		t.Fatalf("skip.WorkItemID = %q, want prior work item %q", got, want)
	}
	if got, want := second.Summary.Unchanged, 1; got != want {
		t.Fatalf("Summary.Unchanged = %d, want %d", got, want)
	}
}

func TestFingerprintSkipsUnchangedChunkAcrossGenerationReplay(t *testing.T) {
	t.Parallel()

	firstRequest := basePlanRequest()
	firstRequest.GenerationID = "generation-1"
	first, err := semanticqueue.BuildPlan(firstRequest)
	if err != nil {
		t.Fatalf("BuildPlan() first generation error = %v, want nil", err)
	}

	secondRequest := basePlanRequest()
	secondRequest.GenerationID = "generation-2"
	secondRequest.PreviousRecords = first.Jobs
	second, err := semanticqueue.BuildPlan(secondRequest)
	if err != nil {
		t.Fatalf("BuildPlan() replay generation error = %v, want nil", err)
	}
	if got, want := len(second.Jobs), 0; got != want {
		t.Fatalf("len(second.Jobs) = %d, want unchanged replay skipped", got)
	}
	if got, want := len(second.Skipped), 1; got != want {
		t.Fatalf("len(second.Skipped) = %d, want %d", got, want)
	}
	if got, want := second.Skipped[0].Fingerprint, first.Jobs[0].Fingerprint; got != want {
		t.Fatalf("replay fingerprint = %q, want prior fingerprint %q", got, want)
	}
	if got, want := second.Skipped[0].WorkItemID, first.Jobs[0].WorkItemID; got != want {
		t.Fatalf("replay work item = %q, want prior work item %q", got, want)
	}
	if got, want := second.Skipped[0].GenerationID, "generation-2"; got != want {
		t.Fatalf("replay generation = %q, want current generation %q", got, want)
	}
}

func TestPlanRequeuesChangedChunksAndMarksPreviousRecordStale(t *testing.T) {
	t.Parallel()

	first, err := semanticqueue.BuildPlan(basePlanRequest())
	if err != nil {
		t.Fatalf("BuildPlan() error = %v, want nil", err)
	}
	previous, err := semanticqueue.Succeed(first.Jobs[0], fixedTime().Add(time.Minute), "response-hash-v1")
	if err != nil {
		t.Fatalf("Succeed() error = %v, want nil", err)
	}

	request := basePlanRequest()
	request.PreviousRecords = []semanticqueue.Record{previous}
	request.Chunks[0].SourceHash = "source-hash-v2"
	request.Chunks[0].ChunkHash = "chunk-hash-v2"
	request.Chunks[0].NormalizedContentHash = "normalized-content-v2"
	second, err := semanticqueue.BuildPlan(request)
	if err != nil {
		t.Fatalf("BuildPlan() changed chunk error = %v, want nil", err)
	}
	if got, want := len(second.Jobs), 1; got != want {
		t.Fatalf("len(second.Jobs) = %d, want changed chunk requeued", got)
	}
	if second.Jobs[0].Fingerprint == previous.Fingerprint {
		t.Fatal("changed chunk reused previous fingerprint, want a new queue fingerprint")
	}
	if got, want := len(second.Stale), 1; got != want {
		t.Fatalf("len(second.Stale) = %d, want %d", got, want)
	}
	stale := second.Stale[0]
	if got, want := stale.Status, semanticqueue.StatusStale; got != want {
		t.Fatalf("stale.Status = %q, want %q", got, want)
	}
	if got, want := stale.StaleReason, semanticqueue.StaleReasonSourceChanged; got != want {
		t.Fatalf("stale.StaleReason = %q, want %q", got, want)
	}
	if got, want := stale.WorkItemID, previous.WorkItemID; got != want {
		t.Fatalf("stale.WorkItemID = %q, want prior work item %q", got, want)
	}
	if got, want := stale.GenerationID, request.GenerationID; got != want {
		t.Fatalf("stale.GenerationID = %q, want current generation %q", got, want)
	}
}

func TestPlanMarksDeletedChunksStaleInStatus(t *testing.T) {
	t.Parallel()

	request := basePlanRequest()
	request.Chunks = append(request.Chunks, secondChunk())
	first, err := semanticqueue.BuildPlan(request)
	if err != nil {
		t.Fatalf("BuildPlan() error = %v, want nil", err)
	}

	nextRequest := basePlanRequest()
	nextRequest.PreviousRecords = first.Jobs
	next, err := semanticqueue.BuildPlan(nextRequest)
	if err != nil {
		t.Fatalf("BuildPlan() after deletion error = %v, want nil", err)
	}
	if got, want := len(next.Stale), 1; got != want {
		t.Fatalf("len(next.Stale) = %d, want %d", got, want)
	}
	stale := next.Stale[0]
	if got, want := stale.Status, semanticqueue.StatusStale; got != want {
		t.Fatalf("stale.Status = %q, want %q", got, want)
	}
	if got, want := stale.StaleReason, semanticqueue.StaleReasonSourceDeleted; got != want {
		t.Fatalf("stale.StaleReason = %q, want %q", got, want)
	}
	if got, want := next.Summary.Deleted, 1; got != want {
		t.Fatalf("Summary.Deleted = %d, want %d", got, want)
	}
}

func TestPlanNoProviderModeCreatesNoProviderJobs(t *testing.T) {
	t.Parallel()

	request := basePlanRequest()
	request.Provider.State = semanticqueue.ProviderStateNotConfigured
	request.Provider.ProviderProfileID = ""
	plan, err := semanticqueue.BuildPlan(request)
	if err != nil {
		t.Fatalf("BuildPlan() no provider error = %v, want nil", err)
	}
	if got, want := len(plan.Jobs), 0; got != want {
		t.Fatalf("len(plan.Jobs) = %d, want %d in no-provider mode", got, want)
	}
	if got, want := len(plan.Skipped), 1; got != want {
		t.Fatalf("len(plan.Skipped) = %d, want %d", got, want)
	}
	skip := plan.Skipped[0]
	if got, want := skip.Status, semanticqueue.StatusSkippedNoProvider; got != want {
		t.Fatalf("skip.Status = %q, want %q", got, want)
	}
	if skip.ProviderJob {
		t.Fatal("skip.ProviderJob = true, want no semantic provider job in no-provider mode")
	}
	if got, want := plan.Summary.NoProvider, 1; got != want {
		t.Fatalf("Summary.NoProvider = %d, want %d", got, want)
	}
}

func TestPlanZeroValueProviderFailsClosedToNoProvider(t *testing.T) {
	t.Parallel()

	request := basePlanRequest()
	request.Provider = semanticqueue.Provider{}
	plan, err := semanticqueue.BuildPlan(request)
	if err != nil {
		t.Fatalf("BuildPlan() zero provider error = %v, want nil", err)
	}
	if got, want := len(plan.Jobs), 0; got != want {
		t.Fatalf("len(plan.Jobs) = %d, want %d for zero-value provider", got, want)
	}
	if got, want := plan.Skipped[0].Status, semanticqueue.StatusSkippedNoProvider; got != want {
		t.Fatalf("skip status = %q, want %q", got, want)
	}
}

func TestPlanReadyProviderRequiresProfile(t *testing.T) {
	t.Parallel()

	request := basePlanRequest()
	request.Provider.ProviderProfileID = ""
	_, err := semanticqueue.BuildPlan(request)
	if err == nil {
		t.Fatal("BuildPlan() error = nil, want missing provider profile error")
	}
}

func TestPlanRecordsTerminalAndRetryablePreflightStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(*semanticqueue.PlanRequest)
		wantStatus semanticqueue.Status
		wantRetry  bool
	}{
		{
			name: "provider unavailable is retryable and creates no job",
			mutate: func(request *semanticqueue.PlanRequest) {
				request.Provider.State = semanticqueue.ProviderStateUnavailable
			},
			wantStatus: semanticqueue.StatusProviderUnavailable,
			wantRetry:  true,
		},
		{
			name: "policy denied is terminal",
			mutate: func(request *semanticqueue.PlanRequest) {
				request.Chunks[0].Policy.Allowed = false
				request.Chunks[0].Policy.State = "disabled_by_policy"
				request.Chunks[0].Policy.Reason = semanticpolicy.ReasonPolicyDisabled
			},
			wantStatus: semanticqueue.StatusSkippedPolicy,
			wantRetry:  false,
		},
		{
			name: "egress denied is terminal",
			mutate: func(request *semanticqueue.PlanRequest) {
				request.Chunks[0].Policy.Allowed = false
				request.Chunks[0].Policy.State = "disabled_by_policy"
				request.Chunks[0].Policy.Reason = semanticpolicy.ReasonEgressProviderDenied
			},
			wantStatus: semanticqueue.StatusSkippedPolicy,
			wantRetry:  false,
		},
		{
			name: "budget exhausted is terminal",
			mutate: func(request *semanticqueue.PlanRequest) {
				request.Chunks[0].Budget.Allowed = false
				request.Chunks[0].Budget.State = semanticqueue.BudgetStateExhausted
				request.Chunks[0].Budget.Reason = semanticqueue.BudgetReasonDailyLimit
			},
			wantStatus: semanticqueue.StatusSkippedBudget,
			wantRetry:  false,
		},
		{
			name: "unsafe guard decision is terminal",
			mutate: func(request *semanticqueue.PlanRequest) {
				request.Chunks[0].Guard.Allowed = false
				request.Chunks[0].Guard.State = semanticguard.StateDeniedPromptInjectionRisk
				request.Chunks[0].Guard.Reason = semanticguard.ReasonPromptInjectionIndicator
			},
			wantStatus: semanticqueue.StatusUnsafePayload,
			wantRetry:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request := basePlanRequest()
			tt.mutate(&request)
			plan, err := semanticqueue.BuildPlan(request)
			if err != nil {
				t.Fatalf("BuildPlan() error = %v, want nil", err)
			}
			if got, want := len(plan.Jobs), 0; got != want {
				t.Fatalf("len(plan.Jobs) = %d, want %d", got, want)
			}
			if got, want := len(plan.Skipped), 1; got != want {
				t.Fatalf("len(plan.Skipped) = %d, want %d", got, want)
			}
			record := plan.Skipped[0]
			if got := record.Status; got != tt.wantStatus {
				t.Fatalf("record.Status = %q, want %q", got, tt.wantStatus)
			}
			if got := record.Retryable; got != tt.wantRetry {
				t.Fatalf("record.Retryable = %t, want %t", got, tt.wantRetry)
			}
			if record.ProviderJob {
				t.Fatal("record.ProviderJob = true, want preflight state to avoid provider jobs")
			}
		})
	}
}

func basePlanRequest() semanticqueue.PlanRequest {
	return semanticqueue.PlanRequest{
		ScopeID:      "repository:eshu",
		GenerationID: "generation-1",
		Provider: semanticqueue.Provider{
			State:             semanticqueue.ProviderStateReady,
			ProviderKind:      "deepseek",
			ProviderProfileID: "semantic-docs-default",
			ProfileClass:      "hosted",
		},
		Now: fixedTime(),
		Chunks: []semanticqueue.SourceChunk{
			{
				SourceID:              "docs:architecture",
				SourceClass:           semanticguard.SourceDocumentation,
				SourceHash:            "source-hash-v1",
				SourceVersion:         "source-version-v1",
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
	}
}

func secondChunk() semanticqueue.SourceChunk {
	chunk := basePlanRequest().Chunks[0]
	chunk.ChunkID = "architecture:section-2"
	chunk.ChunkHash = "chunk-hash-section-2"
	chunk.NormalizedContentHash = "normalized-content-section-2"
	return chunk
}

func fixedTime() time.Time {
	return time.Date(2026, time.June, 9, 4, 0, 0, 0, time.UTC)
}
