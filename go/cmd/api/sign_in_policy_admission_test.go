// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestWireSignInPolicyHandlersNilDatabaseYieldsNilStore proves the
// production wiring path: a nil database yields a nil admission port, so
// the mutation handler carries a true nil store and answers 503, never a
// typed-nil interface that would hide from the nil-store check.
func TestWireSignInPolicyHandlersNilDatabaseYieldsNilStore(t *testing.T) {
	t.Parallel()

	router := &query.APIRouter{LocalIdentity: &query.LocalIdentityHandler{}}
	wireSignInPolicyHandlers(nil, nil, nil, router)
	if router.SignInPolicyMutations == nil {
		t.Fatal("wireSignInPolicyHandlers() left SignInPolicyMutations nil, want a handler with a nil store")
	}
	if router.SignInPolicyMutations.Store != nil {
		t.Fatalf("wired handler Store = %T(%v), want nil for the 503 path", router.SignInPolicyMutations.Store, router.SignInPolicyMutations.Store)
	}
}

// fakeSignInPolicyAdmission is a no-store admission port: the handler test
// below proves the handler is built with the injected versioned admission
// contract without opening a direct table handle.
type fakeSignInPolicyAdmission struct{}

func (f *fakeSignInPolicyAdmission) UpsertSignInPolicy(
	_ context.Context,
	_ string,
	_ query.SignInPolicyUpdateRequest,
	_ string,
	_ time.Time,
) (query.SignInPolicy, error) {
	return query.SignInPolicy{}, nil
}

// TestNewSignInPolicyMutationHandlerTakesAdmissionContract proves the #8872
// seam: the sign-in policy mutation handler is built with the versioned
// admission contract, not a *sql.DB writer handle. The handler must carry
// the exact injected port (identity, not merely non-nil) so eshu-api can
// later bind a core-issued admission endpoint.
func TestNewSignInPolicyMutationHandlerTakesAdmissionContract(t *testing.T) {
	t.Parallel()

	admission := &fakeSignInPolicyAdmission{}
	handler := newSignInPolicyMutationHandler(admission, nil, nil)
	if handler == nil {
		t.Fatal("newSignInPolicyMutationHandler() = nil, want a handler bound to the admission contract")
	}
	if handler.Store != query.SignInPolicyAdmission(admission) {
		t.Fatalf("handler.Store = %T(%v), want the exact injected admission contract %T(%v)", handler.Store, handler.Store, admission, admission)
	}
}

// TestNewSignInPolicyMutationHandlerNilAdmissionStaysNil proves the
// nil-database convention: a nil admission port must stay a nil store,
// never a typed-nil interface that would hide from the nil-store check.
// The 503 response itself is proven end to end by
// TestAdminSignInPolicyUpdateNilStoreReturns503 in package query.
func TestNewSignInPolicyMutationHandlerNilAdmissionStaysNil(t *testing.T) {
	t.Parallel()

	handler := newSignInPolicyMutationHandler(nil, nil, nil)
	if handler == nil {
		t.Fatal("newSignInPolicyMutationHandler() = nil, want a handler with a nil store")
	}
	if handler.Store != nil {
		t.Fatalf("handler.Store = %T(%v), want nil for the 503 path", handler.Store, handler.Store)
	}
}
