// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingImpactReader struct {
	queries int
	err     error
}

func (r *rejectingImpactReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.queries++
	return nil, r.err
}

func (r *rejectingImpactReader) QueryRowContext(context.Context, string, ...any) db.Row {
	r.queries++
	return rejectingImpactRow{err: r.err}
}

type rejectingImpactRow struct{ err error }

func (r rejectingImpactRow) Scan(...any) error { return r.err }

func (*rejectingImpactReader) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, errors.New("snapshot not needed")
}

type findingThenRejectingImpactReader struct {
	queries int
	err     error
}

func (r *findingThenRejectingImpactReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.queries++
	if r.queries == 1 {
		return &oneImpactFindingRows{}, nil
	}
	return nil, r.err
}

type oneImpactFindingRows struct{ read bool }

func (r *oneImpactFindingRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}

func (*oneImpactFindingRows) Scan(dest ...any) error {
	*dest[0].(*string) = "fact:abc"
	*dest[1].(*string) = "source"
	*dest[2].(*[]byte) = []byte(`{"finding_id":"finding:abc","evidence_fact_ids":["fact:evidence"]}`)
	return nil
}

func (*oneImpactFindingRows) Err() error   { return nil }
func (*oneImpactFindingRows) Close() error { return nil }

func TestImpactFindingsGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingImpactReader{err: want}
	store := NewPostgresFindingStoreWithReadStore(guard, true)
	_, err := store.ListSupplyChainImpactFindings(t.Context(), FindingFilter{CVEID: "CVE-2026-1234", Limit: 1})
	if !errors.Is(err, want) || guard.queries != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.queries)
	}
}

func TestImpactAggregatesGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingImpactReader{err: want}
	store := NewPostgresAggregateStoreWithReadStore(guard)
	_, err := store.CountSupplyChainImpactFindings(t.Context(), AggregateFilter{CVEID: "CVE-2026-1234"})
	if !errors.Is(err, want) || guard.queries != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one row", err, guard.queries)
	}
}

func TestImpactRuntimeContextGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingImpactReader{err: want}
	store := NewPostgresFindingStoreWithReadStore(guard, false)
	_, err := store.ListSupplyChainImpactRuntimeContext(t.Context(), []string{"repository:r_217415d9"}, nil, nil)
	if !errors.Is(err, want) || guard.queries != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.queries)
	}
}

func TestImpactRuntimeEnvironmentGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingImpactReader{err: want}
	store := NewPostgresFindingStoreWithReadStore(guard, false)
	_, err := store.ListSupplyChainImpactRuntimeEnvironmentEvidence(
		t.Context(),
		[]RuntimeEnvironmentCandidate{{SubjectDigest: "sha256:abc", Environment: "production"}},
		nil,
		nil,
	)
	if !errors.Is(err, want) || guard.queries != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.queries)
	}
}

func TestImpactExplanationGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingImpactReader{err: want}
	store := NewPostgresFindingStoreWithReadStore(guard, false)
	_, err := store.ExplainSupplyChainImpact(t.Context(), ExplanationFilter{FindingID: "finding:abc"})
	if !errors.Is(err, want) || guard.queries != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.queries)
	}
}

func TestImpactExplanationEvidenceGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingImpactReader{err: want}
	store := NewPostgresFindingStoreWithReadStore(guard, false)
	_, err := store.loadSupplyChainImpactEvidenceFacts(t.Context(), []string{"fact:abc"})
	if !errors.Is(err, want) || guard.queries != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.queries)
	}
}

func TestImpactExplanationHydratesEvidenceThroughGuardedReadPort(t *testing.T) {
	want := errors.New("reader became stale during evidence hydration")
	guard := &findingThenRejectingImpactReader{err: want}
	store := NewPostgresFindingStoreWithReadStore(guard, false)
	_, err := store.ExplainSupplyChainImpact(t.Context(), ExplanationFilter{FindingID: "finding:abc"})
	if !errors.Is(err, want) || guard.queries != 2 {
		t.Fatalf("error %v, guarded queries %d; want evidence reader error after finding query", err, guard.queries)
	}
}
