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
// always answers "no memo rows" independent of staged responses — exactly the
// query this file's tests need to stage memo-HIT rows for. Mirrors the
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

// TestApplyReopenPartitionMemoGateSkipsMemoHitPartition is the RED-then-GREEN
// core proof for issue #4770: a succeeded work item whose (scope_id,
// generation_id) partition already has a memo row matching the CURRENT
// catalog fingerprint must be skipped (not reopened). Disabling the gate (by
// passing an empty currentFingerprint, the same short-circuit the reopen
// callers use on a fingerprint-computation failure) must make this test fail,
// proving the assertion is a real guard on the gate, not a tautology.
func TestApplyReopenPartitionMemoGateSkipsMemoHitPartition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const fingerprint = "sha256:current"
	memoDB := &fifoExecQueryer{
		responses: []queueFakeRows{
			{rows: [][]any{{"scope-a", "gen-a", fingerprint}}},
		},
	}
	memoStore := newDeferredBackfillPartitionMemoStore(memoDB)
	items := []reopenWorkItemRef{
		{WorkItemID: "work-1", Partition: scopeGenerationPartition{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}

	result, err := applyReopenPartitionMemoGate(ctx, memoStore, "deployment_mapping", items, fingerprint, nil, nil)
	if err != nil {
		t.Fatalf("applyReopenPartitionMemoGate() error = %v, want nil", err)
	}
	if len(result.ToReopen) != 0 {
		t.Fatalf("ToReopen = %v, want empty (memo-hit partition must be skipped)", result.ToReopen)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].WorkItemID != "work-1" {
		t.Fatalf("Skipped = %v, want [work-1]", result.Skipped)
	}
}

// TestApplyReopenPartitionMemoGateWithoutFingerprintReopensEverything proves
// the guard in the test above is real: with the SAME memo-hit fixture but an
// empty currentFingerprint (the gate-disabled path), the work item must
// reopen instead of being skipped. This is the RED case that would fail if
// applyReopenPartitionMemoGate's skip logic were removed or short-circuited
// to always skip.
func TestApplyReopenPartitionMemoGateWithoutFingerprintReopensEverything(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memoDB := &fifoExecQueryer{
		responses: []queueFakeRows{
			{rows: [][]any{{"scope-a", "gen-a", "sha256:current"}}},
		},
	}
	memoStore := newDeferredBackfillPartitionMemoStore(memoDB)
	items := []reopenWorkItemRef{
		{WorkItemID: "work-1", Partition: scopeGenerationPartition{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}

	result, err := applyReopenPartitionMemoGate(ctx, memoStore, "deployment_mapping", items, "", nil, nil)
	if err != nil {
		t.Fatalf("applyReopenPartitionMemoGate() error = %v, want nil", err)
	}
	if len(result.ToReopen) != 1 || result.ToReopen[0].WorkItemID != "work-1" {
		t.Fatalf("ToReopen = %v, want [work-1] when the gate is disabled (empty fingerprint)", result.ToReopen)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want empty when the gate is disabled", result.Skipped)
	}
}

// TestApplyReopenPartitionMemoGateReopensNonMemoHitPartition proves a
// partition whose memo fingerprint does NOT match the current pass (catalog
// changed since it last committed) still reopens.
func TestApplyReopenPartitionMemoGateReopensNonMemoHitPartition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memoDB := &fifoExecQueryer{
		responses: []queueFakeRows{
			{rows: [][]any{{"scope-a", "gen-a", "sha256:stale"}}},
		},
	}
	memoStore := newDeferredBackfillPartitionMemoStore(memoDB)
	items := []reopenWorkItemRef{
		{WorkItemID: "work-1", Partition: scopeGenerationPartition{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}

	result, err := applyReopenPartitionMemoGate(ctx, memoStore, "deployment_mapping", items, "sha256:current", nil, nil)
	if err != nil {
		t.Fatalf("applyReopenPartitionMemoGate() error = %v, want nil", err)
	}
	if len(result.ToReopen) != 1 || result.ToReopen[0].WorkItemID != "work-1" {
		t.Fatalf("ToReopen = %v, want [work-1] when the catalog fingerprint changed", result.ToReopen)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want empty when the catalog fingerprint changed", result.Skipped)
	}
}

// TestApplyReopenPartitionMemoGateReopensArgoCDBearingPartition proves the
// ArgoCD carve-out needs no special-casing in the reopen gate: an
// ArgoCD-bearing partition NEVER gets a memo row (writeDeferredBackfillPartitionMemos
// excludes it on the write side), so a work item in that partition is always
// a memo miss here and always reopens, exactly like any other never-memoized
// partition.
func TestApplyReopenPartitionMemoGateReopensArgoCDBearingPartition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// No memo rows at all: the ArgoCD-bearing partition (scope-argocd, gen-1) was
	// deliberately never written by the write-side carve-out.
	memoDB := &fifoExecQueryer{
		responses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	memoStore := newDeferredBackfillPartitionMemoStore(memoDB)
	items := []reopenWorkItemRef{
		{WorkItemID: "work-argocd", Partition: scopeGenerationPartition{ScopeID: "scope-argocd", GenerationID: "gen-1"}},
	}

	result, err := applyReopenPartitionMemoGate(ctx, memoStore, "deployment_mapping", items, "sha256:current", nil, nil)
	if err != nil {
		t.Fatalf("applyReopenPartitionMemoGate() error = %v, want nil", err)
	}
	if len(result.ToReopen) != 1 || result.ToReopen[0].WorkItemID != "work-argocd" {
		t.Fatalf("ToReopen = %v, want [work-argocd] (ArgoCD-bearing partition never memoized, always reopens)", result.ToReopen)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want empty for an ArgoCD-bearing partition", result.Skipped)
	}
}

// TestApplyReopenPartitionMemoGateMixedPartitionsSplitsCorrectly proves the
// gate handles a mixed batch: one memo-hit partition skipped, one non-memo-hit
// (stale fingerprint) partition reopened, and one never-memoized (ArgoCD-like)
// partition reopened, all in a single pass.
func TestApplyReopenPartitionMemoGateMixedPartitionsSplitsCorrectly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const fingerprint = "sha256:current"
	memoDB := &fifoExecQueryer{
		responses: []queueFakeRows{
			{rows: [][]any{
				{"scope-hit", "gen-hit", fingerprint},
				{"scope-stale", "gen-stale", "sha256:old"},
			}},
		},
	}
	memoStore := newDeferredBackfillPartitionMemoStore(memoDB)
	items := []reopenWorkItemRef{
		{WorkItemID: "work-hit", Partition: scopeGenerationPartition{ScopeID: "scope-hit", GenerationID: "gen-hit"}},
		{WorkItemID: "work-stale", Partition: scopeGenerationPartition{ScopeID: "scope-stale", GenerationID: "gen-stale"}},
		{WorkItemID: "work-argocd", Partition: scopeGenerationPartition{ScopeID: "scope-argocd", GenerationID: "gen-argocd"}},
	}

	result, err := applyReopenPartitionMemoGate(ctx, memoStore, "deployment_mapping", items, fingerprint, nil, nil)
	if err != nil {
		t.Fatalf("applyReopenPartitionMemoGate() error = %v, want nil", err)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].WorkItemID != "work-hit" {
		t.Fatalf("Skipped = %v, want [work-hit]", result.Skipped)
	}
	gotReopen := map[string]bool{}
	for _, item := range result.ToReopen {
		gotReopen[item.WorkItemID] = true
	}
	if !gotReopen["work-stale"] || !gotReopen["work-argocd"] || len(result.ToReopen) != 2 {
		t.Fatalf("ToReopen = %v, want [work-stale work-argocd]", result.ToReopen)
	}
}

// TestApplyReopenPartitionMemoGateBlankPartitionAlwaysReopens proves a work
// item with a blank scope_id or generation_id (defensive: schema requires
// NOT NULL, but a legacy row or fixture may leave it empty) always reopens
// rather than risk an unintended skip on unrecognized shape.
func TestApplyReopenPartitionMemoGateBlankPartitionAlwaysReopens(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memoDB := &fifoExecQueryer{
		responses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	memoStore := newDeferredBackfillPartitionMemoStore(memoDB)
	items := []reopenWorkItemRef{
		{WorkItemID: "work-blank", Partition: scopeGenerationPartition{ScopeID: "", GenerationID: ""}},
	}

	result, err := applyReopenPartitionMemoGate(ctx, memoStore, "deployment_mapping", items, "sha256:current", nil, nil)
	if err != nil {
		t.Fatalf("applyReopenPartitionMemoGate() error = %v, want nil", err)
	}
	if len(result.ToReopen) != 1 || result.ToReopen[0].WorkItemID != "work-blank" {
		t.Fatalf("ToReopen = %v, want [work-blank]", result.ToReopen)
	}
}

// TestApplyReopenPartitionMemoGateEmptyItemsIsNoop proves an empty input never
// issues a memo lookup query, matching the zero-row short-circuit convention
// applyDeferredPartitionMemoGate and deferredBackfillPartitionMemoStore.LookupMany
// already use.
func TestApplyReopenPartitionMemoGateEmptyItemsIsNoop(t *testing.T) {
	t.Parallel()

	memoStore := newDeferredBackfillPartitionMemoStore(noopExecQueryer{t: t})
	result, err := applyReopenPartitionMemoGate(context.Background(), memoStore, "deployment_mapping", nil, "sha256:current", nil, nil)
	if err != nil {
		t.Fatalf("applyReopenPartitionMemoGate(nil) error = %v, want nil", err)
	}
	if len(result.ToReopen) != 0 || len(result.Skipped) != 0 {
		t.Fatalf("applyReopenPartitionMemoGate(nil) = %+v, want empty result", result)
	}
}

// TestApplyReopenPartitionMemoGateNilMemoStoreFallsBackToReopenAll proves a
// nil memo store (defensive) degrades to the legacy unconditional-reopen
// contract rather than panicking or silently skipping everything.
func TestApplyReopenPartitionMemoGateNilMemoStoreFallsBackToReopenAll(t *testing.T) {
	t.Parallel()

	items := []reopenWorkItemRef{
		{WorkItemID: "work-1", Partition: scopeGenerationPartition{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}
	result, err := applyReopenPartitionMemoGate(context.Background(), nil, "deployment_mapping", items, "sha256:current", nil, nil)
	if err != nil {
		t.Fatalf("applyReopenPartitionMemoGate() error = %v, want nil", err)
	}
	if len(result.ToReopen) != 1 || result.ToReopen[0].WorkItemID != "work-1" {
		t.Fatalf("ToReopen = %v, want [work-1] with a nil memo store", result.ToReopen)
	}
}

// TestApplyReopenPartitionMemoGateLookupErrorPropagates proves a memo lookup
// failure is surfaced to the caller (not silently swallowed) so
// ReopenDeploymentMappingWorkItems/ReopenCodeImportRepoEdgeWorkItems can decide
// how to handle it, matching applyDeferredPartitionMemoGate's own contract of
// returning lookup errors rather than masking them.
func TestApplyReopenPartitionMemoGateLookupErrorPropagates(t *testing.T) {
	t.Parallel()

	memoDB := &fifoExecQueryer{
		responses: []queueFakeRows{
			{err: errors.New("lookup boom")},
		},
	}
	memoStore := newDeferredBackfillPartitionMemoStore(memoDB)
	items := []reopenWorkItemRef{
		{WorkItemID: "work-1", Partition: scopeGenerationPartition{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}

	_, err := applyReopenPartitionMemoGate(context.Background(), memoStore, "deployment_mapping", items, "sha256:current", nil, nil)
	if err == nil {
		t.Fatal("applyReopenPartitionMemoGate() error = nil, want lookup error")
	}
	if !strings.Contains(err.Error(), "lookup reopen partition memos for deployment_mapping") {
		t.Fatalf("error = %q, want domain-scoped lookup context", err.Error())
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

// TestComputeCurrentReopenCatalogFingerprintMatchesDeferredBackfillFingerprint
// proves computeCurrentReopenCatalogFingerprint derives the SAME fingerprint
// shape BackfillAllRelationshipEvidence uses to write the memo table, over an
// identical catalog snapshot — the load-bearing invariant that lets a
// memo-hit computed by this function actually match a row the backfill pass
// just wrote.
func TestComputeCurrentReopenCatalogFingerprintMatchesDeferredBackfillFingerprint(t *testing.T) {
	t.Parallel()

	catalogPayload := []byte(`{"repo_id":"repo-a","name":"alpha"}`)
	got, err := computeCurrentReopenCatalogFingerprint(context.Background(), &fifoExecQueryer{
		responses: []queueFakeRows{{rows: [][]any{{catalogPayload}}}},
	})
	if err != nil {
		t.Fatalf("computeCurrentReopenCatalogFingerprint() error = %v, want nil", err)
	}

	catalog, err := loadRepositoryCatalog(context.Background(), &fifoExecQueryer{
		responses: []queueFakeRows{{rows: [][]any{{catalogPayload}}}},
	})
	if err != nil {
		t.Fatalf("loadRepositoryCatalog() error = %v, want nil", err)
	}
	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("buildDeferredScopedFactQueryParams() ok = false, want true for a non-empty catalog")
	}
	want := deferredCatalogFingerprint(params)

	if got != want {
		t.Fatalf("computeCurrentReopenCatalogFingerprint() = %q, want %q (must match deferredCatalogFingerprint over the same catalog)", got, want)
	}
	if got == "" {
		t.Fatal("computeCurrentReopenCatalogFingerprint() = empty, want a non-empty fingerprint for a non-empty catalog")
	}
}

// TestComputeCurrentReopenCatalogFingerprintEmptyCatalogReturnsEmpty proves an
// empty/unbuildable catalog (buildDeferredScopedFactQueryParams's ok=false
// case) returns an empty fingerprint rather than fabricating one, so the
// reopen gate falls back to the legacy unconditional-reopen contract instead
// of risking a spurious match against an empty-catalog memo row.
func TestComputeCurrentReopenCatalogFingerprintEmptyCatalogReturnsEmpty(t *testing.T) {
	t.Parallel()

	queryer := &fifoExecQueryer{
		responses: []queueFakeRows{{rows: [][]any{}}},
	}
	got, err := computeCurrentReopenCatalogFingerprint(context.Background(), queryer)
	if err != nil {
		t.Fatalf("computeCurrentReopenCatalogFingerprint() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("computeCurrentReopenCatalogFingerprint() = %q, want empty for an empty catalog", got)
	}
}

// TestComputeCurrentReopenCatalogFingerprintNilQueryerReturnsEmpty proves a
// nil queryer (defensive) returns an empty fingerprint without error.
func TestComputeCurrentReopenCatalogFingerprintNilQueryerReturnsEmpty(t *testing.T) {
	t.Parallel()

	got, err := computeCurrentReopenCatalogFingerprint(context.Background(), nil)
	if err != nil {
		t.Fatalf("computeCurrentReopenCatalogFingerprint(nil) error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("computeCurrentReopenCatalogFingerprint(nil) = %q, want empty", got)
	}
}
