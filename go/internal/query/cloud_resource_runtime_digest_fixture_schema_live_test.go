// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const (
	// runtimeDigestFixtureSchemaPrefix opens the name of every schema this
	// helper creates. The sweep below refuses to drop anything that does not
	// start with it, so the prefix is the outer boundary on what the sweep can
	// touch. Nothing else in Eshu creates a schema with this prefix.
	runtimeDigestFixtureSchemaPrefix = "eshu_digest_fixture_"
	// runtimeDigestFixtureSchemaMaxAge is how old a leftover fixture schema must
	// be before the sweep drops it. Both guards run under a 60-second context,
	// so two hours cannot reach a schema a concurrent run is still using, even a
	// badly stalled one.
	runtimeDigestFixtureSchemaMaxAge = 2 * time.Hour
)

// runtimeDigestFixtureSchemaPattern matches exactly the names
// openRuntimeDigestFixtureDB generates: the prefix, a process id, an underscore,
// and a Unix nanosecond timestamp. It is built from the same prefix constant the
// names are built from, so the two cannot drift apart.
//
// The digit groups are bounded so a name cannot smuggle in something unparseable
// and reach the drop. 19 digits is the width of a Unix nanosecond timestamp
// through the year 2262 and the widest value that fits int64.
var runtimeDigestFixtureSchemaPattern = regexp.MustCompile(
	`^` + regexp.QuoteMeta(runtimeDigestFixtureSchemaPrefix) + `[0-9]{1,10}_([0-9]{1,19})$`,
)

// openRuntimeDigestFixtureDB returns a Postgres handle whose unqualified table
// names resolve inside a schema created for this test run alone, plus that
// schema's name. It registers the cleanup that drops the schema.
//
// The fixture tables are ordinary tables in that schema rather than TEMP
// tables. A TEMP table belongs to the session that created it, and
// database/sql owns session lifetime, not the test. SetMaxOpenConns(1) caps
// how many connections are open at once; it does not pin identity. Let the
// server close the connection, or an idle timeout or a network blip drop it,
// and database/sql opens a replacement whose pg_temp is empty.
//
// What happens next depends on the target database, and the worse case is the
// quiet one:
//
//   - On an empty database the replacement connection cannot resolve the
//     fixture names at all and the test dies with
//     `relation "ingestion_scopes" does not exist`. Confusing, but at least loud.
//   - On a migrated database — any real Eshu database, and the one CI runs
//     against — `ingestion_scopes`, `scope_generations`, `fact_records`, and
//     `graph_node_owner` all exist for real. The unqualified names resolve
//     against those real, empty tables, no error is raised, and the guard fails
//     as `21 of 21 digests got NO runtime evidence`: a message that reads
//     exactly like the starvation regression it exists to catch. Whoever picks
//     that up goes looking for a query bug that is not there.
//
// search_path is carried as a connection runtime parameter, so pgx sets it
// during startup on every connection this handle opens, replacements included.
// Setting it once with `SET search_path` would have the same session-scoped
// problem the TEMP tables had; TestRuntimeDigestFixtureSchemaSurvivesReconnectLive
// is what keeps a future edit from quietly regressing to that shape.
func openRuntimeDigestFixtureDB(t *testing.T, ctx context.Context, dsn string) (*sql.DB, string) {
	t.Helper()

	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse ESHU_POSTGRES_TEST_DSN: %v", err)
	}
	// Only the fixture schema, with no public fallback: an unqualified name
	// this test forgot to create must fail loudly rather than resolve against
	// whatever the target database already holds.
	schema := fmt.Sprintf("%s%d_%d", runtimeDigestFixtureSchemaPrefix, os.Getpid(), time.Now().UnixNano())
	config.RuntimeParams["search_path"] = schema

	db := sql.OpenDB(stdlib.GetConnector(*config))
	t.Cleanup(func() { _ = db.Close() })

	requireRuntimeDigestFixtureCreatePrivilege(t, ctx, db)
	sweepStaleRuntimeDigestFixtureSchemas(t, ctx, db)

	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+quoted); err != nil {
		t.Fatalf("create fixture schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		// The test's own context is usually cancelled by now, so the drop gets
		// a fresh deadline.
		dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := db.ExecContext(dropCtx, `DROP SCHEMA `+quoted+` CASCADE`); err != nil {
			t.Errorf("drop fixture schema %s: %v", schema, err)
		}
	})
	return db, schema
}

