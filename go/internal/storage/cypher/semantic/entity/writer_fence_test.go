// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// scriptedGenerationCheck answers writer freshness probes from a script so
// tests can change the answer between groups. When the script runs out, the
// last answer repeats.
type scriptedGenerationCheck struct {
	fresh []bool
	errs  []error
	calls int
}

func (s *scriptedGenerationCheck) check(
	_ context.Context,
	_, _ string,
) (bool, error) {
	index := s.calls
	if index >= len(s.fresh) {
		index = len(s.fresh) - 1
	}
	s.calls++
	if index < len(s.errs) && s.errs[index] != nil {
		return false, s.errs[index]
	}
	return s.fresh[index], nil
}

func stampedSemanticWrite(rows []semantic.EntityRow) semantic.EntityWrite {
	return semantic.EntityWrite{
		RepoIDs:      []string{"repo-1"},
		Rows:         rows,
		ScopeID:      "scope-1",
		GenerationID: "generation-2",
	}
}

// A generation already superseded before the first group must abort before
// any statement dispatches, with the partial result (zero dispatched rows)
// and the sentinel, never a silent success.
func TestSemanticEntityWriterFenceAbortsBeforeFirstGroup(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	check := &scriptedGenerationCheck{fresh: []bool{false}}
	writer := NewSemanticEntityWriter(executor, 2).WithGenerationCheck(check.check)

	result, err := writer.WriteSemanticEntities(
		context.Background(),
		stampedSemanticWrite(semanticFunctionRows("repo-1", 4)),
	)
	if !errors.Is(err, semantic.ErrGenerationSuperseded) {
		t.Fatalf("WriteSemanticEntities() error = %v, want ErrGenerationSuperseded", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("dispatched %d groups, want 0 for a pre-write superseded generation", len(executor.groupCalls))
	}
	if result.CanonicalWrites != 0 {
		t.Errorf("CanonicalWrites = %d, want 0 (no rows dispatched)", result.CanonicalWrites)
	}
	if result.Groups != 0 {
		t.Errorf("Groups = %d, want 0", result.Groups)
	}
	if check.calls != 1 {
		t.Errorf("freshness check calls = %d, want 1 (fence before the first group)", check.calls)
	}
}

// A generation superseded mid-write aborts after the dispatched groups: the
// partial CanonicalWrites counts dispatched rows only, and the error wraps
// the sentinel.
func TestSemanticEntityWriterFenceAbortsAfterFirstGroupWithPartialWrites(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	check := &scriptedGenerationCheck{fresh: []bool{true, false}}
	writer := NewSemanticEntityWriter(executor, 2).
		WithMaxGroupRows(3).
		WithGenerationCheck(check.check)

	result, err := writer.WriteSemanticEntities(
		context.Background(),
		stampedSemanticWrite(semanticFunctionRows("repo-1", 8)),
	)
	if !errors.Is(err, semantic.ErrGenerationSuperseded) {
		t.Fatalf("WriteSemanticEntities() error = %v, want ErrGenerationSuperseded", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("dispatched %d groups, want 1 (abort before the second group)", len(executor.groupCalls))
	}
	if result.Groups != 1 {
		t.Errorf("Groups = %d, want 1 dispatched group", result.Groups)
	}
	dispatched := 0
	for _, group := range executor.groupCalls {
		dispatched += semanticLabeledStatementRows(group)
	}
	if result.CanonicalWrites != dispatched {
		t.Errorf("CanonicalWrites = %d, want %d dispatched rows only", result.CanonicalWrites, dispatched)
	}
	if dispatched == 0 || dispatched >= 8 {
		t.Errorf("dispatched rows = %d, want a strict partial of the 8 admitted rows", dispatched)
	}
}

// A fence lookup error — including GenerationNotYetActiveError — aborts the
// write wrapped with %w, never as supersession: a pending generation is
// about to activate, not superseded, so the queue must retry it.
func TestSemanticEntityWriterFenceErrorAbortsWrapped(t *testing.T) {
	t.Parallel()

	pending := reducercontract.GenerationNotYetActiveError{
		ScopeID:      "scope-1",
		GenerationID: "generation-2",
	}
	executor := &recordingGroupExecutor{}
	check := &scriptedGenerationCheck{fresh: []bool{true}, errs: []error{pending}}
	writer := NewSemanticEntityWriter(executor, 2).WithGenerationCheck(check.check)

	_, err := writer.WriteSemanticEntities(
		context.Background(),
		stampedSemanticWrite(semanticFunctionRows("repo-1", 2)),
	)
	if err == nil {
		t.Fatalf("WriteSemanticEntities() error = nil, want the wrapped not-yet-active error")
	}
	if !errors.Is(err, reducercontract.ErrGenerationNotYetActive) {
		t.Errorf("WriteSemanticEntities() error = %v, must unwrap to ErrGenerationNotYetActive", err)
	}
	if errors.Is(err, semantic.ErrGenerationSuperseded) {
		t.Errorf("WriteSemanticEntities() error = %v, a pending generation must not read as superseded", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Errorf("dispatched %d groups, want 0 when the fence errors", len(executor.groupCalls))
	}
}

// A stale fence verdict on an unstamped write is ambiguous — the probe ran
// with empty IDs — so it fails closed with an error instead of silently
// dropping the write as superseded or writing it anyway.
func TestSemanticEntityWriterFencedUnstampedWriteFailsClosed(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	check := &scriptedGenerationCheck{fresh: []bool{false}}
	writer := NewSemanticEntityWriter(executor, 2).WithGenerationCheck(check.check)

	_, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    semanticFunctionRows("repo-1", 2),
	})
	if err == nil {
		t.Fatalf("WriteSemanticEntities() error = nil, want the unstamped-fence error")
	}
	if errors.Is(err, semantic.ErrGenerationSuperseded) {
		t.Errorf("WriteSemanticEntities() error = %v, an unstamped write must error, not supersede", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Errorf("dispatched %d groups, want 0 for an unstamped fenced write", len(executor.groupCalls))
	}
}

// Without a wired check the writer keeps the legacy run-to-completion path:
// no stamp required, every group dispatches.
func TestSemanticEntityWriterNilCheckKeepsLegacyBehavior(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewSemanticEntityWriter(executor, 2).WithMaxGroupRows(3)

	result, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    semanticFunctionRows("repo-1", 8),
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v, want nil without a fence", err)
	}
	if result.CanonicalWrites != 8 {
		t.Errorf("CanonicalWrites = %d, want 8", result.CanonicalWrites)
	}
	if len(executor.groupCalls) < 2 {
		t.Errorf("dispatched %d groups, want the full split write", len(executor.groupCalls))
	}
}

// A retract-only write still passes the fence: the first group holds the
// retract, so a superseded retract-only run must abort before wiping the
// active generation's nodes.
func TestSemanticEntityWriterFenceCoversRetractOnlyWrite(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	check := &scriptedGenerationCheck{fresh: []bool{false}}
	writer := NewSemanticEntityWriter(executor, 2).WithGenerationCheck(check.check)

	_, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs:      []string{"repo-1"},
		ScopeID:      "scope-1",
		GenerationID: "generation-2",
	})
	if !errors.Is(err, semantic.ErrGenerationSuperseded) {
		t.Fatalf("WriteSemanticEntities() error = %v, want ErrGenerationSuperseded", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("dispatched %d groups, want 0 for a superseded retract-only run", len(executor.groupCalls))
	}
}

