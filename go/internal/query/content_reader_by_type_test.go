// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// TestListRepoEntitiesByTypeOrdersByEntityIDTiebreaker proves the ORDER BY
// clause carries entity_id as a tiebreaker after relative_path, start_line
// (#5343 review P2: one-token determinism hardening). Without it, which rows
// land past a truncated LIMIT (see fetchK8sResourceCandidates in
// content_relationships.go) is unspecified among rows sharing a
// relative_path/start_line -- this makes the drop reproducible.
func TestListRepoEntitiesByTypeOrdersByEntityIDTiebreaker(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"deployment-1", "repo-1", "deploy/deployment.yaml", "K8sResource", "demo",
					int64(1), int64(20), "yaml", "", []byte(`{}`),
				},
			},
			queryContainsInOrder: []string{
				"FROM content_entities",
				"WHERE repo_id = $1 AND entity_type = $2",
				"ORDER BY relative_path, start_line, entity_id",
				"LIMIT $3",
			},
			wantArgs: []driver.Value{"repo-1", "K8sResource", int64(10)},
		},
	})

	reader := NewContentReader(db)
	if _, err := reader.ListRepoEntitiesByType(context.Background(), "repo-1", "K8sResource", 10); err != nil {
		t.Fatalf("ListRepoEntitiesByType() error = %v, want nil", err)
	}
}

// TestListRepoEntitiesByTypesIssuesTypeFilteredQuery is the only test that
// reads the SQL ListRepoEntitiesByTypes actually issues. The repository
// infrastructure tests that drive this method reach it through
// fakePortContentStore (ports_test.go), which reimplements the type filter in
// Go and therefore passes identically whether the query filters or not -- the
// #5764 round-7 P1 finding.
//
// This test's queryContainsInOrder assertion is a SHAPE check on the SQL
// string, not a full-text one, so it catches the mutations named below but
// not every possible one: a change that keeps all four listed fragments while
// altering the query's overall meaning (for example appending `OR true` to
// the WHERE clause) still passes this assertion, because every fragment is
// still present in order (round-8 P3-1 review follow-up narrows the earlier
// "has to fail here" claim to name this limit). wantArgs below separately
// covers the bind values reaching Postgres (round-8 P2-2 review follow-up),
// so an argument-order swap fails even though it changes neither the query
// string nor its fragments.
//
// Inverting the predicate to `entity_type <> ALL($2::text[])` (every
// NON-infrastructure entity, the round-6 defect in worse form), dropping the
// entity_id ORDER BY tiebreaker, or renaming the table each fails here,
// because the fake driver rejects a query that does not carry these
// fragments in this order.
func TestListRepoEntitiesByTypesIssuesTypeFilteredQuery(t *testing.T) {
	t.Parallel()

	entityTypes := []string{"K8sResource", "TerraformResource"}
	wantTypesArg, err := pgarray.Array(entityTypes).Value()
	if err != nil {
		t.Fatalf("pgarray.Array(entityTypes).Value() error = %v, want nil", err)
	}

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"deployment-1", "repo-1", "deploy/deployment.yaml", "K8sResource", "demo",
					int64(1), int64(20), "yaml", "", []byte(`{}`),
				},
				{
					"tf-1", "repo-1", "infra/main.tf", "TerraformResource", "bucket",
					int64(3), int64(9), "hcl", "", []byte(`{}`),
				},
			},
			queryContainsInOrder: []string{
				"FROM content_entities",
				"WHERE repo_id = $1 AND entity_type = ANY($2::text[])",
				"ORDER BY relative_path, start_line, entity_id",
				"LIMIT $3",
			},
			wantArgs: []driver.Value{"repo-1", wantTypesArg, int64(10)},
		},
	})

	reader := NewContentReader(db)
	entities, err := reader.ListRepoEntitiesByTypes(
		context.Background(), "repo-1", entityTypes, 10,
	)
	if err != nil {
		t.Fatalf("ListRepoEntitiesByTypes() error = %v, want nil", err)
	}
	if len(entities) != 2 {
		t.Fatalf("len(ListRepoEntitiesByTypes) = %d, want 2: %#v", len(entities), entities)
	}
	if got, want := entities[0].EntityType, "K8sResource"; got != want {
		t.Fatalf("entities[0].EntityType = %q, want %q", got, want)
	}
	if got, want := entities[1].EntityType, "TerraformResource"; got != want {
		t.Fatalf("entities[1].EntityType = %q, want %q", got, want)
	}
}

// TestListRepoEntitiesByTypesEmptyTypeListIssuesNoQuery pins the empty-type-list
// guard to "no query at all", not "a query that matches everything". The fake
// driver is opened with no queued result, so any query reaching it fails with
// "unexpected query" -- deleting `if len(entityTypes) == 0 { return nil, nil }`
// sends `entity_type = ANY(ARRAY[]::text[])` to Postgres and fails this test
// here (#5764 round-7 P1).
func TestListRepoEntitiesByTypesEmptyTypeListIssuesNoQuery(t *testing.T) {
	t.Parallel()

	reader := NewContentReader(openContentReaderTestDB(t, nil))
	entities, err := reader.ListRepoEntitiesByTypes(context.Background(), "repo-1", nil, 10)
	if err != nil {
		t.Fatalf("ListRepoEntitiesByTypes(nil types) error = %v, want nil", err)
	}
	if entities != nil {
		t.Fatalf("ListRepoEntitiesByTypes(nil types) = %#v, want nil", entities)
	}
}
