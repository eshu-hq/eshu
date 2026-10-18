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

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

func TestReadLiveScopeObservationsBatchQueryShape(t *testing.T) {
	t.Parallel()

	query := membershipstore.LiveScopeObservationsBatchQuery
	for _, want := range []string{
		"WHERE scope_id = ANY($1)",
		"evaluated_at + make_interval(secs => liveness_window_seconds) >= $2",
		"upstream_classification, successor_slug, classified_at",
		"ORDER BY scope_id, selector_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch query missing %q: %s", want, query)
		}
	}
}

func TestReadLiveScopeObservationsBatchGroupsByScope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	since := now.Add(-2 * 24 * time.Hour)
	classifiedAt := now.Add(-time.Hour)
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{
		{
			"scope:a", "sel-a", "selected",
			sql.NullTime{Time: now, Valid: true},
			since, 3, now, 172800,
			sql.NullString{},
			sql.NullString{},
			sql.NullTime{},
		},
		{
			"scope:b", "sel-a", "not_listed",
			sql.NullTime{},
			since, 3, now, 172800,
			sql.NullString{String: "transferred", Valid: true},
			sql.NullString{String: "archive/b", Valid: true},
			sql.NullTime{Time: classifiedAt, Valid: true},
		},
		{
			"scope:b", "sel-b", "not_listed",
			sql.NullTime{},
			since, 2, now.Add(-time.Minute), 172800,
			sql.NullString{},
			sql.NullString{},
			sql.NullTime{},
		},
	}}}}
	got, err := membershipstore.ReadLiveScopeObservationsBatch(
		context.Background(), database, []string{"scope:a", "scope:b"}, now,
	)
	if err != nil {
		t.Fatalf("ReadLiveScopeObservationsBatch() error = %v", err)
	}
	if len(database.Queries) != 1 {
		t.Fatalf("query count = %d, want one statement for the page", len(database.Queries))
	}
	if want := []any{[]string{"scope:a", "scope:b"}, now.UTC()}; !reflect.DeepEqual(database.Queries[0].Args, want) {
		t.Fatalf("bound args = %#v, want %#v", database.Queries[0].Args, want)
	}
	if len(got) != 2 || len(got["scope:a"]) != 1 || len(got["scope:b"]) != 2 {
		t.Fatalf("grouped = %+v, want one row for scope:a and two for scope:b", got)
	}
	row := got["scope:b"][0]
	if row.SelectorID != "sel-a" || row.State != selection.StateNotListed ||
		row.Classification != selection.UpstreamTransferred || row.SuccessorSlug != "archive/b" ||
		!row.ClassifiedAt.Equal(classifiedAt.UTC()) {
		t.Fatalf("scope:b sel-a row = %+v, want the transferred verdict carried", row)
	}
}

func TestReadLiveScopeObservationsBatchEmptyReadsNothing(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{}
	got, err := membershipstore.ReadLiveScopeObservationsBatch(
		context.Background(), database, nil, time.Now(),
	)
	if err != nil {
		t.Fatalf("ReadLiveScopeObservationsBatch(nil) error = %v", err)
	}
	if len(got) != 0 || len(database.Queries) != 0 {
		t.Fatalf("empty scope set must read nothing, got %+v with %d queries", got, len(database.Queries))
	}
	if _, err := membershipstore.ReadLiveScopeObservationsBatch(context.Background(), nil, []string{"s"}, time.Now()); err == nil {
		t.Fatal("ReadLiveScopeObservationsBatch(nil queryer) error = nil, want an error")
	}
}
