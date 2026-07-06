// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// fifoExecQueryer is a minimal ExecQueryer that answers QueryContext calls
// from a strict FIFO queue with no query-text special-casing. It exists
// because the shared fakeExecQueryer (work_queue_lifecycle_test.go)
// unconditionally intercepts lookupDeferredBackfillPartitionMemosQuery and
// always answers "no memo rows" independent of staged responses. Mirrors the
// dedicated-fake convention noopExecQueryer already uses in
// deferred_backfill_partition_memo_test.go for the same reason.
type fifoExecQueryer struct {
	responses []queueFakeRows
}

func (f *fifoExecQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("fifoExecQueryer: unexpected ExecContext")
}

func (f *fifoExecQueryer) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) {
	if len(f.responses) == 0 {
		return nil, errors.New("fifoExecQueryer: no more staged responses")
	}
	rows := f.responses[0]
	f.responses = f.responses[1:]
	if rows.err != nil {
		return nil, rows.err
	}
	return &rows, nil
}

// TestApplyReopenPartitionMemoGateNilSkipSetReopensEverything is the core
// proof for the bootstrap-index P0 fix (issue #4770/#4816 hostile re-review):
// a nil skip-set must reopen every candidate unconditionally, never fall back
// to a memo-table re-read. This is the exact scenario a standalone caller
// (bootstrap-index's Reopen* phase, called after its own
// BackfillAllRelationshipEvidence phase already wrote a fresh memo row) hits.
func TestApplyReopenPartitionMemoGateNilSkipSetReopensEverything(t *testing.T) {
	t.Parallel()

	items := []reopenWorkItemRef{
		{WorkItemID: "work-1", Partition: scopeGenerationPartition{ScopeID: "scope-a", GenerationID: "gen-a"}},
		{WorkItemID: "work-2", Partition: scopeGenerationPartition{ScopeID: "scope-b", GenerationID: "gen-b"}},
	}

	result := applyReopenPartitionMemoGate(context.Background(), "deployment_mapping", items, nil, nil)

	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want empty (nil skip-set must reopen unconditionally)", result.Skipped)
	}
	gotReopen := map[string]bool{}
	for _, item := range result.ToReopen {
		gotReopen[item.WorkItemID] = true
	}
	if !gotReopen["work-1"] || !gotReopen["work-2"] || len(result.ToReopen) != 2 {
		t.Fatalf("ToReopen = %v, want [work-1 work-2]", result.ToReopen)
	}
}

