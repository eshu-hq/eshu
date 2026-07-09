// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
)

func TestResolveBootstrapCredentialOwnerReturnsUserIDAndSubjectHash(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"sha256:owner-subject"}}},
			{rows: [][]any{{"user-id-1"}}},
		},
	}
	store := NewIdentitySubjectStore(db)

	userID, subjectIDHash, err := store.ResolveBootstrapCredentialOwner(context.Background(), "tenant_local", "workspace_local")
	if err != nil {
		t.Fatalf("ResolveBootstrapCredentialOwner() error = %v", err)
	}
	if userID != "user-id-1" {
		t.Fatalf("userID = %q, want %q", userID, "user-id-1")
	}
	if subjectIDHash != "sha256:owner-subject" {
		t.Fatalf("subjectIDHash = %q, want %q", subjectIDHash, "sha256:owner-subject")
	}
	if len(db.queries) != 2 {
		t.Fatalf("queries = %d, want 2 (subject lookup then owner lookup): %#v", len(db.queries), db.queries)
	}
}

func TestResolveBootstrapCredentialOwnerFailsClosedWhenNoCredentialRow(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{}}},
	}
	store := NewIdentitySubjectStore(db)

	_, _, err := store.ResolveBootstrapCredentialOwner(context.Background(), "tenant_local", "workspace_local")
	if !errors.Is(err, ErrBootstrapCredentialNotFound) {
		t.Fatalf("err = %v, want ErrBootstrapCredentialNotFound", err)
	}
}

func TestResolveBootstrapCredentialOwnerRequiresTenantAndWorkspace(t *testing.T) {
	t.Parallel()

	store := NewIdentitySubjectStore(&fakeExecQueryer{})

	if _, _, err := store.ResolveBootstrapCredentialOwner(context.Background(), "", "workspace_local"); err == nil {
		t.Fatal("expected error for empty tenant_id")
	}
	if _, _, err := store.ResolveBootstrapCredentialOwner(context.Background(), "tenant_local", ""); err == nil {
		t.Fatal("expected error for empty workspace_id")
	}
}
