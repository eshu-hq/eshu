// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

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
		if err := resolvesToLoopback(host); err != nil {
			return err
		}
	}
	return nil
}

// resolvesToLoopback accepts a loopback IP, or a host name whose every
// resolved address is loopback. A name that does not resolve is refused.
func resolvesToLoopback(host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() {
			return fmt.Errorf("host %q is not loopback", host)
		}
		return nil
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("host %q does not resolve: %w", host, err)
	}
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("host %q resolves to %s, which is not loopback", host, a)
		}
	}
	return nil
}

// dsnEndpoints lists every distinct host:port a DSN can dial: its first
// host and each pgx fallback (a TLS fallback to the same host collapses).
func dsnEndpoints(dsn string) ([]string, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	add := func(host string, port uint16) {
		ep := net.JoinHostPort(host, strconv.Itoa(int(port)))
		if !seen[ep] {
			seen[ep] = true
			out = append(out, ep)
		}
	}
	add(cfg.Host, cfg.Port)
	for _, fb := range cfg.Fallbacks {
		add(fb.Host, fb.Port)
	}
	return out, nil
}

// singleServer refuses an in-tree DSN that names more than one server: the
// disposable-database helper dials it again, and a second host could be a
// server the safety check never saw.
func singleServer(dsn string) error {
	eps, err := dsnEndpoints(dsn)
	if err != nil {
		return err
	}
	if len(eps) != 1 {
		return fmt.Errorf("the in-tree DSN names %d servers %v; name exactly one", len(eps), eps)
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
	mineEnd, err1 := dsnEndpoints(adminDSN)
	theirEnds, err2 := dsnEndpoints(satelliteDSN)
	if err := errors.Join(err1, err2); err != nil {
		return errors.Join(fmt.Errorf("compare servers: %w", err), errMine, errTheirs)
	}
	for _, mine := range mineEnd {
		for _, theirs := range theirEnds {
			if mine == theirs {
				return fmt.Errorf("in-tree server and satellite DSN share the endpoint %s", mine)
			}
		}
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

// requireInTreeServerSafe fails the in-tree entry test before any database
// is created on an unsafe server.
func requireInTreeServerSafe(t *testing.T, admin string) {
	t.Helper()
	if err := singleServer(admin); err != nil {
		t.Fatalf("%s refused: %v", envInTreeDSN, err)
	}
	db, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatalf("open in-tree admin DSN: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := inTreeServerSafe(context.Background(), db, admin, os.Getenv(envSatelliteDSN)); err != nil {
		t.Fatalf("%s refused: %v", envInTreeDSN, err)
	}
}

// claimPlaneHTTPClient is the satellite leg's client: a bounded timeout, and
// no redirects, so a redirect from the forwarded claim plane can never carry
// harness writes to another address.
func claimPlaneHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