// requireRuntimeDigestFixtureCreatePrivilege fails the test when the DSN role
// cannot create a schema.
//
// The fixtures used to be TEMP tables, which any role can create. A per-run
// schema needs CREATE on the database, so a role provisioned with only TEMP
// rights runs these guards for the first time here.
//
// Failing is the deliberate choice over skipping. These two guards are the only
// thing standing between the #5789 starvation bug and a silent return, and a
// guard that quietly does not run is the same class of defect the PR fixes: an
// absent answer that looks like a passing one. A missing GRANT is a five-second
// fix for whoever set the DSN, and they only find out about it if we say so.
//
// Falling back to TEMP tables would be worse than either — it would restore the
// exact reconnect defect this file exists to remove.
func requireRuntimeDigestFixtureCreatePrivilege(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	var allowed bool
	err := db.QueryRowContext(
		ctx,
		`SELECT has_database_privilege(current_user, current_database(), 'CREATE')`,
	).Scan(&allowed)
	if err != nil {
		t.Fatalf("check CREATE privilege on the ESHU_POSTGRES_TEST_DSN database: %v", err)
	}
	if !allowed {
		var role, database string
		if err := db.QueryRowContext(ctx, `SELECT current_user, current_database()`).Scan(&role, &database); err != nil {
			t.Fatalf("read current_user/current_database: %v", err)
		}
		t.Fatalf(
			"role %q lacks CREATE on database %q, so this test cannot create its per-run fixture schema.\n"+
				"Run: GRANT CREATE ON DATABASE %s TO %s;\n"+
				"These fixtures were TEMP tables, which need no such grant, until a reconnect was found to "+
				"silently resolve them against the real tables. Skipping instead of failing would leave the "+
				"#5789 starvation guard quietly not running, and falling back to TEMP tables would bring the "+
				"reconnect defect back.",
			role, database, pgx.Identifier{database}.Sanitize(), pgx.Identifier{role}.Sanitize(),
		)
	}
}

// sweepStaleRuntimeDigestFixtureSchemas drops fixture schemas left behind by
// earlier runs that never reached their cleanup.
//
// t.Cleanup does not run on SIGKILL, a killed container, or a timeout that
// cannot unwind, so on a shared live-test database the per-run schemas would
// otherwise pile up forever. Sweeping at start rather than at end is what makes
// a killed run self-heal on the next one.
//
// The sweep is best effort: a drop that loses a race with a concurrent sweep is
// logged, not fatal. Turning housekeeping into a test failure would just be a
// new flake.
func sweepStaleRuntimeDigestFixtureSchemas(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	// starts_with is a literal prefix match. LIKE would treat the underscores in
	// the prefix as single-character wildcards, widening what the query returns.
	rows, err := db.QueryContext(
		ctx,
		`SELECT nspname FROM pg_namespace WHERE starts_with(nspname, $1) ORDER BY nspname`,
		runtimeDigestFixtureSchemaPrefix,
	)
	if err != nil {
		t.Logf("sweep leftover fixture schemas: list failed, continuing: %v", err)
		return
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			t.Logf("sweep leftover fixture schemas: scan failed, continuing: %v", err)
			return
		}
		names = append(names, name)
	}
	closeErr := rows.Close()
	if err := rows.Err(); err != nil {
		t.Logf("sweep leftover fixture schemas: iterate failed, continuing: %v", err)
		return
	}
	if closeErr != nil {
		t.Logf("sweep leftover fixture schemas: close failed, continuing: %v", closeErr)
		return
	}

	for _, name := range staleRuntimeDigestFixtureSchemas(names, time.Now(), runtimeDigestFixtureSchemaMaxAge) {
		if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+pgx.Identifier{name}.Sanitize()+` CASCADE`); err != nil {
			t.Logf("sweep leftover fixture schema %s: %v", name, err)
			continue
		}
		t.Logf("swept leftover fixture schema %s from a run that never reached its cleanup", name)
	}
}

// staleRuntimeDigestFixtureSchemas picks the schema names the sweep may drop.
//
// A name qualifies only when it matches openRuntimeDigestFixtureDB's own naming
// pattern in full and the timestamp inside it is more than maxAge old. Anything
// that fails either check is left alone, including a name that merely starts
// with the fixture prefix, one carrying an unparseable timestamp, and one dated
// in the future — a clock that has gone backwards is not a reason to delete
// somebody else's schema.
func staleRuntimeDigestFixtureSchemas(names []string, now time.Time, maxAge time.Duration) []string {
	var stale []string
	for _, name := range names {
		match := runtimeDigestFixtureSchemaPattern.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		nanos, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			continue
		}
		age := now.Sub(time.Unix(0, nanos))
		if age <= maxAge {
			continue
		}
		stale = append(stale, name)
	}
	return stale
}

