// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

func TestReadLiveScopeObservationsBindsTheLiveFilter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 8, 6, 0, 0, 123456789, time.FixedZone("EDT", -4*3600))
	listedAt := now.Add(-time.Hour)
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{
		{"sel-a@app:1:2", "not_listed", sql.NullTime{}, listedAt, 2, now.Add(-time.Minute), 172800},
		{"sel-b@token:abc", "selected", sql.NullTime{Time: listedAt, Valid: true}, listedAt, 1, listedAt, 3600},
	}}}}
	got, err := membershipstore.ReadLiveScopeObservations(context.Background(), database, "scope:a", now)
	if err != nil {
		t.Fatalf("ReadLiveScopeObservations() error = %v", err)
	}
	want := []selection.Observation{
		{
			SelectorID: "sel-a@app:1:2", State: selection.StateNotListed, StateSince: listedAt.UTC(),
			StateCycleCount: 2, EvaluatedAt: now.Add(-time.Minute).UTC(), LivenessWindow: 48 * time.Hour,
		},
		{
			SelectorID: "sel-b@token:abc", State: selection.StateSelected, LastListedAt: listedAt.UTC(),
			StateSince: listedAt.UTC(), StateCycleCount: 1, EvaluatedAt: listedAt.UTC(), LivenessWindow: time.Hour,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadLiveScopeObservations() = %+v, want %+v", got, want)
	}
	wantArgs := []any{"scope:a", now.UTC().Truncate(time.Microsecond)}
	if args := database.Queries[0].Args; !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("bound args = %#v, want %#v", args, wantArgs)
	}
	query := membershipstore.LiveScopeObservationsQuery
	for _, fragment := range []string{
		"FROM repository_selection_observations",
		"WHERE scope_id = $1",
		"evaluated_at + make_interval(secs => liveness_window_seconds) >= $2",
		"ORDER BY selector_id",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("live read query missing %q: %s", fragment, query)
		}
	}
}

func TestReadLiveScopeObservationsWrapsErrors(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{FailWith: errors.New("conn reset")}}}
	if _, err := membershipstore.ReadLiveScopeObservations(context.Background(), database, "scope:a", time.Now()); err == nil ||
		!strings.Contains(err.Error(), "conn reset") {
		t.Fatalf("ReadLiveScopeObservations() error = %v, want the wrapped query error", err)
	}
	if _, err := membershipstore.ReadLiveScopeObservations(context.Background(), nil, "scope:a", time.Now()); err == nil {
		t.Fatal("ReadLiveScopeObservations(nil queryer) error = nil")
	}
	if _, err := membershipstore.ReadLiveScopeObservations(context.Background(), &fake.ExecQueryer{}, " ", time.Now()); err == nil {
		t.Fatal("ReadLiveScopeObservations(blank scope) error = nil")
	}
}
