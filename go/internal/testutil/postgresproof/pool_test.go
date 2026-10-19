// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRaisePoolForConcurrentClaimersSetsRaisedLimits(t *testing.T) {
	database, err := sql.Open("pgx", "postgres://postgres:postgres@localhost:1/postgres?sslmode=disable")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	limit := RaisePoolForConcurrentClaimers(database, 8)
	if limit < 8 {
		t.Fatalf("RaisePoolForConcurrentClaimers(8) = %d, want at least 8", limit)
	}
	if got := database.Stats().MaxOpenConnections; got != limit {
		t.Fatalf("MaxOpenConnections = %d, want raised limit %d", got, limit)
	}
}

func TestRaisePoolForConcurrentClaimersFloorsAtOneClaimer(t *testing.T) {
	database, err := sql.Open("pgx", "postgres://postgres:postgres@localhost:1/postgres?sslmode=disable")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	limit := RaisePoolForConcurrentClaimers(database, 0)
	if limit < 1 {
		t.Fatalf("RaisePoolForConcurrentClaimers(0) = %d, want at least 1", limit)
	}
	if got := database.Stats().MaxOpenConnections; got != limit {
		t.Fatalf("MaxOpenConnections = %d, want raised limit %d", got, limit)
	}
}
