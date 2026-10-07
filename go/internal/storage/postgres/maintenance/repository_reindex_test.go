// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenancestore_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/maintenance"
)

func TestRepositoryReindexStoreRequestSortsAndDeduplicatesScopes(t *testing.T) {
	t.Parallel()

	stored := time.Date(2026, 10, 7, 12, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{{Data: [][]any{
			{"git-repository-scope:a", stored},
			{"git-repository-scope:b", stored},
		}}},
	}
	store := maintenancestore.NewRepositoryReindexStore(database)

	got, err := store.RequestRepositoryReindex(context.Background(), []string{
		"git-repository-scope:b", "git-repository-scope:a", "git-repository-scope:b",
	})
	if err != nil {
		t.Fatalf("RequestRepositoryReindex() error = %v, want nil", err)
	}
	if gotLen, want := len(database.Queries), 1; gotLen != want {
		t.Fatalf("query count = %d, want %d (one statement per request)", gotLen, want)
	}
	args := database.Queries[0].Args
	if gotLen, want := len(args), 1; gotLen != want {
		t.Fatalf("query args = %d, want %d (the timestamp must come from the database clock)", gotLen, want)
	}
	if want := []string{"git-repository-scope:a", "git-repository-scope:b"}; !reflect.DeepEqual(args[0], want) {
		t.Fatalf("bound scope ids = %#v, want sorted and deduplicated %#v", args[0], want)
	}
	if gotLen, want := len(got), 2; gotLen != want {
		t.Fatalf("returned records = %d, want %d", gotLen, want)
	}
	for _, record := range got {
		if !record.RequestedAt.Equal(stored) || record.RequestedAt.Location() != time.UTC {
			t.Fatalf("record %q RequestedAt = %v, want %v in UTC", record.ScopeID, record.RequestedAt, stored.UTC())
		}
	}
	if got[0].ScopeID != "git-repository-scope:a" || got[1].ScopeID != "git-repository-scope:b" {
		t.Fatalf("records = %+v, want ordered by scope id", got)
	}
}

func TestRepositoryReindexStoreRequestQueryIsMonotonicSortedAndDistinct(t *testing.T) {
	t.Parallel()

	query := maintenancestore.RequestRepositoryReindexQuery
	for _, want := range []string{
		"INSERT INTO repository_reindex_requests",
		"SELECT DISTINCT unnest($1::text[])",
		"ORDER BY 1",
		"now()",
		"ON CONFLICT (scope_id) DO UPDATE",
		"GREATEST(",
		"RETURNING scope_id, requested_at",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("request query missing %q: %s", want, query)
		}
	}
}

func TestRepositoryReindexStoreRequestRejectsEmptyAndBlankScopes(t *testing.T) {
	t.Parallel()

	for name, scopeIDs := range map[string][]string{
		"empty": nil,
		"blank": {"git-repository-scope:a", "  "},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			database := &fake.ExecQueryer{}
			store := maintenancestore.NewRepositoryReindexStore(database)
			if _, err := store.RequestRepositoryReindex(context.Background(), scopeIDs); err == nil {
				t.Fatal("RequestRepositoryReindex() error = nil, want non-nil")
			}
			if got := len(database.Queries); got != 0 {
				t.Fatalf("query count = %d, want 0 (nothing may be written)", got)
			}
		})
	}
}

func TestRepositoryReindexStoreRequestErrorsWhenRowsMissing(t *testing.T) {
	t.Parallel()

	stored := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{{Data: [][]any{{"git-repository-scope:a", stored}}}},
	}
	store := maintenancestore.NewRepositoryReindexStore(database)

	_, err := store.RequestRepositoryReindex(context.Background(), []string{"git-repository-scope:a", "git-repository-scope:b"})
	if err == nil {
		t.Fatal("RequestRepositoryReindex() error = nil, want non-nil when RETURNING yields fewer rows than requested")
	}
}

func TestRepositoryReindexStoreWatermarksReadsRowsNewerThanFleet(t *testing.T) {
	t.Parallel()

	stored := time.Date(2026, 10, 7, 12, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{{Data: [][]any{{"git-repository-scope:a", stored}}}},
	}
	store := maintenancestore.NewRepositoryReindexStore(database)
	after := time.Date(2026, 10, 1, 0, 0, 0, 0, time.FixedZone("PDT", -7*3600))

	got, err := store.RepositoryReindexWatermarks(context.Background(), after)
	if err != nil {
		t.Fatalf("RepositoryReindexWatermarks() error = %v, want nil", err)
	}
	if want := map[string]time.Time{"git-repository-scope:a": stored.UTC()}; !reflect.DeepEqual(got, want) {
		t.Fatalf("watermarks = %v, want %v", got, want)
	}
	if location := got["git-repository-scope:a"].Location(); location != time.UTC {
		t.Fatalf("watermark location = %v, want UTC", location)
	}
	query := database.Queries[0]
	if !strings.Contains(query.Query, "WHERE requested_at > $1") {
		t.Fatalf("watermark query missing fleet filter: %s", query.Query)
	}
	if bound, ok := query.Args[0].(time.Time); !ok || !bound.Equal(after) || bound.Location() != time.UTC {
		t.Fatalf("bound after = %#v, want %v in UTC", query.Args[0], after.UTC())
	}
}

func TestRepositoryReindexStoreRequiresDatabase(t *testing.T) {
	t.Parallel()

	store := maintenancestore.NewRepositoryReindexStore(nil)
	if _, err := store.RequestRepositoryReindex(context.Background(), []string{"git-repository-scope:a"}); err == nil {
		t.Fatal("RequestRepositoryReindex() error = nil, want non-nil without a database")
	}
	if _, err := store.RepositoryReindexWatermarks(context.Background(), time.Time{}); err == nil {
		t.Fatal("RepositoryReindexWatermarks() error = nil, want non-nil without a database")
	}
}

func TestRepositoryReindexSchemaMatchesEmbeddedMigration(t *testing.T) {
	t.Parallel()

	for _, definition := range postgres.BootstrapDefinitions() {
		if strings.HasSuffix(definition.Path, "/162_repository_reindex_requests.sql") {
			if !strings.Contains(definition.SQL, strings.TrimSpace(maintenancestore.RepositoryReindexSchemaSQL)) {
				t.Fatalf("migration 162 does not contain the store's DDL:\n%s", maintenancestore.RepositoryReindexSchemaSQL)
			}
			return
		}
	}
	t.Fatal("migration 162_repository_reindex_requests.sql is not embedded")
}
