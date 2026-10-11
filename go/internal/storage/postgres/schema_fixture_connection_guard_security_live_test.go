// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOwnedFixtureConnectorRejectsUnsafeInputBeforeDial(t *testing.T) {
	const validID = "123456789"
	const valid169 = "postgres://eshu169fixture:0123456789abcdef0123456789abcdef@127.0.0.1:15432/eshu169fixture?sslmode=disable"
	const validRollback = "postgres://postgres@127.0.0.1:15432/eshu_7033_rollback?sslmode=disable"
	for _, tc := range []struct {
		name, dsn string
		kind      ownedFixtureKind
	}{
		{"remote_host", strings.Replace(valid169, "127.0.0.1", "10.0.0.1", 1), ownedFixtureMigration169},
		{"query_host_override", valid169 + "&host=10.0.0.1", ownedFixtureMigration169},
		{"encoded_host_override", valid169 + "&%68ost=10.0.0.1", ownedFixtureMigration169},
		{"duplicate_sslmode", valid169 + "&sslmode=require", ownedFixtureMigration169},
		{"encoded_sslmode", strings.Replace(valid169, "sslmode", "%73slmode", 1), ownedFixtureMigration169},
		{"service", valid169 + "&service=production", ownedFixtureMigration169},
		{"options", valid169 + "&options=-csearch_path%3Dpublic", ownedFixtureMigration169},
		{"wrong_role", strings.Replace(valid169, "eshu169fixture:", "postgres:", 1), ownedFixtureMigration169},
		{"wrong_database", strings.Replace(valid169, "/eshu169fixture?", "/postgres?", 1), ownedFixtureMigration169},
		{"password_escape", strings.Replace(valid169, "0123456789abcdef", "%30"+"123456789abcdef", 1), ownedFixtureMigration169},
		{"port_zero", strings.Replace(valid169, ":15432/", ":0/", 1), ownedFixtureMigration169},
		{"port_leading_zero", strings.Replace(valid169, ":15432/", ":015432/", 1), ownedFixtureMigration169},
		{"rollback_password", strings.Replace(validRollback, "postgres@", "postgres:secret@", 1), ownedFixtureRollback},
		{"rollback_query_host", validRollback + "&host=10.0.0.1", ownedFixtureRollback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			database, err := openOwnedFixtureDB(ctx, tc.dsn, func() string { return validID }, tc.kind, fixtureSession{})
			if database != nil {
				_ = database.Close()
				t.Fatal("unsafe fixture DSN returned a database handle")
			}
			if !errors.Is(err, errUnsafeFixtureConnection) {
				t.Fatalf("unsafe fixture DSN error = %v, want guard rejection", err)
			}
		})
	}
	for _, id := range []string{"", "0", "000123", "-1", "not-an-id"} {
		t.Run("expected_id_"+id, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			database, err := openOwnedFixtureDB(ctx, valid169, func() string { return id }, ownedFixtureMigration169, fixtureSession{})
			if database != nil {
				_ = database.Close()
				t.Fatal("invalid expected system identifier returned a database handle")
			}
			if !errors.Is(err, errUnsafeFixtureConnection) {
				t.Fatalf("invalid expected system identifier error = %v, want guard rejection", err)
			}
		})
	}
}

func TestOwnedFixtureConnectorRejectsInheritedPGEnvironment(t *testing.T) {
	const dsn = "postgres://postgres@127.0.0.1:15432/eshu_7033_rollback?sslmode=disable"
	for _, name := range []string{
		"PGHOST", "PGHOSTADDR", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD",
		"PGSERVICE", "PGSERVICEFILE", "PGOPTIONS",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "untrusted")
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			database, err := openOwnedFixtureDB(ctx, dsn, func() string { return "123456789" }, ownedFixtureRollback, fixtureSession{})
			if database != nil {
				_ = database.Close()
				t.Fatalf("inherited %s returned a database handle", name)
			}
			if !errors.Is(err, errUnsafeFixtureConnection) || !strings.Contains(err.Error(), name) {
				t.Fatalf("inherited %s error = %v, want explicit rejection", name, err)
			}
		})
	}
}

func TestOwnedFixtureConnectorRejectsWrongServerAndReconnect(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_MIGRATION169_TEST_DSN")
	expected := os.Getenv("ESHU_TEST_POSTGRES_SYSTEM_IDENTIFIER")
	if dsn == "" || expected == "" {
		t.Skip("requires the disposable migration 169 fixture and launcher-supplied system identifier")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wrong := fmt.Sprintf("%d", 1)
	if expected == wrong {
		wrong = "2"
	}
	rejected, err := openOwnedFixtureDB(ctx, dsn, func() string { return wrong }, ownedFixtureMigration169, fixtureSession{})
	if rejected != nil {
		_ = rejected.Close()
		t.Fatal("mismatched server identity returned a database handle")
	}
	if !errors.Is(err, errUnsafeFixtureConnection) {
		t.Fatalf("mismatched server identity error = %v, want guard rejection", err)
	}

	current := expected
	database, err := openOwnedFixtureDB(ctx, dsn, func() string { return current }, ownedFixtureMigration169, fixtureSession{})
	if err != nil {
		t.Fatalf("owned fixture connection rejected: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxIdleConns(0)
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("owned fixture first physical connection: %v", err)
	}
	current = wrong
	if err := database.PingContext(ctx); !errors.Is(err, errUnsafeFixtureConnection) {
		t.Fatalf("reconnected wrong system identifier error = %v, want guard rejection", err)
	}
}
