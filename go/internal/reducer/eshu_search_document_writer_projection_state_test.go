// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// fakeSearchDocumentProjectionStateWriter records projection-state lifecycle calls.
type fakeSearchDocumentProjectionStateWriter struct {
	beginBuildingCalls []fakeSearchDocProjectionStateBeginCall
	finalizeReadyCalls []fakeSearchDocProjectionStateFinalizeCall
	markFailedCalls    []fakeSearchDocProjectionStateMarkFailedCall
	beginBuildingErr   error
	finalizeReadyOK    bool
	finalizeReadyErr   error
}

type fakeSearchDocProjectionStateBeginCall struct {
	scopeID  string
	genID    string
	revision int64
	fence    int64
}

type fakeSearchDocProjectionStateFinalizeCall struct {
	scopeID       string
	genID         string
	revision      int64
	fence         int64
	documentCount int64
}

type fakeSearchDocProjectionStateMarkFailedCall struct {
	scopeID  string
	genID    string
	revision int64
	fence    int64
}

func (f *fakeSearchDocumentProjectionStateWriter) BeginBuilding(_ context.Context, scopeID, generationID string) (revision, fence int64, err error) {
	f.beginBuildingCalls = append(f.beginBuildingCalls, fakeSearchDocProjectionStateBeginCall{scopeID: scopeID, genID: generationID, revision: int64(len(f.beginBuildingCalls) + 1), fence: int64((len(f.beginBuildingCalls) + 1) * 10)})
	if f.beginBuildingErr != nil {
		return 0, 0, f.beginBuildingErr
	}
	return f.beginBuildingCalls[len(f.beginBuildingCalls)-1].revision, f.beginBuildingCalls[len(f.beginBuildingCalls)-1].fence, nil
}

func (f *fakeSearchDocumentProjectionStateWriter) FinalizeReady(_ context.Context, scopeID, generationID string, revision, fence, documentCount int64) (bool, error) {
	f.finalizeReadyCalls = append(f.finalizeReadyCalls, fakeSearchDocProjectionStateFinalizeCall{scopeID: scopeID, genID: generationID, revision: revision, fence: fence, documentCount: documentCount})
	if f.finalizeReadyErr != nil {
		return false, f.finalizeReadyErr
	}
	return f.finalizeReadyOK, nil
}

func (f *fakeSearchDocumentProjectionStateWriter) MarkFailed(_ context.Context, scopeID, generationID string, revision, fence int64) (bool, error) {
	f.markFailedCalls = append(f.markFailedCalls, fakeSearchDocProjectionStateMarkFailedCall{scopeID: scopeID, genID: generationID, revision: revision, fence: fence})
	return true, nil
}

func TestSearchDocWriterBeginCallsProjectionStateBeginBuilding(t *testing.T) {
	t.Parallel()

	projState := &fakeSearchDocumentProjectionStateWriter{}
	db := &fakeSearchDocExecer{retireAffected: 0}
	writer := PostgresEshuSearchDocumentWriter{
		DB:              db,
		ProjectionState: projState,
		Now:             func() time.Time { return time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC) },
	}
	session, err := writer.BeginEshuSearchDocumentWrite(context.Background(), EshuSearchDocumentWriteBegin{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
	})
	if err != nil {
		t.Fatalf("BeginEshuSearchDocumentWrite error = %v", err)
	}
	if session == nil {
		t.Fatal("session = nil")
	}
	if len(projState.beginBuildingCalls) != 1 {
		t.Fatalf("begin building calls = %d, want 1", len(projState.beginBuildingCalls))
	}
	if got, want := projState.beginBuildingCalls[0].scopeID, "scope-1"; got != want {
		t.Errorf("begin scope = %q, want %q", got, want)
	}
}

func TestSearchDocWriterBeginProjectionStateErrorFailsSession(t *testing.T) {
	t.Parallel()

	projState := &fakeSearchDocumentProjectionStateWriter{beginBuildingErr: errors.New("db down")}
	db := &fakeSearchDocExecer{}
	writer := PostgresEshuSearchDocumentWriter{
		DB:              db,
		ProjectionState: projState,
	}
	_, err := writer.BeginEshuSearchDocumentWrite(context.Background(), EshuSearchDocumentWriteBegin{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
	})
	if err == nil {
		t.Fatal("expected error from projection state BeginBuilding failure")
	}
	if len(db.execs) > 0 {
		t.Fatalf("mutations after failed projection state = %d, want 0", len(db.execs))
	}
}

