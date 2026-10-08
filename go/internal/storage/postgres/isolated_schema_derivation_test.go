// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestReducerFairnessIsolatedSchemaDerivesFromBootstrap is the seeded-violation
// guard for the isolated-schema drift class behind #6680 and #7493: a live
// proof helper that hand-picks bootstrap definitions (or hand-writes DDL)
// silently falls behind the production schema, and the proof then fails with
// a missing column, table, or function far from the real cause.
//
// The guard builds one schema through the helper under test and one through
// the production bootstrap, then diffs their schema objects (relations,
// columns, indexes, sequences, routines, triggers, constraints). Any object
// the helper-built schema lacks fails the test. Extension-owned objects are
// excluded: whichever schema bootstraps first in a pristine database hosts
// pg_trgm, so they can never match by construction.
//
// It is RED on the pre-fix helpers (both lack production objects) and GREEN
// once the helpers derive from ApplyBootstrapWithoutContentSearchIndexes.
// It is enrolled in the reducer contention gate so a reintroduced hand-picked
// list fails a PR, not the scheduled lane.
func TestReducerFairnessIsolatedSchemaDerivesFromBootstrap(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the isolated-schema derivation guard")
	}

	ctx := context.Background()
	helperDB, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	// The reference is the same production bootstrap the deadlock proof
	// applies: full definitions minus the deferred content-search indexes.
	referenceDB := openClaimDeadlockProofDB(t, dsn, 1)
	assertIsolatedSchemaMatchesBootstrap(t, ctx, helperDB, referenceDB)
}

// assertIsolatedSchemaMatchesBootstrap fails when the schema behind helperDB
// lacks any object present in the schema behind referenceDB, or carries an
// object the reference does not have. Each handle is queried through its own
// search_path so type names render identically on both sides.
func assertIsolatedSchemaMatchesBootstrap(t *testing.T, ctx context.Context, helperDB, referenceDB *sql.DB) {
	t.Helper()

	helperSchema := isolatedSchemaCurrentSchema(t, ctx, helperDB)
	referenceSchema := isolatedSchemaCurrentSchema(t, ctx, referenceDB)
	helperObjects := postgresproof.ListSchemaObjects(t, ctx, helperDB, helperSchema)
	referenceObjects := postgresproof.ListSchemaObjects(t, ctx, referenceDB, referenceSchema)

	helperSet := make(map[string]struct{}, len(helperObjects))
	for _, object := range helperObjects {
		helperSet[object] = struct{}{}
	}
	referenceSet := make(map[string]struct{}, len(referenceObjects))
	for _, object := range referenceObjects {
		referenceSet[object] = struct{}{}
	}
	var missing, extra []string
	for _, object := range referenceObjects {
		if _, ok := helperSet[object]; !ok {
			missing = append(missing, object)
		}
	}
	for _, object := range helperObjects {
		if _, ok := referenceSet[object]; !ok {
			extra = append(extra, object)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return
	}
	t.Fatalf("isolated schema %q drifted from the production bootstrap %q: %d object(s) missing, %d extra\nmissing: %v\nextra: %v",
		helperSchema, referenceSchema, len(missing), len(extra),
		postgresproof.FirstSchemaObjects(missing, 20), postgresproof.FirstSchemaObjects(extra, 20))
}

// isolatedSchemaCurrentSchema returns the schema a proof handle builds into:
// the first entry of its pinned search_path.
func isolatedSchemaCurrentSchema(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()

	var schema string
	if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatalf("read current_schema: %v", err)
	}
	if schema == "" {
		t.Fatal("current_schema() is empty: the proof handle lost its pinned search_path")
	}
	return schema
}
