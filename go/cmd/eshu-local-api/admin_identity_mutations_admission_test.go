// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// fakeAdminIdentityAdmission is a no-store admission port for the wiring
// test: all five mutations succeed without touching Postgres.
type fakeAdminIdentityAdmission struct{}

func (f *fakeAdminIdentityAdmission) RevokeAdminInvitation(
	_ context.Context,
	_ query.AdminInvitationRevokeRequest,
) (query.AdminInvitationRevokeResult, error) {
	return query.AdminInvitationRevokeResult{}, nil
}

func (f *fakeAdminIdentityAdmission) GrantAdminRoleAssignment(
	_ context.Context,
	_ query.AdminRoleAssignmentGrantRequest,
) (query.AdminRoleAssignmentMutationResult, error) {
	return query.AdminRoleAssignmentMutationResult{}, nil
}

func (f *fakeAdminIdentityAdmission) RevokeAdminRoleAssignment(
	_ context.Context,
	_ query.AdminRoleAssignmentRevokeRequest,
) (query.AdminRoleAssignmentMutationResult, error) {
	return query.AdminRoleAssignmentMutationResult{}, nil
}

func (f *fakeAdminIdentityAdmission) CreateAdminIdPGroupMapping(
	_ context.Context,
	_ query.AdminIdPGroupMappingCreateRequest,
) (query.AdminIdPGroupMappingCreateResult, error) {
	return query.AdminIdPGroupMappingCreateResult{}, nil
}

func (f *fakeAdminIdentityAdmission) DeleteAdminIdPGroupMapping(
	_ context.Context,
	_ query.AdminIdPGroupMappingDeleteRequest,
) (query.AdminIdPGroupMappingDeleteResult, error) {
	return query.AdminIdPGroupMappingDeleteResult{}, nil
}

// TestNewAdminIdentityMutationHandlerTakesAdmissionContract proves the
// #8872 seam: the admin identity mutation handler is built with the
// versioned admission contract, not a *sql.DB writer handle. The handler
// must carry the exact injected port (identity, not merely non-nil) so
// eshu-api can later bind a core-issued admission endpoint.
func TestNewAdminIdentityMutationHandlerTakesAdmissionContract(t *testing.T) {
	t.Parallel()

	store := &fakeAdminIdentityAdmission{}
	handler := newAdminIdentityMutationHandler(store, nil)
	if handler == nil {
		t.Fatal("newAdminIdentityMutationHandler() = nil, want a handler bound to the admission contract")
	}
	if handler.Store != query.AdminIdentityAdmission(store) {
		t.Fatalf("handler.Store = %T(%v), want the exact injected admission contract %T(%v)", handler.Store, handler.Store, store, store)
	}
}

// TestNewAdminIdentityMutationHandlerUnwrapsTypedNil proves the fail-closed
// convention: a typed-nil adapter becomes an untyped nil Store, so the
// routes answer 503 instead of serving through a nil backing store.
func TestNewAdminIdentityMutationHandlerUnwrapsTypedNil(t *testing.T) {
	t.Parallel()

	var typedNil *postgresAdminIdentityMutationAdapter
	handler := newAdminIdentityMutationHandler(typedNil, nil)
	if handler == nil {
		t.Fatal("newAdminIdentityMutationHandler() = nil, want a handler with a nil store")
	}
	if handler.Store != nil {
		t.Fatalf("handler.Store = %T(%v), want nil after unwrapping the typed-nil adapter", handler.Store, handler.Store)
	}
}
