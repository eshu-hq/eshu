// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// stagedIdentRows is a minimal db.Rows fake: each row is a list of string
// columns scanned positionally into *string destinations.
type stagedIdentRows struct {
	rows [][]string
	pos  int
}

func (r *stagedIdentRows) Next() bool {
	if r.pos >= len(r.rows) {
		return false
	}
	r.pos++
	return true
}

func (r *stagedIdentRows) Scan(dest ...any) error {
	row := r.rows[r.pos-1]
	if len(dest) != len(row) {
		return fmt.Errorf("staged ident fake: want %d dests for %d columns", len(row), len(dest))
	}
	for i, column := range row {
		target, ok := dest[i].(*string)
		if !ok {
			return fmt.Errorf("staged ident fake: dest %d is not *string", i)
		}
		*target = column
	}
	return nil
}

func (r *stagedIdentRows) Err() error   { return nil }
func (r *stagedIdentRows) Close() error { return nil }

// stagedIdentQueryer answers winner-fact point lookups by fact id.
type stagedIdentQueryer struct {
	byFactID map[string][]string
	calls    int
}

func (q *stagedIdentQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	q.calls++
	id, _ := args[0].(string)
	row, ok := q.byFactID[id]
	if !ok {
		return &stagedIdentRows{}, nil
	}
	return &stagedIdentRows{rows: [][]string{row}}, nil
}

func TestResolveCandidateIdentsFromStored(t *testing.T) {
	stored := map[string]StoredWinner{
		"k1": {
			CanonicalKey: "k1", PackageID: "pkg", RepositoryID: "repo",
			SubjectDigest: "digest", CVEID: "CVE-1", AdvisoryID: "ADV-1",
		},
	}
	reader := &stagedIdentQueryer{byFactID: map[string][]string{}}
	idents, unresolvable, err := ResolveCandidateIdents(
		context.Background(), reader, []string{"k1"},
		map[string]StagedWinner{}, map[string]StagedOverride{}, stored,
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if unresolvable != 0 {
		t.Fatalf("unresolvable = %d, want 0", unresolvable)
	}
	if len(idents) != 1 {
		t.Fatalf("idents = %d, want 1", len(idents))
	}
	got := idents[0]
	if got.PackageID.String != "pkg" || got.RepositoryID.String != "repo" ||
		got.SubjectDigest.String != "digest" || got.C0.String != "CVE-1" ||
		got.AdvisoryID.String != "ADV-1" {
		t.Fatalf("ident = %+v, want stored columns", got)
	}
	if reader.calls != 0 {
		t.Fatalf("stored keys must not point-fetch, calls = %d", reader.calls)
	}
}

func TestResolveCandidateIdentsC0Fallback(t *testing.T) {
	stored := map[string]StoredWinner{
		"ka": {CanonicalKey: "ka", AdvisoryID: "ADV-9"},
		"kb": {CanonicalKey: "kb"},
	}
	reader := &stagedIdentQueryer{byFactID: map[string][]string{}}
	idents, _, err := ResolveCandidateIdents(
		context.Background(), reader, []string{"ka", "kb"},
		map[string]StagedWinner{}, map[string]StagedOverride{}, stored,
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(idents) != 2 {
		t.Fatalf("idents = %d, want 2", len(idents))
	}
	if idents[0].C0.String != "ADV-9" {
		t.Fatalf("c0 = %q, want advisory fallback", idents[0].C0.String)
	}
	if idents[1].C0.Valid {
		t.Fatalf("c0 valid with no cve or advisory: %+v", idents[1])
	}
}

func TestResolveCandidateIdentsPointFetch(t *testing.T) {
	winners := map[string]StagedWinner{
		"new": {CanonicalKey: "new", WinnerFactID: "fact-1"},
	}
	reader := &stagedIdentQueryer{byFactID: map[string][]string{
		"fact-1": {"p-new", "r-new", "d-new", "CVE-N", "ADV-N"},
	}}
	idents, unresolvable, err := ResolveCandidateIdents(
		context.Background(), reader, []string{"new"},
		winners, map[string]StagedOverride{}, map[string]StoredWinner{},
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if unresolvable != 0 || len(idents) != 1 {
		t.Fatalf("idents = %d unresolvable = %d, want 1 and 0", len(idents), unresolvable)
	}
	if got := idents[0]; got.PackageID.String != "p-new" || got.C0.String != "CVE-N" {
		t.Fatalf("ident = %+v, want point-fetched columns", got)
	}
}

func TestResolveCandidateIdentsMissingFactSkips(t *testing.T) {
	winners := map[string]StagedWinner{
		"ghost": {CanonicalKey: "ghost", WinnerFactID: "fact-gone"},
	}
	reader := &stagedIdentQueryer{byFactID: map[string][]string{}}
	idents, unresolvable, err := ResolveCandidateIdents(
		context.Background(), reader, []string{"ghost"},
		winners, map[string]StagedOverride{}, map[string]StoredWinner{},
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if unresolvable != 1 || len(idents) != 0 {
		t.Fatalf("idents = %d unresolvable = %d, want 0 and 1", len(idents), unresolvable)
	}
}
