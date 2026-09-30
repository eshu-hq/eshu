// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type candidateExplanationReader struct {
	calls int
	query string
	args  []any
	rows  db.Rows
	err   error
}

func (r *candidateExplanationReader) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	r.calls++
	r.query = query
	r.args = args
	return r.rows, r.err
}

type candidateExplanationLegacyDB struct {
	calls int
	err   error
}

func (r *candidateExplanationLegacyDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	r.calls++
	return nil, r.err
}

type candidateExplanationRows struct {
	values []string
	read   bool
	closed bool
}

func (r *candidateExplanationRows) Next() bool {
	if r.read || r.values == nil {
		return false
	}
	r.read = true
	return true
}

func (r *candidateExplanationRows) Scan(dest ...any) error {
	for i, value := range r.values {
		*dest[i].(*string) = value
	}
	return nil
}

func (*candidateExplanationRows) Err() error { return nil }
func (r *candidateExplanationRows) Close() error {
	r.closed = true
	return nil
}

func TestContainerImageCandidateExplanationGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	reader := &candidateExplanationReader{err: want}
	writer := &candidateExplanationLegacyDB{err: errors.New("writer must not be queried")}
	store := NewPostgresContainerImageIdentityStoreWithReadStore(reader)
	store.DB = writer

	_, err := store.ExplainContainerImageCandidate(t.Context(), "registry.example/team/app:latest")
	if !errors.Is(err, want) || reader.calls != 1 || writer.calls != 0 {
		t.Fatalf("error %v, reader calls %d, writer calls %d; want guarded reader error only", err, reader.calls, writer.calls)
	}
	if reader.query != explainContainerImageCandidateQuery || len(reader.args) != 1 || reader.args[0] != "oci-registry://registry.example/team/app" {
		t.Fatalf("query %q, args %#v; want bounded OCI repository query", reader.query, reader.args)
	}
}

func TestContainerImageCandidateExplanationLegacyDB(t *testing.T) {
	want := errors.New("legacy database unavailable")
	legacy := &candidateExplanationLegacyDB{err: want}
	store := NewPostgresContainerImageIdentityStore(legacy)
	_, err := store.ExplainContainerImageCandidate(t.Context(), "registry.example/team/app@sha256:abc")
	if !errors.Is(err, want) || legacy.calls != 1 {
		t.Fatalf("error %v, legacy calls %d; want original DB error and one query", err, legacy.calls)
	}
}

func TestContainerImageCandidateExplanationReaderTruth(t *testing.T) {
	for _, tc := range []struct {
		name       string
		values     []string
		wantReason string
		wantScope  string
	}{
		{"missing OCI target", nil, "oci_registry_target_outside_scope", "outside_configured_targets"},
		{"pending OCI target", []string{"oci-scope", "pending", "generation", "pending", "pending", "", "", ""}, "oci_registry_target_collection_pending", "configured_pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := &candidateExplanationRows{values: tc.values}
			reader := &candidateExplanationReader{rows: rows}
			store := NewPostgresContainerImageIdentityStoreWithReadStore(reader)
			detail, err := store.ExplainContainerImageCandidate(t.Context(), "registry.example/team/app:latest")
			if err != nil || reader.calls != 1 || !rows.closed {
				t.Fatalf("detail %#v, error %v, calls %d, rows closed %t", detail, err, reader.calls, rows.closed)
			}
			if detail["reason"] != tc.wantReason || detail["collector_scope"] != tc.wantScope || detail["candidate_repository_id"] != "oci-registry://registry.example/team/app" {
				t.Fatalf("detail %#v; want reason %q, scope %q, OCI repository", detail, tc.wantReason, tc.wantScope)
			}
		})
	}
}

func TestContainerImageCandidateExplanationUnconfiguredAndNoQueryInputs(t *testing.T) {
	_, err := (PostgresContainerImageIdentityStore{}).ExplainContainerImageCandidate(t.Context(), "registry.example/team/app:latest")
	if err == nil || err.Error() != "container image identity database is required" {
		t.Fatalf("unconfigured store error %v; want required database", err)
	}

	reader := &candidateExplanationReader{err: errors.New("unexpected query")}
	store := NewPostgresContainerImageIdentityStoreWithReadStore(reader)
	for _, imageRef := range []string{"", "registry.example/team/app"} {
		if _, err := store.ExplainContainerImageCandidate(t.Context(), imageRef); err != nil {
			t.Fatalf("image ref %q: %v", imageRef, err)
		}
	}
	if reader.calls != 0 {
		t.Fatalf("guarded queries = %d; want no query for invalid or repository-only references", reader.calls)
	}
}
