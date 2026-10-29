// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
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

func TestWriterRoleAttestorPreservesDSNTargetSessionAttrs(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the physical connection proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	primary, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	var inRecovery bool
	if err := primary.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil {
		t.Fatal(err)
	}
	_ = primary.Close(ctx)
	if inRecovery {
		t.Skip("the proof requires a primary")
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("target_session_attrs", "standby")
		parsed.RawQuery = query.Encode()
		dsn = parsed.String()
	} else {
		dsn += " target_session_attrs=standby"
	}
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil || parsed.ValidateConnect == nil {
		t.Fatalf("standby DSN did not install a validator: %v", err)
	}
	db, err := OpenWriterDBWithRoleAttestorHooks(dsn, func(context.Context, *pgconn.PgConn) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err == nil {
		t.Fatal("role attestor bypassed DSN target_session_attrs=standby on a primary")
	}
}
