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
	testPrincipal = membership.GitHubAppPrincipal("1", "2")
	testSelector  = membership.NewGitHubOrgSelector("githubOrg", "acme", nil, false, testPrincipal)
	evaluatedAt   = time.Date(2026, 10, 8, 6, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	testWindow    = 48 * time.Hour
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
	since := listedAt.Add(-time.Hour)
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{
		{
			"git-repository-scope:a", "selected",
			sql.NullInt64{Int64: 7, Valid: true},
			sql.NullTime{Time: listedAt, Valid: true},
			since, 3, evaluatedAt, 172800,
		},
		{
			"git-repository-scope:b", "not_listed",
			sql.NullInt64{},
			sql.NullTime{},
			listedAt, 2, evaluatedAt, 3600,
		},
	}}}}
	got, err := membershipstore.NewObservationStore(database).Observations(context.Background(), testSelector.ID)
	if err != nil {
		t.Fatalf("Observations() error = %v", err)
	}
	want := []membership.Observation{
		{
			ScopeID: "git-repository-scope:a", State: membership.StateSelected, GitHubRepoID: 7,
			LastListedAt: listedAt.UTC(), StateSince: since.UTC(), StateCycleCount: 3,
			EvaluatedAt: evaluatedAt.UTC(), LivenessWindow: testWindow,
		},
		{
			ScopeID: "git-repository-scope:b", State: membership.StateNotListed,
			StateSince: listedAt.UTC(), StateCycleCount: 2, EvaluatedAt: evaluatedAt.UTC(), LivenessWindow: time.Hour,
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
		Selector:       testSelector,
		EvaluatedAt:    evaluatedAt,
		LivenessWindow: testWindow,
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
		testSelector.ID, membership.KindGitHubOrg, "acme", evaluatedAt.UTC(), int64(172800),
		[]string{"git-repository-scope:a", "git-repository-scope:b", "git-repository-scope:c"},
		[]string{"selected", "archived_excluded", "not_listed"},
		[]int64{11, 12, 0},
	}
	if got := database.Execs[0].Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("bound args = %#v, want %#v", got, want)
	}
}

func TestObservationStoreUpsertAcceptsExplicitSelectedRows(t *testing.T) {
	t.Parallel()

	explicit := membership.NewExplicitSelector("explicit", "acme", nil, testPrincipal)
	database := &fake.ExecQueryer{}
	err := membershipstore.NewObservationStore(database).UpsertObservations(context.Background(), membership.Batch{
		Selector: explicit, EvaluatedAt: evaluatedAt, LivenessWindow: testWindow,
		Rows: []membership.Row{{ScopeID: "git-repository-scope:a", State: membership.StateSelected}},
	})
	if err != nil || len(database.Execs) != 1 {
		t.Fatalf("explicit upsert = %v with %d execs, want one statement", err, len(database.Execs))
	}
	if got := database.Execs[0].Args[1]; got != membership.KindExplicit {
		t.Fatalf("bound selector kind = %v, want %q", got, membership.KindExplicit)
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
		"state_since = CASE WHEN o.state = EXCLUDED.state THEN o.state_since ELSE EXCLUDED.state_since END",
		"state_cycle_count = CASE WHEN o.state = EXCLUDED.state THEN o.state_cycle_count + 1 ELSE 1 END",
		"liveness_window_seconds = EXCLUDED.liveness_window_seconds",
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
		Selector: testSelector, EvaluatedAt: evaluatedAt, LivenessWindow: testWindow,
	})
	if err != nil || len(database.Execs) != 0 {
		t.Fatalf("UpsertObservations(empty) = %v with %d execs, want nil and none", err, len(database.Execs))
	}
}

func TestObservationStoreRejectsInvalidInputWithoutWriting(t *testing.T) {
	t.Parallel()

	valid := func() membership.Batch {
		return membership.Batch{
			Selector: testSelector, EvaluatedAt: evaluatedAt, LivenessWindow: testWindow,
			Rows: []membership.Row{{ScopeID: "git-repository-scope:a", State: membership.StateSelected}},
		}
	}
	explicit := membership.NewExplicitSelector("explicit", "acme", nil, testPrincipal)
	cases := map[string]func(*membership.Batch){
		"blank selector id": func(b *membership.Batch) { b.Selector.ID = " " },
		"unknown kind":      func(b *membership.Batch) { b.Selector.Kind = "gitlab_group" },
		"blank owner":       func(b *membership.Batch) { b.Selector.Owner = "" },
		"zero evaluated_at": func(b *membership.Batch) { b.EvaluatedAt = time.Time{} },
		"sub-second window": func(b *membership.Batch) { b.LivenessWindow = time.Millisecond },
		"zero window":       func(b *membership.Batch) { b.LivenessWindow = 0 },
		"window too large":  func(b *membership.Batch) { b.LivenessWindow = 1 << 62 },
		"blank scope id":    func(b *membership.Batch) { b.Rows[0].ScopeID = "" },
		"unknown state":     func(b *membership.Batch) { b.Rows[0].State = "hidden" },
		"negative id":       func(b *membership.Batch) { b.Rows[0].GitHubRepoID = -1 },
		"conflicting rows": func(b *membership.Batch) {
			b.Rows = append(b.Rows, membership.Row{ScopeID: "git-repository-scope:a", State: membership.StateNotListed})
		},
		"explicit not_listed row": func(b *membership.Batch) {
			b.Selector = explicit
			b.Rows[0].State = membership.StateNotListed
		},
		"explicit excluded row": func(b *membership.Batch) {
			b.Selector = explicit
			b.Rows[0].State = membership.StateRuleExcluded
		},
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
		Selector: testSelector, EvaluatedAt: evaluatedAt, LivenessWindow: testWindow,
		Rows: []membership.Row{{ScopeID: "git-repository-scope:a", State: membership.StateSelected}},
	}
	if err := store.UpsertObservations(context.Background(), batch); err == nil {
		t.Fatal("UpsertObservations() error = nil without a database")
	}
}

// TestObservationSchemaEqualsEmbeddedMigration keeps the store's DDL and
// migration 163 identical: the migration with its leading comment block and
// blank lines removed must equal schemaSQL.
func TestObservationSchemaEqualsEmbeddedMigration(t *testing.T) {
	t.Parallel()

	for _, definition := range postgres.BootstrapDefinitions() {
		if !strings.HasSuffix(definition.Path, "/163_repository_selection_observations.sql") {
			continue
		}
		lines := strings.Split(definition.SQL, "\n")
		for len(lines) > 0 && (strings.HasPrefix(lines[0], "--") || strings.TrimSpace(lines[0]) == "") {
			lines = lines[1:]
		}
		got := strings.TrimSpace(strings.Join(lines, "\n"))
		if want := strings.TrimSpace(membershipstore.SchemaSQL); got != want {
			t.Fatalf("migration 163 DDL differs from the store's schemaSQL:\nmigration:\n%s\nstore:\n%s", got, want)
		}
		return
	}
	t.Fatal("migration 163_repository_selection_observations.sql is not embedded")
}
