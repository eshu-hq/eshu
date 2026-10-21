// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// storedFakeDB is a scripted db.Queryer for the stored-reader tests. It
// serves one fixed row set for the stored read, records every statement, and
// fails with queryErr when set.
type storedFakeDB struct {
	values   [][]any
	calls    []string
	queryErr error
}

func (f *storedFakeDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	f.calls = append(f.calls, query)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return &fakeRows{values: f.values}, nil
}

// storedRowValues builds the wire values of one stored-read result row: the
// state columns of repoID, then one paths row, then list_truncated, in
// storedReadSQL column order. A nil path makes the paths columns NULL (a
// derived repository with no config artifacts).
func storedRowValues(repoID string, contentVersion *int64, extractorVersion int, rowCount *int64, version int64, path []any, truncated bool) []any {
	row := []any{repoID, nil, int64(extractorVersion), nil, version, nil, nil, nil, nil, truncated}
	if contentVersion != nil {
		row[1] = *contentVersion
	}
	if rowCount != nil {
		row[3] = *rowCount
	}
	if path != nil {
		row[5], row[6], row[7], row[8] = path[0], path[1], path[2], path[3]
	}
	return row
}

func TestReadStoredReturnsTrustedRowsInRequestOrder(t *testing.T) {
	t.Parallel()

	v := int64(7)
	n := int64(2)
	fake := &storedFakeDB{values: [][]any{
		storedRowValues("r-b", &v, 1, &n, v, []any{"mod/app.hcl", "app.hcl", "terraform", true}, false),
		storedRowValues("r-b", &v, 1, &n, v, []any{"chart/values.yaml", "values.yaml", "helm", false}, false),
		storedRowValues("r-a", &v, 1, &n, v, nil, false),
	}}
	got, err := NewPostgresReader(fake).ReadStored(context.Background(), []string{"r-b", "r-a"}, 1)
	if err != nil {
		t.Fatalf("ReadStored() error = %v", err)
	}
	if len(got) != 2 || got[0].RepoID != "r-b" || got[1].RepoID != "r-a" {
		t.Fatalf("ReadStored() order = %v, want [r-b r-a]", got)
	}
	if got[0].Reason != ReasonTrusted || len(got[0].Rows) != 2 {
		t.Fatalf("r-b = reason %q rows %d, want trusted with 2 rows", got[0].Reason, len(got[0].Rows))
	}
	first := got[0].Rows[0]
	if first.RawPath != "mod/app.hcl" || first.RelativePath != "app.hcl" || first.EvidenceKind != "terraform" || !first.NameScoped {
		t.Fatalf("r-b first row = %+v, want the stored terraform row", first)
	}
	// A derived repository with no config artifacts is trusted with no rows,
	// not missing.
	if got[1].Reason != ReasonTrusted || len(got[1].Rows) != 0 {
		t.Fatalf("r-a = reason %q rows %d, want trusted with 0 rows", got[1].Reason, len(got[1].Rows))
	}
	if len(fake.calls) != 1 {
		t.Fatalf("ReadStored() issued %d statements, want exactly 1", len(fake.calls))
	}
}

// TestReadStoredAgreesWithClassify holds the PR3c SQL reader to Classify's
// trust rule (scoped AGENTS.md): for every state the reader must return the
// reason Classify returns for the same inputs, never a copy of the rule.
func TestReadStoredAgreesWithClassify(t *testing.T) {
	t.Parallel()

	current := int64(7)
	older := int64(6)
	count := int64(3)
	cases := []struct {
		name           string
		contentVersion *int64
		stateExtractor int
		rowCount       *int64
		version        *int64
		readExtractor  int
		omitStateRow   bool
	}{
		{name: "trusted", contentVersion: &current, stateExtractor: 1, rowCount: &count, version: &current, readExtractor: 1},
		{name: "missing state", version: &current, readExtractor: 1, omitStateRow: true},
		{name: "failed row count", contentVersion: &current, stateExtractor: 1, version: &current, readExtractor: 1},
		{name: "failed content version", stateExtractor: 1, rowCount: &count, version: &current, readExtractor: 1},
		{name: "stale version", contentVersion: &older, stateExtractor: 1, rowCount: &count, version: &current, readExtractor: 1},
		{name: "stale extractor", contentVersion: &current, stateExtractor: 1, rowCount: &count, version: &current, readExtractor: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var state *State
			if !tc.omitStateRow {
				state = &State{ContentVersion: tc.contentVersion, ExtractorVersion: tc.stateExtractor}
				if tc.rowCount != nil {
					count := int(*tc.rowCount)
					state.RowCount = &count
				}
			}
			want := Classify(state, tc.version, tc.readExtractor)

			var values [][]any
			if !tc.omitStateRow {
				version := int64(0)
				if tc.version != nil {
					version = *tc.version
				}
				values = [][]any{storedRowValues("r-1", tc.contentVersion, tc.stateExtractor, tc.rowCount, version, nil, false)}
			}
			fake := &storedFakeDB{values: values}
			got, err := NewPostgresReader(fake).ReadStored(context.Background(), []string{"r-1"}, tc.readExtractor)
			if err != nil {
				t.Fatalf("ReadStored() error = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("ReadStored() returned %d repositories, want 1", len(got))
			}
			if got[0].Reason != want {
				t.Fatalf("ReadStored() reason = %q, Classify() = %q", got[0].Reason, want)
			}
			if want == ReasonTrusted && len(got[0].Rows) != 0 {
				t.Fatalf("trusted repository has %d rows, want 0", len(got[0].Rows))
			}
			if want != ReasonTrusted && len(got[0].Rows) != 0 {
				t.Fatalf("untrusted repository (%q) carries %d rows, want none", want, len(got[0].Rows))
			}
		})
	}
}

