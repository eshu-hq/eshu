// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestAccessForeignWriterFirstFallsBackToFrozenPrimary(t *testing.T) {
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	foreignFirst := os.Getenv("ESHU_READER_TEST_FOREIGN_WRITER_FIRST_DSN")
	expectedID := os.Getenv("ESHU_READER_TEST_EXPECTED_SYSTEM_ID")
	if writer == "" || reader == "" || foreignFirst == "" || expectedID == "" {
		t.Skip("owned writer, reader, foreign-first candidates, and independent system ID required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Prove the first candidate is an available, writable, foreign primary.
	first, err := pgx.ParseConfig(foreignFirst)
	if err != nil || len(first.Fallbacks) == 0 {
		t.Fatalf("foreign-first candidate list required: %v", err)
	}
	first.Fallbacks = nil
	first.ValidateConnect = nil
	foreign, err := pgx.ConnectConfig(ctx, first)
	if err != nil {
		t.Fatalf("foreign first candidate unavailable: %v", err)
	}
	var foreignID string
	var recovery bool
	var readOnly string
	err = foreign.QueryRow(ctx, "SELECT system_identifier::text,pg_is_in_recovery(),current_setting('transaction_read_only') FROM pg_control_system()").Scan(&foreignID, &recovery, &readOnly)
	closeErr := foreign.Close(ctx)
	if err != nil || closeErr != nil {
		t.Fatalf("foreign first candidate metadata=%v close=%v", err, closeErr)
	}
	if recovery || readOnly != "off" || foreignID == expectedID {
		t.Fatal("first candidate is not an independent writable primary")
	}
	// The trusted direct writer independently anchors the configured system ID.
	trustedCfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writer
		case "ESHU_POSTGRES_READ_DSN":
			return reader
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	trustedCfg.ExpectedSystemID = expectedID
	trusted, err := Open(ctx, trustedCfg, nil)
	if err != nil {
		t.Fatalf("independent system ID does not match trusted writer: %v", err)
	}
	_ = trusted.Close()
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return foreignFirst
		case "ESHU_POSTGRES_READ_DSN":
			return reader
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.ExpectedSystemID = expectedID
	access, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("available foreign first candidate blocked correct fallback: %v", err)
	}
	defer access.Close()
	var selectedID string
	if err := access.Writer().QueryRowContext(ctx, "SELECT system_identifier::text FROM pg_control_system()").Scan(&selectedID); err != nil {
		t.Fatal(err)
	}
	if selectedID != expectedID {
		t.Fatal("business query reached foreign writer")
	}
	req, err := access.ContextWithCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var value int
	if err := access.Reader().QueryRowContext(req, "SELECT 17").Scan(&value); err != nil || value != 17 {
		t.Fatalf("guarded read=%d err=%v", value, err)
	}
	t.Log("reachable foreign first writer was rejected; business SQL ran on the expected primary")
}
