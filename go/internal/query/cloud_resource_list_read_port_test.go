// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectedCloudResourceQueryer struct {
	called int
	err    error
}

func (q *rejectedCloudResourceQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	q.called++
	return nil, q.err
}

func TestCloudResourceListGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectedCloudResourceQueryer{err: want}
	store := NewPostgresCloudResourceListStoreWithReadStore(guard)
	_, err := store.ListCloudResourceIdentities(context.Background(), CloudResourceListPageFilter{
		AllScopes: true,
		Limit:     1,
	})
	if !errors.Is(err, want) || guard.called != 1 {
		t.Fatalf("read error = %v, guarded queries = %d; want stale reader and one guarded query", err, guard.called)
	}
}

func TestContainerIdentityGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectedCloudResourceQueryer{err: want}
	store := NewPostgresContainerImageIdentityStoreWithReadStore(guard)
	_, err := store.ListContainerImageIdentities(context.Background(), ContainerImageIdentityFilter{
		Digest: "sha256:abc",
		Limit:  1,
	})
	if !errors.Is(err, want) || guard.called != 1 {
		t.Fatalf("read error = %v, guarded queries = %d; want stale reader and one guarded query", err, guard.called)
	}
}

func TestCollectorConfiguredGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectedCloudResourceQueryer{err: want}
	store := NewPostgresCollectorListReadinessStoreWithReadStore(guard)
	_, err := store.CollectorConfigured(context.Background(), "aws")
	if !errors.Is(err, want) || guard.called != 1 {
		t.Fatalf("read error = %v, guarded queries = %d; want stale reader and one guarded query", err, guard.called)
	}
}

func TestSBOMAttachmentGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectedCloudResourceQueryer{err: want}
	store := NewPostgresSBOMAttestationAttachmentStoreWithReadStore(guard)
	_, err := store.ListSBOMAttestationAttachments(context.Background(), SBOMAttestationAttachmentFilter{
		SubjectDigest: "sha256:abc",
		Limit:         1,
	})
	if !errors.Is(err, want) || guard.called != 1 {
		t.Fatalf("read error = %v, guarded queries = %d; want stale reader and one guarded query", err, guard.called)
	}
}

func TestAdmissionDecisionGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectedCloudResourceQueryer{err: want}
	store := NewPostgresAdmissionDecisionReadStoreWithReadStore(guard)
	_, err := store.ListAdmissionDecisions(context.Background(), AdmissionDecisionReadFilter{
		Domain: "test", ScopeID: "scope", GenerationID: "generation", Limit: 1,
	})
	if !errors.Is(err, want) || guard.called != 1 {
		t.Fatalf("read error = %v, guarded queries = %d; want stale reader and one guarded query", err, guard.called)
	}
}

type rejectingAggregateReadStore struct {
	calls int
	err   error
}

func (q *rejectingAggregateReadStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	q.calls++
	return nil, q.err
}

func (q *rejectingAggregateReadStore) QueryRowContext(context.Context, string, ...any) db.Row {
	q.calls++
	return rejectingAggregateRow{err: q.err}
}

func (*rejectingAggregateReadStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, errors.New("unexpected snapshot")
}

type rejectingAggregateRow struct{ err error }

func (r rejectingAggregateRow) Scan(...any) error { return r.err }

func TestBusinessAggregateGuardedReadPorts(t *testing.T) {
	want := errors.New("reader is stale")
	for _, tc := range []struct {
		name string
		call func(*rejectingAggregateReadStore) error
	}{
		{"container identity", func(reader *rejectingAggregateReadStore) error {
			store := NewPostgresContainerImageIdentityAggregateStoreWithReadStore(reader)
			_, err := store.CountContainerImageIdentities(t.Context(), ContainerImageIdentityAggregateFilter{})
			return err
		}},
		{"sbom attachment", func(reader *rejectingAggregateReadStore) error {
			store := NewPostgresSBOMAttestationAttachmentAggregateStoreWithReadStore(reader)
			_, err := store.CountSBOMAttestationAttachments(t.Context(), SBOMAttestationAttachmentAggregateFilter{})
			return err
		}},
		{"documentation finding", func(reader *rejectingAggregateReadStore) error {
			store := NewPostgresDocumentationFindingAggregateStoreWithReadStore(reader)
			_, err := store.CountDocumentationFindings(t.Context(), DocumentationFindingAggregateFilter{})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &rejectingAggregateReadStore{err: want}
			if err := tc.call(reader); !errors.Is(err, want) || reader.calls != 1 {
				t.Fatalf("error %v, guarded calls %d; want stale reader and one call", err, reader.calls)
			}
		})
	}
}

func TestBusinessAggregateInventoryGuardedReadPorts(t *testing.T) {
	want := errors.New("reader is stale")
	for _, tc := range []struct {
		name string
		call func(*rejectingAggregateReadStore) error
	}{
		{"container identity", func(reader *rejectingAggregateReadStore) error {
			store := NewPostgresContainerImageIdentityAggregateStoreWithReadStore(reader)
			_, err := store.ContainerImageIdentityInventory(t.Context(), ContainerImageIdentityAggregateFilter{}, ContainerImageIdentityInventoryByOutcome, 1, 0)
			return err
		}},
		{"sbom attachment", func(reader *rejectingAggregateReadStore) error {
			store := NewPostgresSBOMAttestationAttachmentAggregateStoreWithReadStore(reader)
			_, err := store.SBOMAttestationAttachmentInventory(t.Context(), SBOMAttestationAttachmentAggregateFilter{}, SBOMAttestationAttachmentInventoryByAttachmentStatus, 1, 0)
			return err
		}},
		{"documentation finding", func(reader *rejectingAggregateReadStore) error {
			store := NewPostgresDocumentationFindingAggregateStoreWithReadStore(reader)
			_, err := store.DocumentationFindingInventory(t.Context(), DocumentationFindingAggregateFilter{}, DocumentationFindingInventoryByStatus, 1, 0)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &rejectingAggregateReadStore{err: want}
			if err := tc.call(reader); !errors.Is(err, want) || reader.calls != 1 {
				t.Fatalf("error %v, guarded calls %d; want stale reader and one call", err, reader.calls)
			}
		})
	}
}

func TestMultiCloudRuntimeDriftGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectedCloudResourceQueryer{err: want}
	store := NewPostgresMultiCloudRuntimeDriftStoreWithReadStore(guard)
	_, err := store.ListActiveMultiCloudRuntimeDriftFindings(t.Context(), MultiCloudRuntimeDriftFilter{ScopeID: "scope", Limit: 1})
	if !errors.Is(err, want) || guard.called != 1 {
		t.Fatalf("error %v, guarded calls %d; want stale reader and one call", err, guard.called)
	}
}