func TestReadStoredDedupesIDsAndKeepsOrder(t *testing.T) {
	t.Parallel()

	v := int64(7)
	n := int64(1)
	fake := &storedFakeDB{values: [][]any{
		storedRowValues("r-a", &v, 1, &n, v, nil, false),
		storedRowValues("r-b", &v, 1, &n, v, nil, false),
	}}
	got, err := NewPostgresReader(fake).ReadStored(context.Background(), []string{"r-b", "r-a", "r-b"}, 1)
	if err != nil {
		t.Fatalf("ReadStored() error = %v", err)
	}
	ids := []string{got[0].RepoID, got[1].RepoID}
	if len(got) != 2 || !slices.Equal(ids, []string{"r-b", "r-a"}) {
		t.Fatalf("ReadStored() ids = %v, want exactly [r-b r-a]", ids)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("ReadStored() issued %d statements, want exactly 1", len(fake.calls))
	}
}

func TestReadStoredEmptyIDsSendsNoStatement(t *testing.T) {
	t.Parallel()

	fake := &storedFakeDB{}
	got, err := NewPostgresReader(fake).ReadStored(context.Background(), nil, 1)
	if err != nil {
		t.Fatalf("ReadStored(nil) error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ReadStored(nil) = %v, want empty", got)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("ReadStored(nil) issued %d statements, want none", len(fake.calls))
	}
}

func TestReadStoredPropagatesQueryError(t *testing.T) {
	t.Parallel()

	want := errors.New("connection refused")
	fake := &storedFakeDB{queryErr: want}
	if _, err := NewPostgresReader(fake).ReadStored(context.Background(), []string{"r-1"}, 1); !errors.Is(err, want) {
		t.Fatalf("ReadStored() error = %v, want %v", err, want)
	}
}

// TestReadStoredSurfacesListTruncatedOnTrustedOnly pins the stored window
// reason input (#8155 PR3c): a trusted repository reports the state row's
// list_truncated, so the leaf derives the 5,000-window reason from it. An
// untrusted repository reports false even when its state row is truncated,
// because its live fallback load owns the window reason.
func TestReadStoredSurfacesListTruncatedOnTrustedOnly(t *testing.T) {
	t.Parallel()

	v := int64(7)
	older := int64(6)
	fake := &storedFakeDB{values: [][]any{
		storedRowValues("r-truncated", &v, 1, &v, v, nil, true),
		storedRowValues("r-whole", &v, 1, &v, v, nil, false),
		storedRowValues("r-stale", &older, 1, &v, v, nil, true),
	}}
	got, err := NewPostgresReader(fake).ReadStored(context.Background(), []string{"r-truncated", "r-whole", "r-stale"}, 1)
	if err != nil {
		t.Fatalf("ReadStored() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ReadStored() returned %d repositories, want 3", len(got))
	}
	if !got[0].ListTruncated {
		t.Fatal("trusted truncated repository reports ListTruncated = false, want true")
	}
	if got[1].ListTruncated {
		t.Fatal("trusted whole-window repository reports ListTruncated = true, want false")
	}
	if got[2].Reason != ReasonStaleVersion {
		t.Fatalf("r-stale reason = %q, want stale_version", got[2].Reason)
	}
	if got[2].ListTruncated {
		t.Fatal("untrusted repository reports ListTruncated = true, want false: the live fallback owns the window reason")
	}
}

func TestReadStoredRejectsNilQueryer(t *testing.T) {
	t.Parallel()

	if _, err := NewPostgresReader(nil).ReadStored(context.Background(), []string{"r-1"}, 1); err == nil ||
		!strings.Contains(err.Error(), "queryer") {
		t.Fatalf("ReadStored() with nil queryer error = %v, want a queryer error", err)
	}
}