// Executors without ExecuteGroup take the per-statement fallback: the fence
// still runs before every dispatch, and the partial counts dispatched rows.
func TestSemanticEntityWriterFenceCoversFallbackDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	// Three retracts lead, then one singleton upsert per row: the fence
	// fires before the third upsert, so two rows dispatch.
	check := &scriptedGenerationCheck{fresh: []bool{true, true, true, true, true, false}}
	writer := NewSemanticEntityWriterWithParameterizedRows(executor, 100).
		WithGenerationCheck(check.check)

	result, err := writer.WriteSemanticEntities(
		context.Background(),
		stampedSemanticWrite(semanticFunctionRows("repo-1", 4)),
	)
	if !errors.Is(err, semantic.ErrGenerationSuperseded) {
		t.Fatalf("WriteSemanticEntities() error = %v, want ErrGenerationSuperseded", err)
	}
	if got, want := len(executor.calls), 5; got != want {
		t.Fatalf("dispatched %d statements, want %d (3 retracts + 2 upserts)", got, want)
	}
	if result.CanonicalWrites != 2 {
		t.Errorf("CanonicalWrites = %d, want 2 dispatched rows", result.CanonicalWrites)
	}
}

// The empty-write early return stays first: an empty write returns success
// without probing the fence at all.
func TestSemanticEntityWriterEmptyWriteSkipsFence(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	boom := errors.New("fence must not run on an empty write")
	check := &scriptedGenerationCheck{fresh: []bool{true}, errs: []error{boom}}
	writer := NewSemanticEntityWriter(executor, 2).WithGenerationCheck(check.check)

	result, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v, want nil for an empty write", err)
	}
	if result.CanonicalWrites != 0 || result.Groups != 0 {
		t.Errorf("WriteSemanticEntities() result = %+v, want empty", result)
	}
	if check.calls != 0 {
		t.Errorf("freshness check calls = %d, want 0 for an empty write", check.calls)
	}
}

