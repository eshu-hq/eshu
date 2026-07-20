// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// BenchmarkGovernanceAuditStoreAppendSingleEvent is the F-9 (#5170)
// prove-theory-first Bench B: it measures the real synchronous
// GovernanceAuditStore.Append cost for ONE allowed read_authorization event
// against a local Postgres. This is the cost a naive synchronous emission
// would add to every successful MCP read (see the addendum's §1/§4).
//
// It is skipped unless a live Postgres DSN is provided through
// ESHU_GOVERNANCE_AUDIT_BENCH_DSN or ESHU_POSTGRES_DSN, matching the pattern
// used by BenchmarkReducerQueueClaimDeepQueue.
func BenchmarkGovernanceAuditStoreAppendSingleEvent(b *testing.B) {
	dsn := governanceAuditBenchmarkDSN()
	if dsn == "" {
		b.Skip("set ESHU_GOVERNANCE_AUDIT_BENCH_DSN or ESHU_POSTGRES_DSN to run the governance audit append benchmark")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		b.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	sqlConn, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		b.Fatalf("open dedicated postgres connection: %v", err)
	}
	conn := governanceAuditBenchmarkConn{conn: sqlConn}

	schemaName := fmt.Sprintf("governance_audit_bench_%d", time.Now().UnixNano())
	cleanup := func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		_ = sqlConn.Close()
		_ = db.Close()
	}
	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		cleanup()
		b.Fatalf("create benchmark schema: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		cleanup()
		b.Fatalf("set search_path: %v", err)
	}
	if _, err := conn.ExecContext(ctx, GovernanceAuditEventsSchemaSQL()); err != nil {
		cleanup()
		b.Fatalf("apply governance audit schema: %v", err)
	}
	defer cleanup()

	store := NewGovernanceAuditStore(conn)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := governanceReadAllowedBenchmarkEvent(i)
		if err := store.Append(ctx, []governanceaudit.Event{event}); err != nil {
			b.Fatalf("Append() single allowed event: %v", err)
		}
	}
}

// governanceReadAllowedBenchmarkEvent builds the exact allowed
// read_authorization event shape the F-9 addendum specifies for the
// authMiddleware resolver-success emission point (§2/§5): scoped_token actor
// class, admin scope class, allowed decision, scoped_read_allowed reason. The
// loop index is folded into the correlation ID so every call produces a
// distinct event_id (the store's content-hash primary key) and the benchmark
// measures real INSERTs rather than ON CONFLICT DO NOTHING no-ops.
func governanceReadAllowedBenchmarkEvent(i int) governanceaudit.Event {
	return governanceaudit.Event{
		Type:               governanceaudit.EventTypeReadAuthorization,
		ActorClass:         governanceaudit.ActorClassScopedToken,
		ActorIDHash:        "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd",
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           governanceaudit.DecisionAllowed,
		ReasonCode:         "scoped_read_allowed",
		CorrelationID:      fmt.Sprintf("bench-corr-%d", i),
		PolicyRevisionHash: "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba98765432",
		OccurredAt:         time.Now().UTC(),
	}
}

func governanceAuditBenchmarkDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_GOVERNANCE_AUDIT_BENCH_DSN")); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

type governanceAuditBenchmarkConn struct {
	conn *sql.Conn
}

func (c governanceAuditBenchmarkConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.conn.ExecContext(ctx, query, args...)
}

func (c governanceAuditBenchmarkConn) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}
