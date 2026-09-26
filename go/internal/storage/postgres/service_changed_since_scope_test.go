// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// TestComputeServiceChangedSinceDeltaBindsGrantAndLineage proves the store
// hands the caller's grant, the scope selector and the resolved lineage to the
// two resolve statements in the documented parameter order (#6475). A struct
// field the store never binds would leave every live assertion to the SQL
// defaults, so the arguments are asserted directly.
func TestComputeServiceChangedSinceDeltaBindsGrantAndLineage(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{"scope-a", false, "gen-current", observed, false}}},
		{rows: [][]any{{"gen-prior", observed.Add(-time.Hour)}}},
		{rows: [][]any{}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeServiceChangedSinceDelta(context.Background(), statuspkg.ServiceChangedSinceFilter{
		ServiceID:            " component:default/api ",
		ScopeID:              " scope-a ",
		SinceGenerationID:    "gen-prior",
		Scoped:               true,
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	})
	if err != nil {
		t.Fatalf("ComputeServiceChangedSinceDelta() error = %v", err)
	}
	if summary.ScopeID != "scope-a" || summary.ServiceID != "component:default/api" {
		t.Fatalf("summary = %+v; want the scope-a lineage of the trimmed service id", summary)
	}

	wantResolve := []any{
		"component:default/api", "scope-a", true,
		array.Of([]string{"repo-a"}), array.Of([]string{"scope-a"}),
		changedsince.MaxServiceScopeCandidates + 2,
	}
	if got := queryer.args[0]; !reflect.DeepEqual(got, wantResolve) {
		t.Fatalf("resolve args = %#v, want %#v", got, wantResolve)
	}
	wantPrior := []any{"component:default/api", "gen-prior", sql.NullString{String: "scope-a", Valid: true}}
	if got := queryer.args[1]; !reflect.DeepEqual(got, wantPrior) {
		t.Fatalf("prior args = %#v, want %#v", got, wantPrior)
	}
}

// TestComputeServiceChangedSinceDeltaUnattributedPriorBindsNull pins that the
// unattributed lineage's prior lookup binds SQL NULL, so it can only match
// another unattributed row, never an attributed one with an empty scope id.
func TestComputeServiceChangedSinceDeltaUnattributedPriorBindsNull(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: [][]any{{"", true, "gen-legacy", observed, false}}},
		{rows: [][]any{}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeServiceChangedSinceDelta(context.Background(), statuspkg.ServiceChangedSinceFilter{
		ServiceID: "svc", SinceGenerationID: "gen-prior",
	})
	if err != nil {
		t.Fatalf("ComputeServiceChangedSinceDelta() error = %v", err)
	}
	if !summary.Unattributed || summary.SinceGenerationID != "" {
		t.Fatalf("summary = %+v; want the unattributed lineage with no matching prior", summary)
	}
	if got, want := queryer.args[1][2], (sql.NullString{}); got != want {
		t.Fatalf("prior lineage arg = %#v, want SQL NULL", got)
	}
}

// TestComputeServiceChangedSinceDeltaAmbiguityIsBoundedAndSorted pins the
// ambiguity answer's bound: past MaxServiceScopeCandidates admitted scopes the
// list is cut and marked truncated, it is sorted whatever order the rows came
// in, and no diff statement runs.
func TestComputeServiceChangedSinceDeltaAmbiguityIsBoundedAndSorted(t *testing.T) {
	t.Parallel()

	rows := make([][]any, 0, changedsince.MaxServiceScopeCandidates+2)
	for i := changedsince.MaxServiceScopeCandidates + 1; i >= 0; i-- {
		rows = append(rows, []any{fmt.Sprintf("scope-%02d", i), false, "gen", nil, false})
	}
	queryer := &fakeQueryer{responses: []fakeRows{{rows: rows}}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeServiceChangedSinceDelta(context.Background(), statuspkg.ServiceChangedSinceFilter{
		ServiceID: "svc", SinceGenerationID: "gen-prior",
	})
	if err != nil {
		t.Fatalf("ComputeServiceChangedSinceDelta() error = %v", err)
	}
	if got := len(summary.AmbiguousScopeIDs); got != changedsince.MaxServiceScopeCandidates {
		t.Fatalf("ambiguous scope count = %d, want %d", got, changedsince.MaxServiceScopeCandidates)
	}
	if !summary.AmbiguousTruncated {
		t.Fatal("AmbiguousTruncated = false, want true past the bound")
	}
	if summary.AmbiguousScopeIDs[0] != "scope-00" || summary.AmbiguousScopeIDs[19] != "scope-19" {
		t.Fatalf("ambiguous scopes not sorted: %v", summary.AmbiguousScopeIDs)
	}
	if len(queryer.queries) != 1 {
		t.Fatalf("queries = %d, want 1; an ambiguous answer must not run the diff", len(queryer.queries))
	}
}

// TestComputeServiceChangedSinceDeltaOutsideGrantProbesOnlyForScopedCallers
// pins the telemetry-only existence probe: it runs for a scoped caller whose
// grant admitted nothing, and never for an unscoped one.
func TestComputeServiceChangedSinceDeltaOutsideGrantProbesOnlyForScopedCallers(t *testing.T) {
	t.Parallel()

	scoped := &fakeQueryer{responses: []fakeRows{{rows: [][]any{}}, {rows: [][]any{{true}}}}}
	summary, err := NewStatusStore(scoped).ComputeServiceChangedSinceDelta(context.Background(), statuspkg.ServiceChangedSinceFilter{
		ServiceID: "svc", SinceGenerationID: "gen-prior", Scoped: true, AllowedScopeIDs: []string{"scope-a"},
	})
	if err != nil {
		t.Fatalf("scoped: %v", err)
	}
	if summary.ServiceID != "" || !summary.OutsideGrant {
		t.Fatalf("scoped summary = %+v; want not-found with OutsideGrant", summary)
	}
	if len(scoped.queries) != 2 || scoped.queries[1] != serviceChangedSinceLineageExistsQuery {
		t.Fatalf("scoped queries = %d; want resolve then the existence probe", len(scoped.queries))
	}

	unscoped := &fakeQueryer{responses: []fakeRows{{rows: [][]any{}}}}
	summary, err = NewStatusStore(unscoped).ComputeServiceChangedSinceDelta(context.Background(), statuspkg.ServiceChangedSinceFilter{
		ServiceID: "svc", SinceGenerationID: "gen-prior",
	})
	if err != nil {
		t.Fatalf("unscoped: %v", err)
	}
	if summary.OutsideGrant || len(unscoped.queries) != 1 {
		t.Fatalf("unscoped summary = %+v, queries = %d; want no probe", summary, len(unscoped.queries))
	}
}
