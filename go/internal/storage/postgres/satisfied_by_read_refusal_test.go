// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// crossplaneClaimFakeRow is one stub claim row of repository:repo-1's active
// delta generation-1, in the reader's scan order.
func crossplaneClaimFakeRow() []any {
	return []any{
		"fact-claim-1", "repository:repo-1", "generation-1", "content_entity", "content_entity:claim-1",
		"1.0.0", "git", int64(0), "unknown", "git", "content_entity:claim-1",
		"file:///repo/deploy/claim.yaml", "claim-1",
		time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC), false,
		[]byte(`{"entity_type":"K8sResource","entity_id":"claim-1","relative_path":"deploy/claim.yaml","entity_metadata":{"api_version":"delta.acme.internal/v1alpha1","kind":"DeltaQueue"}}`),
	}
}

// crossplaneRefusalTarget is the delta-target lookup row for repo-1.
var crossplaneRefusalTarget = queueFakeRows{rows: [][]any{{"repository:repo-1", "generation-1", "c0", ""}}}

// TestListActiveCrossplaneSatisfiedByFactsFailsOnAMissingBase pins #10005 F1
// on a stub queryer. The intent scope is delta-active and the base lookup
// finds no full generation at its commit: the read fails with the
// non-retryable CrossplaneDeltaBaseRefusedError before the XRD leg runs, so
// the handler never retracts. The manifest reader on the same stub keeps
// its delta-only rows: the fail-closed posture is the Crossplane read's
// alone.
func TestListActiveCrossplaneSatisfiedByFactsFailsOnAMissingBase(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{crossplaneClaimFakeRow()}}, // claim active page
		crossplaneRefusalTarget,                   // delta-target lookup
		{},                                        // claim base page
		{},                                        // base lookup: no base
	}}
	_, err := NewFactStore(db).ListActiveCrossplaneSatisfiedByFacts(context.Background(), "repository:repo-1")
	var refused *CrossplaneDeltaBaseRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("read error = %v, want *CrossplaneDeltaBaseRefusedError", err)
	}
	if refused.ScopeID != "repository:repo-1" || refused.GenerationID != "generation-1" ||
		refused.Read != crossplaneSatisfiedByReadClaim || refused.Outcome != deltaOverlayBaseMissing ||
		refused.BaselineCommitSHA != "c0" || refused.UnionBaseCommitSHA != "c0" {
		t.Fatalf("refusal = %+v, want repo-1 generation-1 claim union_base_missing at c0", refused)
	}
	if reducercontract.IsRetryable(err) {
		t.Fatal("IsRetryable = true, want false: a retry reads the same refused base")
	}
	if got := refused.FailureClass(); got != CrossplaneDeltaBaseRefusedFailureClass {
		t.Fatalf("FailureClass = %q, want %q", got, CrossplaneDeltaBaseRefusedFailureClass)
	}
	if got := len(db.queries); got != 4 {
		t.Fatalf("statements = %d, want 4: the XRD leg must not run after a refusal", got)
	}

	manifest := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{manifestDependencyFakeRow(0)}}, // active page
		crossplaneRefusalTarget,                       // delta-target lookup
		{},                                            // delta-base page
		{},                                            // base lookup: no base
	}}
	loaded, err := NewFactStore(manifest).ListActivePackageManifestDependencyFacts(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	)
	if err != nil || len(loaded) != 1 {
		t.Fatalf("manifest read = %d rows, err %v; want its 1 delta row and no error", len(loaded), err)
	}
}

// TestListActiveCrossplaneSatisfiedByFactsRetriesAFailedProbe pins the probe
// refinement of #10005 F1: the base resolves to a superseded full generation
// and the retention-probe statement fails. The base may be intact, so the
// read fails with a retryable error that carries no failure class of its own
// and wraps the probe failure, instead of the dead-lettering refusal.
func TestListActiveCrossplaneSatisfiedByFactsRetriesAFailedProbe(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{crossplaneClaimFakeRow()}}, // claim active page
		crossplaneRefusalTarget,                   // delta-target lookup
		{},                                        // claim base page
		{rows: [][]any{{"repository:repo-1", "base-1", "superseded"}}}, // base lookup
		{err: context.DeadlineExceeded},                                // retention probe
	}}
	_, err := NewFactStore(db).ListActiveCrossplaneSatisfiedByFacts(context.Background(), "repository:repo-1")
	if err == nil {
		t.Fatal("read error = nil, want the retryable probe failure")
	}
	if !reducercontract.IsRetryable(err) {
		t.Fatalf("IsRetryable(%v) = false, want true for a failed probe statement", err)
	}
	var refused *CrossplaneDeltaBaseRefusedError
	if errors.As(err, &refused) {
		t.Fatalf("read error = %v, want no CrossplaneDeltaBaseRefusedError for a probe failure", err)
	}
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) {
		t.Fatalf("read error carries failure class %q, want none", classified.FailureClass())
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read error = %v, want it to wrap the probe failure", err)
	}
}
