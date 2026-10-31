// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

// De-enrolled from the live-postgres-readiness plan under the drift-gate
// option A (PR #9544, steer 6530394665): the DSN session-attribute property
// stays proven by the enrolled TestDistinctReaderCredentialOnSamePhysicalPrimary
// on the reader path, and attestation ordering stays enrolled in
// fence_role_attestor_live_test.go. The code below stays to re-enroll in
// the post-refit follow-up.

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

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