// The partial-count rule keys on the per-statement entity label: every
// parameterized-rows upsert carries it, the ownership statement does not, so
// dispatched ownership rows never inflate a partial CanonicalWrites.
func TestSemanticEntityWriterPartialCountKeysOnStatementLabel(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriterWithParameterizedRows(executor, 100)

	rows := []semantic.EntityRow{
		{
			RepoID:     "repo-1",
			EntityID:   "impl-1",
			EntityType: "ImplBlock",
			EntityName: "Target",
			FilePath:   "/repo/src/lib.rs",
			Language:   "rust",
			StartLine:  3,
			EndLine:    9,
		},
		{
			RepoID:     "repo-1",
			EntityID:   "func-1",
			EntityType: "Function",
			EntityName: "Target",
			FilePath:   "/repo/src/lib.rs",
			Language:   "rust",
			StartLine:  4,
			EndLine:    8,
			Metadata:   map[string]any{"impl_context": "Target"},
		},
	}
	if _, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    rows,
	}); err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}

	labeled, unlabeled := 0, 0
	for _, stmt := range executor.calls {
		if stmt.Operation != sourcecypher.OperationCanonicalUpsert {
			continue
		}
		if _, ok := stmt.Parameters[sourcecypher.StatementMetadataEntityLabelKey]; ok {
			labeled++
		} else {
			unlabeled++
		}
	}
	// Both singleton entity upserts carry the label key; the ownership
	// statement dispatches too but carries none, so the labeled-row count
	// covers the two entity rows and nothing else.
	if labeled != 2 {
		t.Errorf("labeled upserts = %d, want 2 (one per parameterized row)", labeled)
	}
	if unlabeled != 1 {
		t.Errorf("unlabeled upserts = %d, want 1 (the ownership statement)", unlabeled)
	}
	if got, want := semanticLabeledStatementRows(executor.calls), 2; got != want {
		t.Errorf("semanticLabeledStatementRows() = %d, want %d", got, want)
	}
}

// Abort-then-converge: a run superseded after group 1 aborts, then a
// current-generation run replays every group byte-identically to convergence.
// Upserts are MERGE-on-identity, so the replayed first group overwrites the
// same uids instead of duplicating them.
func TestSemanticEntityWriterAbortThenConverge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	write := stampedSemanticWrite(semanticFunctionRows("repo-1", 8))

	first := &recordingGroupExecutor{}
	superseded := &scriptedGenerationCheck{fresh: []bool{true, false}}
	_, err := NewSemanticEntityWriter(first, 2).
		WithMaxGroupRows(3).
		WithGenerationCheck(superseded.check).
		WriteSemanticEntities(ctx, write)
	if !errors.Is(err, semantic.ErrGenerationSuperseded) {
		t.Fatalf("first run error = %v, want ErrGenerationSuperseded", err)
	}
	if len(first.groupCalls) != 1 {
		t.Fatalf("first run dispatched %d groups, want 1 before the abort", len(first.groupCalls))
	}

	second := &recordingGroupExecutor{}
	fresh := &scriptedGenerationCheck{fresh: []bool{true}}
	converged, err := NewSemanticEntityWriter(second, 2).
		WithMaxGroupRows(3).
		WithGenerationCheck(fresh.check).
		WriteSemanticEntities(ctx, write)
	if err != nil {
		t.Fatalf("second run error = %v, want nil", err)
	}
	if converged.CanonicalWrites != 8 {
		t.Errorf("second run CanonicalWrites = %d, want 8", converged.CanonicalWrites)
	}
	// The replayed first group is statement-identical to the aborted run's
	// group: same MERGE-on-identity Cypher, same row parameters, so the
	// same uids are overwritten, never duplicated.
	if len(second.groupCalls) == 0 {
		t.Fatalf("second run dispatched 0 groups, want the full write")
	}
	replayed := second.groupCalls[0]
	original := first.groupCalls[0]
	if len(replayed) != len(original) {
		t.Fatalf("replayed group holds %d statements, aborted group held %d", len(replayed), len(original))
	}
	for i := range replayed {
		if replayed[i].Cypher != original[i].Cypher {
			t.Errorf("replayed statement %d cypher differs from the aborted run", i)
		}
	}
	// The converged run covers every admitted row exactly once across its
	// own groups.
	covered := 0
	for _, group := range second.groupCalls {
		covered += semanticLabeledStatementRows(group)
	}
	if covered != 8 {
		t.Errorf("converged run covers %d labeled rows, want 8", covered)
	}
}
