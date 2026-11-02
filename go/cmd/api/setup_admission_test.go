// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestNewSetupHandlerTakesAdmissionContract proves the #8872 seam: the
// first-run setup wizard handler is built with the versioned admission
// contract, not a *sql.DB writer handle. The handler must carry the exact
// injected port (identity, not merely non-nil) so eshu-api can later bind
// a core-issued admission endpoint.
func TestNewSetupHandlerTakesAdmissionContract(t *testing.T) {
	t.Parallel()

	store := &postgresSetupAdapter{store: pgstorage.NewIdentitySubjectStore(&fakeSetupAdapterDB{})}
	handler := newSetupHandler(store, nil, nil, nil, "", "")
	if handler == nil {
		t.Fatal("newSetupHandler() = nil, want a handler bound to the admission contract")
	}
	if handler.Store != query.SetupAdmission(store) {
		t.Fatalf("handler.Store = %T(%v), want the exact injected admission contract %T(%v)", handler.Store, handler.Store, store, store)
	}
}

// TestNewSetupHandlerUnwrapsTypedNil proves the fail-closed convention: a
// typed-nil adapter becomes an untyped nil Store, so the wizard routes
// answer 503 instead of serving through a nil backing store.
func TestNewSetupHandlerUnwrapsTypedNil(t *testing.T) {
	t.Parallel()

	var typedNil *postgresSetupAdapter
	handler := newSetupHandler(typedNil, nil, nil, nil, "", "")
	if handler == nil {
		t.Fatal("newSetupHandler() = nil, want a handler with a nil store")
	}
	if handler.Store != nil {
		t.Fatalf("handler.Store = %T(%v), want nil after unwrapping the typed-nil adapter", handler.Store, handler.Store)
	}
}
