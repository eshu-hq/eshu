// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// beginnerDB is an ExecQueryer that also begins transactions, like the reducer's
// instrumented database.
type beginnerDB struct{ db.ExecQueryer }

func (beginnerDB) Begin(context.Context) (db.Transaction, error) { return nil, sql.ErrConnDone }

// TestSupplyChainImpactFanOutIsWiredBesideTheWriterWithTheQueueLeaseOwner guards
// the #9695 wire in the reducer entrypoint: the fan-out exists exactly when the
// writer does, it is the Postgres store, and it fences its own-item update on the
// lease owner the work queue acknowledges with.
func TestSupplyChainImpactFanOutIsWiredBesideTheWriterWithTheQueueLeaseOwner(t *testing.T) {
	t.Parallel()

	database := beginnerDB{}
	handlers := buildReducerSupplyChainSecurityHandlers(database, postgres.FactStore{}, nil, endpointPresenceWirings{})
	if handlers.SupplyChainImpactWriter == nil {
		t.Fatal("SupplyChainImpactWriter is nil with a beginner database")
	}
	store, ok := handlers.SupplyChainImpactFanOut.(postgres.SupplyChainImpactFanOutStore)
	if !ok {
		t.Fatalf("SupplyChainImpactFanOut = %T, want postgres.SupplyChainImpactFanOutStore", handlers.SupplyChainImpactFanOut)
	}
	if store.LeaseOwner != reducerWorkQueueLeaseOwner {
		t.Fatalf("fan-out lease owner = %q, want the work queue's %q", store.LeaseOwner, reducerWorkQueueLeaseOwner)
	}
	if store.Beginner == nil || store.DB == nil {
		t.Fatal("fan-out store is missing its beginner or database")
	}
	// Without a beginner the writer is nil and so is the fan-out: the domain is
	// not registered at all.
	bare := buildReducerSupplyChainSecurityHandlers(nil, postgres.FactStore{}, nil, endpointPresenceWirings{})
	if bare.SupplyChainImpactWriter != nil || bare.SupplyChainImpactFanOut != nil {
		t.Fatal("a database without a beginner must leave both the writer and the fan-out nil")
	}
}
