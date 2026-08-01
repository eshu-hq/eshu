// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestContainerImageIdentitySupportPaginationIsCompleteUnderICUCollationLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_ICU_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_ICU_TEST_DSN to run the locale-aware support pagination proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open ICU postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var collation, locale string
	var provider string
	if err := db.QueryRowContext(ctx, `
SELECT datcollate, datlocprovider::text, COALESCE(datlocale, '')
FROM pg_database
WHERE datname = current_database()`).Scan(&collation, &provider, &locale); err != nil {
		t.Fatalf("read database collation: %v", err)
	}
	if provider != "i" || locale == "" {
		t.Fatalf("locale-aware proof requires ICU database collation, got collate=%q provider=%q locale=%q", collation, provider, locale)
	}
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	const (
		shortScope = "repository:icu-cursor-prefix-a"
		longScope  = "repository:icu-cursor-prefix-aa"
		wantRows   = listFactsByKindPageSize + 2
	)
	digest := "sha256:" + strings.Repeat("77", 32)
	cleanupContainerImageIdentityPrefixPaginationLive(t, ctx, db, shortScope, longScope)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentityPrefixPaginationLive(t, cleanupCtx, db, shortScope, longScope)
	})
	seedContainerImageIdentityPrefixPaginationLive(t, ctx, db, digest, shortScope, longScope)

	rows, err := NewFactStore(SQLDB{DB: db}).ListActiveCICDRunCorrelationFacts(ctx, []string{digest}, nil)
	if err != nil {
		t.Fatalf("list support facts under %s/%s collation: %v", locale, collation, err)
	}
	assertContainerImageIdentityPrefixPaginationRows(t, rows, longScope, wantRows)
}
