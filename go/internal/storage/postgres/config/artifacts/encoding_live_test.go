// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// TestCheckEncodingReadsTheDatabaseEncodingLive runs the start-up check on the
// bootstrapped UTF8 database and on a SQL_ASCII database created beside it on
// the same server. server_encoding is per database, so the check must refuse
// the second even though the server is the same.
func TestCheckEncodingReadsTheDatabaseEncodingLive(t *testing.T) {
	ctx, db := openDatabase(t)

	got := artifacts.CheckEncoding(ctx, postgres.SQLDB{DB: db}, nil)
	if !got.Supported || got.ServerEncoding != "UTF8" {
		t.Fatalf("CheckEncoding(bootstrapped database) = %+v, want UTF8 supported", got)
	}

	name := fmt.Sprintf("eshu_cfgart_ascii_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+name+
		" ENCODING 'SQL_ASCII' LC_COLLATE 'C' LC_CTYPE 'C' TEMPLATE template0"); err != nil {
		t.Fatalf("create SQL_ASCII database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	config, err := pgx.ParseConfig(os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	config.Database = name
	ascii := stdlib.OpenDB(*config)
	t.Cleanup(func() { _ = ascii.Close() })

	got = artifacts.CheckEncoding(ctx, postgres.SQLDB{DB: ascii}, nil)
	if got.Supported || got.ServerEncoding != "SQL_ASCII" || got.Reason != artifacts.ReasonEncodingUnsupported {
		t.Fatalf("CheckEncoding(SQL_ASCII database) = %+v, want unsupported SQL_ASCII", got)
	}
}