// TestRuntimeDigestFixtureSchemaSurvivesReconnectLive is the guard on the
// mechanism the whole fixture change rests on.
//
// search_path rides as a pgx connection runtime parameter, so Postgres applies
// it at startup on every connection the pool opens — including the replacement
// it opens after the server drops one. A session `SET search_path` would look
// identical for as long as one connection survives, and the two starvation
// guards would both keep passing on it, because neither one ever loses a
// connection. This test is the only thing that tells the two shapes apart.
//
// It runs with SetMaxOpenConns(1), the pool setting the TEMP-table fixtures used
// to rely on, so the terminated backend and its replacement are the same slot
// and the pid comparison is exact.
func TestRuntimeDigestFixtureSchemaSurvivesReconnectLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live fixture-schema reconnect proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	db, schema := openRuntimeDigestFixtureDB(t, ctx, dsn)
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, `CREATE TABLE fixture_marker (marker text PRIMARY KEY)`); err != nil {
		t.Fatalf("create fixture marker table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fixture_marker VALUES ('present')`); err != nil {
		t.Fatalf("seed fixture marker row: %v", err)
	}

	var beforePID int
	if err := db.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&beforePID); err != nil {
		t.Fatalf("read backend pid before the reconnect: %v", err)
	}

	// Killing our own backend always returns an error, and that error is the
	// point: it forces database/sql to discard the connection and open a
	// replacement for the next statement.
	if _, err := db.ExecContext(ctx, `SELECT pg_terminate_backend(pg_backend_pid())`); err == nil {
		t.Fatal("pg_terminate_backend(pg_backend_pid()) returned no error, so the backend was not terminated " +
			"and this test never exercised a reconnect")
	}

	var (
		afterPID int
		resolved string
		marker   string
	)
	err := db.QueryRowContext(
		ctx,
		`SELECT pg_backend_pid(), current_schema(), (SELECT marker FROM fixture_marker)`,
	).Scan(&afterPID, &resolved, &marker)
	if err != nil {
		t.Fatalf(
			"query after the reconnect: %v\nthe replacement connection could not resolve the fixture schema, "+
				"so search_path is not being applied at connection startup",
			err,
		)
	}

	if afterPID == beforePID {
		t.Fatalf("backend pid is still %d after pg_terminate_backend: no reconnect happened, so this run "+
			"proved nothing about replacement connections", beforePID)
	}
	if resolved != schema {
		t.Fatalf(
			"current_schema() after the reconnect = %q, want %q: the replacement connection did not get the "+
				"fixture search_path, so it is session-scoped and every fixture read after a dropped "+
				"connection silently resolves against the real Eshu tables",
			resolved, schema,
		)
	}
	if marker != "present" {
		t.Fatalf("unqualified read of fixture_marker after the reconnect = %q, want %q", marker, "present")
	}
}

// TestRuntimeDigestFixtureSweepDropsOnlyStaleFixtureSchemasLive proves the
// start-of-run sweep is scoped tightly enough to be safe on a shared database.
//
// The sweep deletes schemas, so its blast radius is the only thing worth
// testing about it. This seeds one schema the sweep must drop and four it must
// leave alone, then opens a SECOND fixture handle and lets that handle's own
// start-of-run sweep do the work. Calling the sweep function directly would
// pass just as well with the sweep unwired from openRuntimeDigestFixtureDB,
// which is the case that matters: a killed run only self-heals if the next run
// sweeps without being asked.
func TestRuntimeDigestFixtureSweepDropsOnlyStaleFixtureSchemasLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live fixture-schema sweep proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	db, _ := openRuntimeDigestFixtureDB(t, ctx, dsn)

	ancient := time.Now().Add(-24 * time.Hour).UnixNano()
	fresh := time.Now().UnixNano()
	cases := []struct {
		schema string
		swept  bool
		why    string
	}{
		{
			schema: fmt.Sprintf("%s%d_%d", runtimeDigestFixtureSchemaPrefix, os.Getpid(), ancient),
			swept:  true,
			why:    "a day-old fixture schema is what a killed run leaves behind",
		},
		{
			schema: fmt.Sprintf("%s%d_%d", runtimeDigestFixtureSchemaPrefix, os.Getpid(), fresh),
			why:    "a fixture schema created seconds ago may belong to a concurrent run",
		},
		{
			schema: runtimeDigestFixtureSchemaPrefix + "keep",
			why:    "carries the prefix but not the naming pattern, so this helper did not create it",
		},
		{
			schema: fmt.Sprintf("%s%d_%d_extra", runtimeDigestFixtureSchemaPrefix, os.Getpid(), ancient),
			why:    "old enough, but the trailing segment means the pattern does not match in full",
		},
		{
			schema: fmt.Sprintf("eshu_digest_fixtures_%d_%d", os.Getpid(), ancient),
			why:    "an adjacent prefix; the sweep must not treat prefix underscores as wildcards",
		},
	}

	for _, testCase := range cases {
		quoted := pgx.Identifier{testCase.schema}.Sanitize()
		if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+quoted); err != nil {
			t.Fatalf("create sweep fixture schema %s: %v", testCase.schema, err)
		}
		t.Cleanup(func() {
			dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer dropCancel()
			if _, err := db.ExecContext(dropCtx, `DROP SCHEMA IF EXISTS `+quoted+` CASCADE`); err != nil {
				t.Errorf("drop sweep fixture schema %s: %v", testCase.schema, err)
			}
		})
	}

	// Opening a second fixture handle is what runs the sweep, exactly as the
	// next run after a killed one would.
	openRuntimeDigestFixtureDB(t, ctx, dsn)

	for _, testCase := range cases {
		var exists bool
		err := db.QueryRowContext(
			ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`,
			testCase.schema,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check sweep fixture schema %s: %v", testCase.schema, err)
		}
		if testCase.swept && exists {
			t.Fatalf("schema %s survived the sweep, want it dropped: %s", testCase.schema, testCase.why)
		}
		if !testCase.swept && !exists {
			t.Fatalf("schema %s was dropped by the sweep, want it left alone: %s", testCase.schema, testCase.why)
		}
	}
}
