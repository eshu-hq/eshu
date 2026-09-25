// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reset

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// readFakeRows replays (scope_id, generation_id, skip_reason) rows.
type readFakeRows struct {
	rows  [][3]string
	index int
	err   error
}

func (r *readFakeRows) Next() bool { return r.index < len(r.rows) }

func (r *readFakeRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	r.index++
	if len(dest) != 3 {
		return errors.New("scan destination count is not 3")
	}
	for i := range dest {
		*dest[i].(*string) = row[i]
	}
	return nil
}

func (r *readFakeRows) Err() error   { return r.err }
func (r *readFakeRows) Close() error { return nil }

// readFakeQueryer serves one canned generation read and records the statement.
type readFakeQueryer struct {
	rows  [][3]string
	query string
	args  []any
}

func (q *readFakeQueryer) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	q.query = query
	q.args = args
	return &readFakeRows{rows: q.rows}, nil
}

// TestReadAffectedGenerationsSplitsCoveredFromSkipped pins the classification
// contract of the single read (#7116): a row with a skip reason is reported and
// never enters the generation set the four statements bind, and a row without
// one is covered.
func TestReadAffectedGenerationsSplitsCoveredFromSkipped(t *testing.T) {
	t.Parallel()

	q := &readFakeQueryer{rows: [][3]string{
		{"scope-active", "gen-active", ""},
		{"scope-failed", "gen-failed", ""},
		{"scope-empty", "", recovery.SkipReasonNoRecoverableGeneration},
		{"scope-pending", "", recovery.SkipReasonNewestGenerationNotFailed},
		{"scope-new", "", recovery.SkipReasonNoActiveGeneration},
	}}

	generations, skipped, err := ReadAffectedGenerations(context.Background(), q, recovery.RefinalizeFilter{AllScopes: true})
	if err != nil {
		t.Fatalf("ReadAffectedGenerations() error = %v, want nil", err)
	}

	if got, want := strings.Join(generations.ScopeIDs, ","), "scope-active,scope-failed"; got != want {
		t.Fatalf("covered scope ids = %q, want %q: a skipped scope must not enter the bound set", got, want)
	}
	if got, want := strings.Join(generations.GenerationIDs, ","), "gen-active,gen-failed"; got != want {
		t.Fatalf("covered generation ids = %q, want %q", got, want)
	}
	if got, want := skipped.Total(), 3; got != want {
		t.Fatalf("skipped total = %d, want %d; ByReason = %v", got, want, skipped.ByReason)
	}
	for _, reason := range []string{
		recovery.SkipReasonNoRecoverableGeneration,
		recovery.SkipReasonNewestGenerationNotFailed,
		recovery.SkipReasonNoActiveGeneration,
	} {
		if skipped.ByReason[reason] != 1 {
			t.Fatalf("skipped.ByReason[%q] = %d, want 1", reason, skipped.ByReason[reason])
		}
	}
	if skipped.ByReason == nil || skipped.Samples == nil {
		t.Fatal("skipped maps must be non-nil so the response always carries the report")
	}
}

// TestReadAffectedGenerationsReportsUnknownNamedScopes proves a named id that
// matched no ingestion_scopes row is reported once, in sorted order, and that
// the all-scopes path never invents unknown scopes.
func TestReadAffectedGenerationsReportsUnknownNamedScopes(t *testing.T) {
	t.Parallel()

	q := &readFakeQueryer{rows: [][3]string{{"scope-known", "gen-known", ""}}}
	_, skipped, err := ReadAffectedGenerations(context.Background(), q, recovery.RefinalizeFilter{
		ScopeIDs: []string{"scope-known", "zz-missing", "aa-missing", "zz-missing"},
	})
	if err != nil {
		t.Fatalf("ReadAffectedGenerations() error = %v, want nil", err)
	}
	if got, want := skipped.ByReason[recovery.SkipReasonUnknownScope], 2; got != want {
		t.Fatalf("unknown_scope count = %d, want %d (a scope named twice counts once)", got, want)
	}
	if got, want := strings.Join(skipped.Samples[recovery.SkipReasonUnknownScope], ","), "aa-missing,zz-missing"; got != want {
		t.Fatalf("unknown_scope sample = %q, want %q", got, want)
	}

	allScopes := &readFakeQueryer{rows: nil}
	_, skipped, err = ReadAffectedGenerations(context.Background(), allScopes, recovery.RefinalizeFilter{AllScopes: true})
	if err != nil {
		t.Fatalf("ReadAffectedGenerations(AllScopes) error = %v, want nil", err)
	}
	if skipped.Total() != 0 {
		t.Fatalf("all-scopes over an empty deployment skipped %d, want 0", skipped.Total())
	}
}

// TestAffectedGenerationsQueryReportsEveryClosedSkipReason keeps the SQL and the
// Go vocabulary from drifting: each reason the report can carry must be a
// literal the read returns, or a scope classified in SQL would be reported under
// a name no consumer knows.
func TestAffectedGenerationsQueryReportsEveryClosedSkipReason(t *testing.T) {
	t.Parallel()

	query, _ := AffectedGenerationsQuery(recovery.RefinalizeFilter{AllScopes: true})
	for _, reason := range []string{
		recovery.SkipReasonNoRecoverableGeneration,
		recovery.SkipReasonNewestGenerationNotFailed,
		recovery.SkipReasonNoActiveGeneration,
	} {
		if !strings.Contains(query, "'"+reason+"'") {
			t.Fatalf("AffectedGenerationsQuery never returns skip reason %q:\n%s", reason, query)
		}
	}
}
