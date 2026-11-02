// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
)

// loopbackOnly refuses a DSN that can reach any non-loopback host: pgx dials
// the fallback hosts in order, so every one of them must be loopback.
func loopbackOnly(dsn string) error {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse DSN: %w", err)
	}
	hosts := []string{cfg.Host}
	for _, fb := range cfg.Fallbacks {
		hosts = append(hosts, fb.Host)
	}
	for _, host := range hosts {
		if !isLoopback(host) {
			return fmt.Errorf("host %q is not loopback", host)
		}
	}
	return nil
}

// coreDatabases are the databases a disposable in-tree server must never
// hold: the core database and the satellite's.
var coreDatabases = []string{"eshu", "workflow_claims"}

// inTreeServerSafe refuses an in-tree server that holds the core or the
// satellite database, or that is the satellite DSN's own server (same
// system_identifier, or the same host and port when the satellite role
// cannot read pg_control_system). The in-tree leg creates and drops whole
// databases, so it may only run on a dedicated throwaway server.
func inTreeServerSafe(ctx context.Context, admin *sql.DB, adminDSN, satelliteDSN string) error {
	var held []string
	rows, err := admin.QueryContext(ctx, "SELECT datname FROM pg_database WHERE datname = ANY($1) ORDER BY 1", coreDatabases)
	if err != nil {
		return fmt.Errorf("list databases: %w", err)
	}
	if held, err = scanStrings(rows); err != nil {
		return fmt.Errorf("list databases: %w", err)
	}
	if len(held) > 0 {
		return fmt.Errorf("in-tree server holds %v; use a dedicated throwaway server", held)
	}
	if satelliteDSN == "" {
		return nil
	}
	sat, err := sql.Open("pgx", satelliteDSN)
	if err != nil {
		return fmt.Errorf("open satellite DSN: %w", err)
	}
	defer func() { _ = sat.Close() }()
	mine, errMine := systemIdentifier(ctx, admin)
	theirs, errTheirs := systemIdentifier(ctx, sat)
	if errMine == nil && errTheirs == nil {
		if mine == theirs {
			return fmt.Errorf("in-tree server is the satellite's server (system_identifier %s)", mine)
		}
		return nil
	}
	mineEnd, err1 := dsnEndpoint(adminDSN)
	theirEnd, err2 := dsnEndpoint(satelliteDSN)
	if err := errors.Join(err1, err2); err != nil {
		return errors.Join(fmt.Errorf("compare servers: %w", err), errMine, errTheirs)
	}
	if mineEnd == theirEnd {
		return fmt.Errorf("in-tree server and satellite DSN share the endpoint %s", mineEnd)
	}
	return nil
}

func systemIdentifier(ctx context.Context, db *sql.DB) (string, error) {
	var id string
	if err := db.QueryRowContext(ctx, "SELECT system_identifier::text FROM pg_control_system()").Scan(&id); err != nil {
		return "", fmt.Errorf("read system_identifier: %w", err)
	}
	return id, nil
}

// dsnEndpoint is a DSN's first host and port.
func dsnEndpoint(dsn string) (string, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return "", fmt.Errorf("parse DSN: %w", err)
	}
	return net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))), nil
}

// requireInTreeServerSafe fails the in-tree entry test before any database
// is created on an unsafe server.
func requireInTreeServerSafe(t *testing.T, admin string) {
	t.Helper()
	db, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatalf("open in-tree admin DSN: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := inTreeServerSafe(context.Background(), db, admin, os.Getenv(envSatelliteDSN)); err != nil {
		t.Fatalf("%s refused: %v", envInTreeDSN, err)
	}
}
