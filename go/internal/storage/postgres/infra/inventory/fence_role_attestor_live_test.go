// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 physical-connection proof that writer role attestation runs before session hooks on every new connection
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestWriterRoleAttestorRunsBeforeSessionHooksOnEveryPhysicalConnection(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the physical connection proof")
	}
	var attested atomic.Int32
	var hookCalls atomic.Int32
	db, err := OpenWriterDBWithRoleAttestorHooks(dsn, func(ctx context.Context, conn *pgconn.PgConn) error {
		attested.Add(1)
		return nil
	}, []ConnectHook{func(ctx context.Context, conn *pgx.Conn) error {
		hookCalls.Add(1)
		if attested.Load() != hookCalls.Load() {
			return errors.New("session hook ran before role attestation")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxIdleConns(0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for range 2 {
		if err := db.PingContext(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := attested.Load(); got != 2 {
		t.Fatalf("attested physical connections = %d, want 2", got)
	}
}
