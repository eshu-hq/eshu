// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestSeedInitialAdminNegativeLeakage(t *testing.T) {
	banner := withBootstrapBannerCapture(t)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	dek := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	db := &fakeSeedDB{countRows: 0}
	audit := &fakeAuditAppender{}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
		"ESHU_AUTH_SECRET_ENC_KEY": dek,
	}), nil, logger, audit)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v", err)
	}

	bannerText := banner.String()
	passwordLine := extractBannerValue(t, bannerText, "password:")
	recoveryLine := extractBannerValue(t, bannerText, "recovery code:")

	logText := logBuf.String()
	if strings.Contains(logText, passwordLine) {
		t.Fatalf("structured log leaked the generated password: %q", logText)
	}
	if strings.Contains(logText, recoveryLine) {
		t.Fatalf("structured log leaked the generated recovery code: %q", logText)
	}
	if auditEventsLeak(audit.events, passwordLine, recoveryLine) {
		t.Fatalf("durable audit event leaked plaintext: %#v", audit.events)
	}
	if len(audit.events) == 0 {
		t.Fatal("no durable audit events were recorded for a generated-mode seed")
	}
	for _, exec := range db.execArgs {
		for _, arg := range exec {
			s, ok := arg.(string)
			if !ok {
				continue
			}
			if s == passwordLine || s == recoveryLine {
				t.Fatalf("a non-credential-table SQL exec carried the plaintext: %v", exec)
			}
		}
	}
}

func extractBannerValue(t *testing.T, banner, label string) string {
	t.Helper()
	idx := strings.Index(banner, label)
	if idx < 0 {
		t.Fatalf("banner missing label %q: %q", label, banner)
	}
	rest := banner[idx+len(label):]
	end := strings.IndexByte(rest, '\n')
	if end < 0 {
		end = len(rest)
	}
	return strings.TrimSpace(rest[:end])
}

func fakeSeedExecsContain(execs []string, substr string) bool {
	for _, e := range execs {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}

// TestSeedInitialAdminAuditEventsPersistToRealGovernanceAuditStore is a
// real-Postgres proof that seedInitialAdmin's durable audit events actually
// persist. Every other audit test in this file uses fakeAuditAppender,
// which only captures whatever Append receives directly — it never runs
// governanceaudit.NormalizeEvent, so it cannot catch an event the real
// pgstorage.GovernanceAuditStore would silently reject and drop (Append's
// error is always discarded via `_ = appender.Append(...)`, matching
// query.LocalIdentityHandler's established fire-and-forget convention).
// NormalizeEvent requires a non-zero OccurredAt; skipped unless a DSN is
// provided, matching this package's other real-Postgres proofs.
func TestSeedInitialAdminAuditEventsPersistToRealGovernanceAuditStore(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres governance-audit persistence proof")
	}
	banner := withBootstrapBannerCapture(t)

	ctx := context.Background()
	schemaName := fmt.Sprintf("seed_initial_admin_audit_%d", time.Now().UnixNano())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") })
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if err := pgstorage.ApplyBootstrap(ctx, pgstorage.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	dek := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // base64 of 32 raw bytes
	store := pgstorage.NewGovernanceAuditStore(pgstorage.SQLDB{DB: db})
	identityDB := pgstorage.SQLDB{DB: db}
	err = seedInitialAdmin(ctx, identityDB, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
		"ESHU_AUTH_SECRET_ENC_KEY": dek,
		"ESHU_ADMIN_USERNAME":      "owner",
	}), nil, nil, store)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v", err)
	}
	if banner.Len() == 0 {
		t.Fatal("banner not written")
	}

	modeEvents, err := store.List(ctx, pgstorage.GovernanceAuditQuery{
		OperatorAuthorized: true,
		ReasonCode:         bootstrapAuditReasonModeGenerated,
	})
	if err != nil {
		t.Fatalf("List() mode-choice events error = %v", err)
	}
	if len(modeEvents) != 1 {
		t.Fatalf("persisted mode-choice events = %d, want 1 (an event that fails governanceaudit.NormalizeEvent is silently dropped)", len(modeEvents))
	}
	if modeEvents[0].OccurredAt.IsZero() {
		t.Fatal("persisted mode-choice event OccurredAt is zero")
	}

	generatedEvents, err := store.List(ctx, pgstorage.GovernanceAuditQuery{
		OperatorAuthorized: true,
		ReasonCode:         bootstrapAuditReasonGenerated,
	})
	if err != nil {
		t.Fatalf("List() credential-generated events error = %v", err)
	}
	if len(generatedEvents) != 1 {
		t.Fatalf("persisted credential-generated events = %d, want 1", len(generatedEvents))
	}
	if generatedEvents[0].CorrelationID == "" {
		t.Fatal("persisted credential-generated event missing key_id CorrelationID")
	}
}
