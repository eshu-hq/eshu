// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage_test

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	bootstrapOptIn = flag.String("containerimage-bootstrap", "", "set to 1 only for an empty disposable container-image proof database")
	bootstrapName  = flag.String("containerimage-database", "", "exact expected disposable proof database name")
)

func TestMain(m *testing.M) {
	flag.Parse()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	name, err := disposableBootstrapDatabase(*bootstrapOptIn, *bootstrapName, dsn)
	if err == nil && name != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err = bootstrapContainerImageFixture(ctx, dsn, name)
		cancel()
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "container-image disposable bootstrap:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func disposableBootstrapDatabase(optIn, expected, dsn string) (string, error) {
	if optIn == "" && expected == "" {
		return "", nil
	}
	if optIn != "1" {
		return "", fmt.Errorf("explicit bootstrap opt-in must equal 1")
	}
	if !regexp.MustCompile(`^eshu_containerimage_[a-z0-9_]+$`).MatchString(expected) {
		return "", fmt.Errorf("expected an owned container-image proof database name")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return "", fmt.Errorf("an explicit PostgreSQL URI is required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || config.Database != expected {
		return "", fmt.Errorf("DSN database differs from the expected disposable identity")
	}
	return expected, nil
}

func bootstrapContainerImageFixture(ctx context.Context, dsn, expected string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open fixture connection: %w", err)
	}
	defer func() { _ = db.Close() }()
	var name, schema string
	if err := db.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&name, &schema); err != nil {
		return fmt.Errorf("read connected fixture identity: %w", err)
	}
	if name != expected || schema != "public" {
		return fmt.Errorf("connected fixture identity or schema differs from the requested empty database")
	}
	var tables int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
`).Scan(&tables); err != nil {
		return fmt.Errorf("inspect empty fixture: %w", err)
	}
	if tables != 0 {
		return fmt.Errorf("disposable fixture must be empty before bootstrap")
	}
	return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db})
}
