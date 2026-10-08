// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

var (
	testSelector = membership.NewGitHubOrgSelector("githubOrg", "acme", nil, false)
	evaluatedAt  = time.Date(2026, 10, 8, 6, 0, 0, 0, time.FixedZone("EDT", -4*3600))
)

func TestObservationStoreKnownScopesReadsTheOrgPartition(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{
		{"git-repository-scope:a", "acme/a"},
		{"git-repository-scope:b", "Acme/b"},
	}}}}
	got, err := membershipstore.NewObservationStore(database).KnownScopes(context.Background(), " Acme ")
	if err != nil {
		t.Fatalf("KnownScopes() error = %v", err)
	}
	want := []membership.KnownScope{
		{ScopeID: "git-repository-scope:a", Slug: "acme/a"},
		{ScopeID: "git-repository-scope:b", Slug: "Acme/b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("KnownScopes() = %+v, want %+v", got, want)
	}
	query := database.Queries[0]
	if !reflect.DeepEqual(query.Args, []any{"acme"}) {
		t.Fatalf("bound owner = %#v, want the trimmed lowercased org", query.Args)
	}
	knownScopesSQL := membershipstore.KnownScopesQuery
	for _, want := range []string{
		"source_system = 'git'",
		"scope_kind = 'repository'",
		"collector_kind = 'git'",
		"lower(split_part(payload->>'repo_slug', '/', 1)) = $1",
	} {
		if !strings.Contains(knownScopesSQL, want) {
			t.Fatalf("known scopes query missing %q: %s", want, knownScopesSQL)
		}
	}
}

func TestObservationStoreObservationsMapsNullsAndUTC(t *testing.T) {
	t.Parallel()

	listedAt := time.Date(2026, 10, 8, 5, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{
		{
			"git-repository-scope:a", "selected",
			sql.NullInt64{Int64: 7, Valid: true},
			sql.NullTime{Time: listedAt, Valid: true},
			sql.NullTime{},
			0, evaluatedAt, 300,
		},
		{
			"git-repository-scope:b", "not_listed",
			sql.NullInt64{},
			sql.NullTime{},
			sql.NullTime{Time: listedAt, Valid: true},
			2, evaluatedAt, 300,
		},
	}}}}
	got, err := membershipstore.NewObservationStore(database).Observations(context.Background(), testSelector.ID)
	if err != nil {
		t.Fatalf("Observations() error = %v", err)
	}
	want := []membership.Observation{
		{
			ScopeID: "git-repository-scope:a", State: membership.StateSelected, GitHubRepoID: 7,
			LastListedAt: listedAt.UTC(), EvaluatedAt: evaluatedAt.UTC(), EvaluationInterval: 5 * time.Minute,
		},
		{
			ScopeID: "git-repository-scope:b", State: membership.StateNotListed,
			FirstUnlistedAt: listedAt.UTC(), UnlistedCycleCount: 2, EvaluatedAt: evaluatedAt.UTC(), EvaluationInterval: 5 * time.Minute,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Observations() = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(database.Queries[0].Args, []any{testSelector.ID}) {
		t.Fatalf("bound selector = %#v, want %q", database.Queries[0].Args, testSelector.ID)
	}
}

func TestObservationStoreUpsertBindsOneSortedStatement(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{}
	err := membershipstore.NewObservationStore(database).UpsertObservations(context.Background(), membership.Batch{
		Selector:    testSelector,
		EvaluatedAt: evaluatedAt,
		Interval:    5 * time.Minute,
		Rows: []membership.Row{
			{ScopeID: "git-repository-scope:c", State: membership.StateNotListed},
			{ScopeID: "git-repository-scope:a", State: membership.StateSelected, GitHubRepoID: 11},
			{ScopeID: "git-repository-scope:b", State: membership.StateArchivedExcluded, GitHubRepoID: 12},
			{ScopeID: "git-repository-scope:a", State: membership.StateSelected, GitHubRepoID: 11},
		},
	})
	if err != nil {
		t.Fatalf("UpsertObservations() error = %v", err)
	}
	if got := len(database.Execs); got != 1 {
		t.Fatalf("exec count = %d, want one statement per batch", got)
	}
	want := []any{
		testSelector.ID, membership.KindGitHubOrg, "acme", evaluatedAt.UTC(), int64(300),
		[]string{"git-repository-scope:a", "git-repository-scope:b", "git-repository-scope:c"},
		[]string{"selected", "archived_excluded", "not_listed"},
		[]int64{11, 12, 0},
	}
	if got := database.Execs[0].Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("bound args = %#v, want %#v", got, want)
	}
}

