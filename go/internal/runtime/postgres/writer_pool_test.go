// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestWriterPoolErrorsCarryNoConnectionTarget pins the #7253 contract for the
// writer pool. Handlers write err.Error() into 5xx bodies, and the writer pool
// serves authorization, audit, mutation, and sign-in reads. pgx formats a
// connection failure with the database user, the database, and the dialed
// address, so the pool must return a fixed text, keep the driver error behind
// Unwrap, and log the detail on the logger it was given.
func TestWriterPoolErrorsCarryNoConnectionTarget(t *testing.T) {
	t.Parallel()

	cfg, err := pgx.ParseConfig("postgres://alice:s3cret@127.0.0.1:1/appdb?sslmode=disable&connect_timeout=2")
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	var logs bytes.Buffer
	pool := openWriterPool(cfg, slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { _ = pool.Close() })

	err = pool.PingContext(context.Background())
	if err == nil {
		t.Fatal("PingContext() error = nil, want a dial failure")
	}
	for _, leaked := range []string{"alice", "appdb", "127.0.0.1", "user=", "database=", "s3cret"} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("writer pool error carries %q: %v", leaked, err)
		}
	}
	var connectErr *pgconn.ConnectError
	if !errors.As(err, &connectErr) {
		t.Error("errors.As(ConnectError) = false, want the driver error reachable for classification")
	}
	for _, want := range []string{`"event_name":"postgres.store.error"`, `"failure_class":"unavailable"`, "user=alice"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("writer pool logger lacks %q:\n%s", want, logs.String())
		}
	}
}
