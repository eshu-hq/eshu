// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowControlEmbeddedSchemaMatchesDataPlaneSchema(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../../../schema/data-plane/postgres/014_workflow_control_plane.sql")
	if err != nil {
		t.Fatalf("read workflow control schema file: %v", err)
	}
	if got, want := strings.TrimSpace(workflowControlSchemaSQL), strings.TrimSpace(string(raw)); got != want {
		t.Fatal("embedded workflow control schema does not match data-plane schema file")
	}
}

func TestWorkflowControlSchemaBackfillsRequiredIdentity(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"generation_id TEXT NOT NULL",
		"SET generation_id = work_item_id || ':legacy-missing-generation'",
		"THEN 'legacy_missing_generation_identity'",
		"SET source_system = collector_kind",
		"SET acceptance_unit_id = scope_id",
		"SET source_run_id = generation_id",
		"ALTER COLUMN generation_id SET NOT NULL",
		"ALTER COLUMN source_system SET NOT NULL",
		"ALTER COLUMN acceptance_unit_id SET NOT NULL",
		"ALTER COLUMN source_run_id SET NOT NULL",
	} {
		if !strings.Contains(workflowControlSchemaSQL, want) {
			t.Fatalf("workflowControlSchemaSQL missing identity migration %q", want)
		}
	}
}

func TestWorkflowControlSchemaIndexesCollectorScopeGenerationLookup(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"workflow_work_items_collector_scope_generation_updated_idx",
		"ON workflow_work_items (",
		"collector_kind,",
		"scope_id,",
		"generation_id,",
		"updated_at DESC,",
		"work_item_id ASC",
	} {
		if !strings.Contains(workflowControlSchemaSQL, want) {
			t.Fatalf("workflowControlSchemaSQL missing status lookup index marker %q", want)
		}
	}
}

// TestWorkflowControlSchemaIndexesFamilyQueueDepth locks the partial index that
// keeps the per-family queue-depth gauge query (#2857) off a sequential scan of
// workflow_work_items on every metrics scrape. The index must match the gauge
// query's WHERE filter and grouping columns.
func TestWorkflowControlSchemaIndexesFamilyQueueDepth(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"workflow_work_items_family_queue_depth_idx",
		"ON workflow_work_items (collector_kind, source_system, status)",
		"WHERE status IN ('pending', 'claimed', 'failed_retryable', 'expired')",
	} {
		if !strings.Contains(workflowControlSchemaSQL, want) {
			t.Fatalf("workflowControlSchemaSQL missing family queue-depth index marker %q", want)
		}
	}
}

func TestWorkflowControlClaimNextEligibleRequiresCompleteIdentity(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"source_system <> ''",
		"acceptance_unit_id <> ''",
		"source_run_id <> ''",
		"generation_id <> ''",
	} {
		if !strings.Contains(claimNextWorkflowWorkItemQuery, want) {
			t.Fatalf("claim query missing identity guard %q:\n%s", want, claimNextWorkflowWorkItemQuery)
		}
	}
	if strings.Contains(claimNextWorkflowWorkItemQuery, "COALESCE(item.generation_id, '') AS generation_id") {
		t.Fatalf("claim query still coerces nullable generation identity:\n%s", claimNextWorkflowWorkItemQuery)
	}
}

func TestWorkflowControlEnqueuePreservesRequiredGenerationID(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC)
	item := workflow.WorkItem{
		WorkItemID:          "item-1",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		SourceSystem:        "git",
		ScopeID:             "scope-1",
		AcceptanceUnitID:    "repository:scope-1",
		SourceRunID:         "source-run-1",
		GenerationID:        "generation-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := store.EnqueueWorkItems(context.Background(), []workflow.WorkItem{item}); err != nil {
		t.Fatalf("EnqueueWorkItems() error = %v, want nil", err)
	}
	query := db.execs[0].query
	if strings.Contains(query, "NULLIF($13, '')") {
		t.Fatalf("enqueue query still converts generation_id to NULL:\n%s", query)
	}
	if !strings.Contains(query, "$13, NULLIF($14, '')") {
		t.Fatalf("enqueue query missing direct generation_id placeholder:\n%s", query)
	}
}
