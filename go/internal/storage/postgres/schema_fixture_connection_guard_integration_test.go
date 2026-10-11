// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

var errUnsafeFixtureConnection = errors.New("refuse unowned PostgreSQL fixture connection")

const rollbackTestDatabase = "eshu_7033_rollback"

var (
	migration169FixtureURI = regexp.MustCompile(`^postgres://eshu169fixture:([0-9a-f]{32})@127\.0\.0\.1:([1-9][0-9]{0,4})/eshu169fixture\?sslmode=disable$`)
	rollbackFixtureURI     = regexp.MustCompile(`^postgres://postgres@127\.0\.0\.1:([1-9][0-9]{0,4})/eshu_7033_rollback\?sslmode=disable$`)
)

type ownedFixtureKind uint8

const (
	ownedFixtureMigration169 ownedFixtureKind = iota + 1
	ownedFixtureRollback
)

type fixtureSession struct {
	searchPath      string
	applicationName string
}

type ownedFixtureEndpoint struct {
	port     uint16
	database string
	user     string
	password string
}

// openOwnedFixtureDB validates the exact launcher target before dialing and
// validates every physical PostgreSQL connection before it enters sql.DB.
func openOwnedFixtureDB(
	ctx context.Context, dsn string, expectedID func() string, kind ownedFixtureKind, session fixtureSession,
) (*sql.DB, error) {
	endpoint, err := parseOwnedFixtureEndpoint(dsn, kind)
	if err != nil {
		return nil, err
	}
	if _, err := canonicalSystemIdentifier(expectedID()); err != nil {
		return nil, err
	}
	if err := rejectInheritedPGEnvironment(os.Environ()); err != nil {
		return nil, err
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot parse canonical URI", errUnsafeFixtureConnection)
	}
	if config.Host != "127.0.0.1" || config.Port != endpoint.port ||
		config.Database != endpoint.database || config.User != endpoint.user ||
		config.Password != endpoint.password || config.TLSConfig != nil ||
		len(config.Fallbacks) != 0 || len(config.RuntimeParams) != 0 {
		return nil, fmt.Errorf("%w: parsed connection target differs from canonical URI", errUnsafeFixtureConnection)
	}
	config.ConnectTimeout = 3 * time.Second
	config.ValidateConnect = func(connectCtx context.Context, connection *pgconn.PgConn) error {
		return validateOwnedFixtureConnection(connectCtx, connection, expectedID, endpoint)
	}
	if session.searchPath != "" || session.applicationName != "" {
		config.AfterConnect = func(connectCtx context.Context, connection *pgconn.PgConn) error {
			if session.searchPath != "" {
				if err := setOwnedFixtureSessionValue(connectCtx, connection, "search_path", session.searchPath); err != nil {
					return err
				}
			}
			if session.applicationName != "" {
				if err := setOwnedFixtureSessionValue(connectCtx, connection, "application_name", session.applicationName); err != nil {
					return err
				}
			}
			return nil
		}
	}
	database := stdlib.OpenDB(*config)
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("open owned PostgreSQL fixture: %w", err)
	}
	return database, nil
}

func parseOwnedFixtureEndpoint(dsn string, kind ownedFixtureKind) (ownedFixtureEndpoint, error) {
	var endpoint ownedFixtureEndpoint
	var match []string
	var portText string
	switch kind {
	case ownedFixtureMigration169:
		match = migration169FixtureURI.FindStringSubmatch(dsn)
		if len(match) != 3 {
			return endpoint, fmt.Errorf("%w: noncanonical migration 169 URI", errUnsafeFixtureConnection)
		}
		endpoint.database, endpoint.user, endpoint.password = "eshu169fixture", "eshu169fixture", match[1]
		portText = match[2]
	case ownedFixtureRollback:
		match = rollbackFixtureURI.FindStringSubmatch(dsn)
		if len(match) != 2 {
			return endpoint, fmt.Errorf("%w: noncanonical rollback URI", errUnsafeFixtureConnection)
		}
		endpoint.database, endpoint.user = rollbackTestDatabase, "postgres"
		portText = match[1]
	default:
		return endpoint, fmt.Errorf("%w: unknown fixture kind", errUnsafeFixtureConnection)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return endpoint, fmt.Errorf("%w: invalid explicit loopback port", errUnsafeFixtureConnection)
	}
	endpoint.port = uint16(port)
	return endpoint, nil
}

func canonicalSystemIdentifier(raw string) (string, error) {
	identifier, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || identifier == 0 || strconv.FormatUint(identifier, 10) != raw {
		return "", fmt.Errorf("%w: missing or noncanonical expected system identifier", errUnsafeFixtureConnection)
	}
	return raw, nil
}

func rejectInheritedPGEnvironment(environment []string) error {
	var names []string
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "PG") {
			names = append(names, name)
		}
	}
	if len(names) > 0 {
		sort.Strings(names)
		return fmt.Errorf("%w: inherited PostgreSQL environment keys %s", errUnsafeFixtureConnection, strings.Join(names, ","))
	}
	return nil
}

func validateOwnedFixtureConnection(
	ctx context.Context, connection *pgconn.PgConn, expectedID func() string, endpoint ownedFixtureEndpoint,
) error {
	wantID, err := canonicalSystemIdentifier(expectedID())
	if err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result := connection.ExecParams(probeCtx, `SELECT system_identifier::text, current_database(), current_user,
current_setting('server_version_num')::int FROM pg_control_system()`, nil, nil, nil, nil).Read()
	if result.Err != nil {
		return fmt.Errorf("%w: fixture identity query failed: %v", errUnsafeFixtureConnection, result.Err)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 4 {
		return fmt.Errorf("%w: unexpected fixture identity row shape", errUnsafeFixtureConnection)
	}
	row := result.Rows[0]
	version, err := strconv.Atoi(string(row[3]))
	if err != nil || string(row[0]) != wantID || string(row[1]) != endpoint.database ||
		string(row[2]) != endpoint.user || version/10000 != 18 {
		return fmt.Errorf("%w: system identifier, database, role, or major version mismatch", errUnsafeFixtureConnection)
	}
	return nil
}

func setOwnedFixtureSessionValue(ctx context.Context, connection *pgconn.PgConn, name, value string) error {
	setupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result := connection.ExecParams(setupCtx,
		"SELECT set_config($1, $2, false)", [][]byte{[]byte(name), []byte(value)}, nil, nil, nil).Read()
	if result.Err != nil {
		return fmt.Errorf("configure owned PostgreSQL fixture session %s: %w", name, result.Err)
	}
	return nil
}