func TestObservationStoreUpsertQueryIsAdvanceOnlyAndOrdered(t *testing.T) {
	t.Parallel()

	upsertSQL := membershipstore.UpsertObservationsQuery
	for _, want := range []string{
		"INSERT INTO repository_selection_observations AS o",
		"unnest($6::text[], $7::text[], $8::bigint[])",
		"NULLIF(s.github_repo_id, 0)",
		"ORDER BY s.scope_id",
		"ON CONFLICT (scope_id, selector_id) DO UPDATE",
		"o.unlisted_cycle_count + 1",
		"COALESCE(o.first_unlisted_at, EXCLUDED.evaluated_at)",
		"WHERE o.evaluated_at < EXCLUDED.evaluated_at",
	} {
		if !strings.Contains(upsertSQL, want) {
			t.Fatalf("upsert query missing %q: %s", want, upsertSQL)
		}
	}
}

func TestObservationStoreUpsertWithNoRowsWritesNothing(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{}
	err := membershipstore.NewObservationStore(database).UpsertObservations(context.Background(), membership.Batch{
		Selector: testSelector, EvaluatedAt: evaluatedAt, Interval: 5 * time.Minute,
	})
	if err != nil || len(database.Execs) != 0 {
		t.Fatalf("UpsertObservations(empty) = %v with %d execs, want nil and none", err, len(database.Execs))
	}
}

func TestObservationStoreRejectsInvalidInputWithoutWriting(t *testing.T) {
	t.Parallel()

	valid := func() membership.Batch {
		return membership.Batch{
			Selector: testSelector, EvaluatedAt: evaluatedAt, Interval: 5 * time.Minute,
			Rows: []membership.Row{{ScopeID: "git-repository-scope:a", State: membership.StateSelected}},
		}
	}
	cases := map[string]func(*membership.Batch){
		"blank selector id": func(b *membership.Batch) { b.Selector.ID = " " },
		"unknown kind":      func(b *membership.Batch) { b.Selector.Kind = "gitlab_group" },
		"blank owner":       func(b *membership.Batch) { b.Selector.Owner = "" },
		"zero evaluated_at": func(b *membership.Batch) { b.EvaluatedAt = time.Time{} },
		"sub-second":        func(b *membership.Batch) { b.Interval = time.Millisecond },
		"blank scope id":    func(b *membership.Batch) { b.Rows[0].ScopeID = "" },
		"unknown state":     func(b *membership.Batch) { b.Rows[0].State = "hidden" },
		"negative id":       func(b *membership.Batch) { b.Rows[0].GitHubRepoID = -1 },
		"conflicting rows": func(b *membership.Batch) {
			b.Rows = append(b.Rows, membership.Row{ScopeID: "git-repository-scope:a", State: membership.StateNotListed})
		},
		"interval too large": func(b *membership.Batch) { b.Interval = 1 << 62 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			database := &fake.ExecQueryer{}
			batch := valid()
			mutate(&batch)
			if err := membershipstore.NewObservationStore(database).UpsertObservations(context.Background(), batch); err == nil {
				t.Fatal("UpsertObservations() error = nil, want non-nil")
			}
			if len(database.Execs) != 0 {
				t.Fatalf("exec count = %d, want 0", len(database.Execs))
			}
		})
	}

	store := membershipstore.NewObservationStore(&fake.ExecQueryer{})
	if _, err := store.KnownScopes(context.Background(), " "); err == nil {
		t.Fatal("KnownScopes(blank) error = nil, want non-nil")
	}
	if _, err := store.Observations(context.Background(), ""); err == nil {
		t.Fatal("Observations(blank) error = nil, want non-nil")
	}
}

func TestObservationStoreRequiresDatabase(t *testing.T) {
	t.Parallel()

	store := membershipstore.NewObservationStore(nil)
	if _, err := store.KnownScopes(context.Background(), "acme"); err == nil {
		t.Fatal("KnownScopes() error = nil without a database")
	}
	if _, err := store.Observations(context.Background(), testSelector.ID); err == nil {
		t.Fatal("Observations() error = nil without a database")
	}
	batch := membership.Batch{
		Selector: testSelector, EvaluatedAt: evaluatedAt, Interval: time.Minute,
		Rows: []membership.Row{{ScopeID: "git-repository-scope:a", State: membership.StateSelected}},
	}
	if err := store.UpsertObservations(context.Background(), batch); err == nil {
		t.Fatal("UpsertObservations() error = nil without a database")
	}
}

func TestObservationSchemaMatchesEmbeddedMigration(t *testing.T) {
	t.Parallel()

	for _, definition := range postgres.BootstrapDefinitions() {
		if strings.HasSuffix(definition.Path, "/163_repository_selection_observations.sql") {
			if !strings.Contains(definition.SQL, strings.TrimSpace(membershipstore.SchemaSQL)) {
				t.Fatalf("migration 163 does not contain the store's DDL:\n%s", membershipstore.SchemaSQL)
			}
			return
		}
	}
	t.Fatal("migration 163_repository_selection_observations.sql is not embedded")
}