func TestSearchDocWriterFinalizeCallsProjectionStateFinalizeReady(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	projState := &fakeSearchDocumentProjectionStateWriter{finalizeReadyOK: true}
	db := &fakeSearchDocExecer{}
	writer := PostgresEshuSearchDocumentWriter{
		DB:              db,
		ProjectionState: projState,
		Now:             func() time.Time { return now },
	}
	result, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents:    []searchdocs.Document{sampleSearchDoc("searchdoc:content_entity:e-1")},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}
	if result.CanonicalWrites != 1 {
		t.Errorf("canonical writes = %d, want 1", result.CanonicalWrites)
	}
	if len(projState.beginBuildingCalls) != 1 {
		t.Fatalf("begin building calls = %d, want 1", len(projState.beginBuildingCalls))
	}
	if len(projState.finalizeReadyCalls) != 1 {
		t.Fatalf("finalize ready calls = %d, want 1", len(projState.finalizeReadyCalls))
	}
	fc := projState.finalizeReadyCalls[0]
	if got, want := fc.scopeID, "scope-1"; got != want {
		t.Errorf("finalize scope = %q, want %q", got, want)
	}
	if got, want := fc.documentCount, int64(1); got != want {
		t.Errorf("document count = %d, want %d", got, want)
	}
	// revision and fence must match what BeginBuilding returned.
	if got, want := fc.revision, projState.beginBuildingCalls[0].revision; got != want {
		t.Errorf("revision = %d, want %d", got, want)
	}
	if got, want := fc.fence, projState.beginBuildingCalls[0].fence; got != want {
		t.Errorf("fence = %d, want %d", got, want)
	}
}

func TestSearchDocWriterFinalizeFalseCASDoesNotFailWrite(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	projState := &fakeSearchDocumentProjectionStateWriter{finalizeReadyOK: false} // CAS rejected
	db := &fakeSearchDocExecer{}
	writer := PostgresEshuSearchDocumentWriter{
		DB:              db,
		ProjectionState: projState,
		Now:             func() time.Time { return now },
	}
	result, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents:    []searchdocs.Document{sampleSearchDoc("searchdoc:content_entity:e-1")},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v, want nil (false CAS must not fail write)", err)
	}
	if result.CanonicalWrites != 1 {
		t.Errorf("canonical writes = %d, want 1 (write still succeeds)", result.CanonicalWrites)
	}
	if len(projState.finalizeReadyCalls) != 1 {
		t.Fatalf("finalize ready calls = %d, want 1", len(projState.finalizeReadyCalls))
	}
}

func TestSearchDocWriterCancelCallsProjectionStateMarkFailed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	projState := &fakeSearchDocumentProjectionStateWriter{}
	db := &fakeSearchDocExecer{}
	writer := PostgresEshuSearchDocumentWriter{
		DB:              db,
		ProjectionState: projState,
		Now:             func() time.Time { return now },
	}
	session, err := writer.BeginEshuSearchDocumentWrite(context.Background(), EshuSearchDocumentWriteBegin{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
	})
	if err != nil {
		t.Fatalf("BeginEshuSearchDocumentWrite error = %v", err)
	}
	if err := session.Cancel(context.Background()); err != nil {
		t.Fatalf("Cancel error = %v", err)
	}
	if len(projState.markFailedCalls) != 1 {
		t.Fatalf("mark failed calls = %d, want 1", len(projState.markFailedCalls))
	}
	mf := projState.markFailedCalls[0]
	if got, want := mf.scopeID, "scope-1"; got != want {
		t.Errorf("mark failed scope = %q, want %q", got, want)
	}
	if got, want := mf.revision, projState.beginBuildingCalls[0].revision; got != want {
		t.Errorf("revision = %d, want %d", got, want)
	}
}

func TestSearchDocWriterNilProjectionStateByteIdentical(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeSearchDocExecer{retireAffected: 0}
	writer := PostgresEshuSearchDocumentWriter{DB: db, Now: func() time.Time { return now }}
	result, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents:    []searchdocs.Document{sampleSearchDoc("searchdoc:content_entity:e-1")},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}
	if result.CanonicalWrites != 1 {
		t.Errorf("canonical writes = %d, want 1", result.CanonicalWrites)
	}
}