// TestApplyReopenPartitionMemoGateSkipSetSkipsMemberPartition proves the
// same-pass skip-set path (issue #4770/#4816): a work item whose partition is
// a member of skippedThisPass is skipped.
func TestApplyReopenPartitionMemoGateSkipSetSkipsMemberPartition(t *testing.T) {
	t.Parallel()

	items := []reopenWorkItemRef{
		{WorkItemID: "work-1", Partition: scopeGenerationPartition{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}
	skipSet := map[scopeGenerationPartition]struct{}{
		{ScopeID: "scope-a", GenerationID: "gen-a"}: {},
	}

	result := applyReopenPartitionMemoGate(context.Background(), "deployment_mapping", items, skipSet, nil)

	if len(result.ToReopen) != 0 {
		t.Fatalf("ToReopen = %v, want empty (skip-set member must be skipped)", result.ToReopen)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].WorkItemID != "work-1" {
		t.Fatalf("Skipped = %v, want [work-1]", result.Skipped)
	}
}

// TestApplyReopenPartitionMemoGateSkipSetReopensNonMemberPartition proves a
// work item whose partition is NOT a member of skippedThisPass (the backfill
// reprocessed it this pass) still reopens.
func TestApplyReopenPartitionMemoGateSkipSetReopensNonMemberPartition(t *testing.T) {
	t.Parallel()

	items := []reopenWorkItemRef{
		{WorkItemID: "work-1", Partition: scopeGenerationPartition{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}
	// Empty-but-non-nil skip-set: every candidate was a memo MISS this pass
	// (the exact RED scenario for the #4770/#4816 same-pass fix).
	skipSet := map[scopeGenerationPartition]struct{}{}

	result := applyReopenPartitionMemoGate(context.Background(), "deployment_mapping", items, skipSet, nil)

	if len(result.ToReopen) != 1 || result.ToReopen[0].WorkItemID != "work-1" {
		t.Fatalf("ToReopen = %v, want [work-1] (empty-but-non-nil skip-set must not accidentally skip)", result.ToReopen)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want empty", result.Skipped)
	}
}

// TestApplyReopenPartitionMemoGateMixedPartitionsSplitsCorrectly proves the
// skip-set gate handles a mixed batch: one member partition skipped, one
// non-member partition reopened, in a single pass.
func TestApplyReopenPartitionMemoGateMixedPartitionsSplitsCorrectly(t *testing.T) {
	t.Parallel()

	items := []reopenWorkItemRef{
		{WorkItemID: "work-hit", Partition: scopeGenerationPartition{ScopeID: "scope-hit", GenerationID: "gen-hit"}},
		{WorkItemID: "work-miss", Partition: scopeGenerationPartition{ScopeID: "scope-miss", GenerationID: "gen-miss"}},
	}
	skipSet := map[scopeGenerationPartition]struct{}{
		{ScopeID: "scope-hit", GenerationID: "gen-hit"}: {},
	}

	result := applyReopenPartitionMemoGate(context.Background(), "deployment_mapping", items, skipSet, nil)

	if len(result.Skipped) != 1 || result.Skipped[0].WorkItemID != "work-hit" {
		t.Fatalf("Skipped = %v, want [work-hit]", result.Skipped)
	}
	if len(result.ToReopen) != 1 || result.ToReopen[0].WorkItemID != "work-miss" {
		t.Fatalf("ToReopen = %v, want [work-miss]", result.ToReopen)
	}
}

// TestApplyReopenPartitionMemoGateBlankPartitionAlwaysReopens proves a work
// item with a blank scope_id or generation_id (defensive: schema requires
// NOT NULL, but a legacy row or fixture may leave it empty) always reopens
// rather than risk an unintended skip on unrecognized shape, even when its
// blank partition would otherwise coincidentally match a skip-set zero value.
func TestApplyReopenPartitionMemoGateBlankPartitionAlwaysReopens(t *testing.T) {
	t.Parallel()

	items := []reopenWorkItemRef{
		{WorkItemID: "work-blank", Partition: scopeGenerationPartition{ScopeID: "", GenerationID: ""}},
	}
	skipSet := map[scopeGenerationPartition]struct{}{
		{ScopeID: "", GenerationID: ""}: {},
	}

	result := applyReopenPartitionMemoGate(context.Background(), "deployment_mapping", items, skipSet, nil)

	if len(result.ToReopen) != 1 || result.ToReopen[0].WorkItemID != "work-blank" {
		t.Fatalf("ToReopen = %v, want [work-blank]", result.ToReopen)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want empty for a blank partition", result.Skipped)
	}
}

// TestApplyReopenPartitionMemoGateEmptyItemsIsNoop proves an empty input
// returns an empty result without panicking, for both the nil and skip-set
// paths.
func TestApplyReopenPartitionMemoGateEmptyItemsIsNoop(t *testing.T) {
	t.Parallel()

	result := applyReopenPartitionMemoGate(context.Background(), "deployment_mapping", nil, nil, nil)
	if len(result.ToReopen) != 0 || len(result.Skipped) != 0 {
		t.Fatalf("applyReopenPartitionMemoGate(nil items, nil skip-set) = %+v, want empty result", result)
	}

	result = applyReopenPartitionMemoGate(context.Background(), "deployment_mapping", nil, map[scopeGenerationPartition]struct{}{}, nil)
	if len(result.ToReopen) != 0 || len(result.Skipped) != 0 {
		t.Fatalf("applyReopenPartitionMemoGate(nil items, empty skip-set) = %+v, want empty result", result)
	}
}

// TestListSucceededDeploymentMappingWorkItemsQueryIncludesPartitionColumns
// locks the schema-shape change (issue #4770): the reopen listing query MUST
// select scope_id and generation_id alongside work_item_id so the reopen
// partition-memo gate can key on the work item's partition without a join —
// fact_work_items already carries both columns directly on the row.
func TestListSucceededDeploymentMappingWorkItemsQueryIncludesPartitionColumns(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"SELECT work_item_id, scope_id, generation_id",
		"FROM fact_work_items",
		"domain = 'deployment_mapping'",
		"status = 'succeeded'",
	} {
		if !strings.Contains(listSucceededDeploymentMappingWorkItemsQuery, want) {
			t.Fatalf("listSucceededDeploymentMappingWorkItemsQuery missing %q:\n%s", want, listSucceededDeploymentMappingWorkItemsQuery)
		}
	}
}

// TestListSucceededCodeImportRepoEdgeWorkItemsQueryIncludesPartitionColumns is
// the code_import_repo_edge sibling of the schema-shape lock above.
func TestListSucceededCodeImportRepoEdgeWorkItemsQueryIncludesPartitionColumns(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"SELECT work_item_id, scope_id, generation_id",
		"FROM fact_work_items",
		"domain = 'code_import_repo_edge'",
		"status = 'succeeded'",
	} {
		if !strings.Contains(listSucceededCodeImportRepoEdgeWorkItemsQuery, want) {
			t.Fatalf("listSucceededCodeImportRepoEdgeWorkItemsQuery missing %q:\n%s", want, listSucceededCodeImportRepoEdgeWorkItemsQuery)
		}
	}
}

// TestListSucceededDeploymentMappingWorkItemsScansPartitionColumns proves
// listSucceededDeploymentMappingWorkItems (the Go scan side) actually reads
// back the scope_id/generation_id columns into reopenWorkItemRef.Partition,
// not just that the query text asks for them.
func TestListSucceededDeploymentMappingWorkItemsScansPartitionColumns(t *testing.T) {
	t.Parallel()

	queryer := &fifoExecQueryer{
		responses: []queueFakeRows{
			{rows: [][]any{{"work-1", "scope-a", "gen-a"}}},
		},
	}
	items, err := listSucceededDeploymentMappingWorkItems(context.Background(), queryer)
	if err != nil {
		t.Fatalf("listSucceededDeploymentMappingWorkItems() error = %v, want nil", err)
	}
	want := []reopenWorkItemRef{
		{WorkItemID: "work-1", Partition: scopeGenerationPartition{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}
	if len(items) != 1 || items[0] != want[0] {
		t.Fatalf("listSucceededDeploymentMappingWorkItems() = %+v, want %+v", items, want)
	}
}

// TestListSucceededCodeImportRepoEdgeWorkItemsScansPartitionColumns is the
// code_import_repo_edge sibling of the scan proof above.
func TestListSucceededCodeImportRepoEdgeWorkItemsScansPartitionColumns(t *testing.T) {
	t.Parallel()

	queryer := &fifoExecQueryer{
		responses: []queueFakeRows{
			{rows: [][]any{{"work-2", "scope-b", "gen-b"}}},
		},
	}
	items, err := listSucceededCodeImportRepoEdgeWorkItems(context.Background(), queryer)
	if err != nil {
		t.Fatalf("listSucceededCodeImportRepoEdgeWorkItems() error = %v, want nil", err)
	}
	want := []reopenWorkItemRef{
		{WorkItemID: "work-2", Partition: scopeGenerationPartition{ScopeID: "scope-b", GenerationID: "gen-b"}},
	}
	if len(items) != 1 || items[0] != want[0] {
		t.Fatalf("listSucceededCodeImportRepoEdgeWorkItems() = %+v, want %+v", items, want)
	}
}
