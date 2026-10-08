// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// fakeRows serves (scope_id, content) pairs. A nil content models the NULL
// manifest of a dirty scope (#7609).
type fakeRows struct {
	rows [][2]any
	next int
	err  error
}

func (f *fakeRows) Next() bool { f.next++; return f.next <= len(f.rows) }
func (f *fakeRows) Scan(dest ...any) error {
	row := f.rows[f.next-1]
	*(dest[0].(*string)) = row[0].(string)
	switch d := dest[1].(type) {
	case *sql.NullString:
		if row[1] == nil {
			*d = sql.NullString{}
		} else {
			*d = sql.NullString{String: row[1].(string), Valid: true}
		}
	case *string:
		*d = row[1].(string)
	default:
		return errors.New("fakeRows supports *string and *sql.NullString destinations")
	}
	return nil
}
func (f *fakeRows) Err() error   { return f.err }
func (f *fakeRows) Close() error { return nil }

type fakeQueryer struct {
	queries []string
	rows    [][2]any
	err     error
}

func (f *fakeQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	f.queries = append(f.queries, query)
	if f.err != nil {
		return nil, f.err
	}
	return &fakeRows{rows: f.rows}, nil
}

func TestGoModuleScopeIDsMatchesModuleOrPathPrefix(t *testing.T) {
	t.Parallel()

	q := &fakeQueryer{rows: [][2]any{
		{"scope-lib", "module github.com/acme/lib\n"},
		{"scope-lib", "module github.com/acme/lib\n"}, // nested go.mod of the same repository
		{"scope-v2", "module github.com/acme/lib/v2\n"},
		{"scope-ext", "module github.com/acme/libext\n"},
		{"scope-mono", "module github.com/acme/mono\r\n"},
		{"scope-broken", "go 1.24\n"},
		{"scope-commented", "module github.com/acme/lib // old\n"},
	}}
	got, err := New(q).GoModuleScopeIDs(context.Background(), []string{
		"scip-go gomod github.com/acme/lib/client Client#Request().",
		"scip-go gomod github.com/acme/mono/svc/api Serve().",
		"scip-go gomod context Background().",
	})
	if err != nil {
		t.Fatalf("GoModuleScopeIDs() error = %v, want nil", err)
	}
	if want := []string{"scope-lib", "scope-mono"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %v, want %v (libext shares only a string prefix, /v2 extends the path, a commented module line is not a producer)", got, want)
	}
	if !reflect.DeepEqual(q.queries, []string{GoModuleManifestsQuery}) {
		t.Fatalf("queries = %v, want only the go.mod manifest read", q.queries)
	}
}

func TestPackageScopeIDsMatchesManifestName(t *testing.T) {
	t.Parallel()

	q := &fakeQueryer{rows: [][2]any{
		{"scope-a", `{"name":"@acme/logging"}`},
		{"scope-b", `{"name":"@acme/other"}`},
		{"scope-c", `{"name":"@acme/logging"}`},
		{"scope-bad", `{"name":`},
	}}
	got, err := New(q).PackageScopeIDs(context.Background(), []string{"package:@acme/logging#Logger"})
	if err != nil {
		t.Fatalf("PackageScopeIDs() error = %v, want nil", err)
	}
	if want := []string{"scope-a", "scope-c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
}

func TestPackageScopeIDsIncludesNullDirtyScopes(t *testing.T) {
	t.Parallel()

	q := &fakeQueryer{rows: [][2]any{
		{"scope-dirty", nil},
		{"scope-other", `{"name":"@acme/unrelated"}`},
	}}
	got, err := New(q).PackageScopeIDs(context.Background(), []string{"package:@acme/shared#Thing"})
	if err != nil {
		t.Fatalf("PackageScopeIDs() error = %v, want nil", err)
	}
	if want := []string{"scope-dirty"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %v, want %v (a NULL manifest marks a dirty scope, always scanned)", got, want)
	}
}

func TestScopeIDsIssueNoQueryWithoutUsableKeys(t *testing.T) {
	t.Parallel()

	q := &fakeQueryer{}
	store := New(q)
	if got, err := store.GoModuleScopeIDs(context.Background(), []string{"scip-go gomod github.com/acme/lib"}); err != nil || got != nil {
		t.Fatalf("GoModuleScopeIDs(no import path) = %v, %v, want nil, nil", got, err)
	}
	if got, err := store.PackageScopeIDs(context.Background(), []string{"package:lodash"}); err != nil || got != nil {
		t.Fatalf("PackageScopeIDs(no export) = %v, %v, want nil, nil", got, err)
	}
	if len(q.queries) != 0 {
		t.Fatalf("queries = %v, want none", q.queries)
	}
}

func TestScopeIDsWrapQueryErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	q := &fakeQueryer{err: boom}
	if _, err := New(q).GoModuleScopeIDs(context.Background(), []string{"scip-go gomod github.com/acme/lib Do()."}); !errors.Is(err, boom) {
		t.Fatalf("GoModuleScopeIDs() error = %v, want it to wrap %v", err, boom)
	}
}

// TestGoModuleScopeIDsRequiresPathBoundary pins the boundary: a module whose
// path is only a string prefix of the import path (github.com/acme/lib against
// github.com/acme/libext/x) is not a producer.
func TestGoModuleScopeIDsRequiresPathBoundary(t *testing.T) {
	t.Parallel()

	q := &fakeQueryer{rows: [][2]any{
		{"scope-lib", "module github.com/acme/lib\n"},
		{"scope-ext", "module github.com/acme/libext\n"},
	}}
	got, err := New(q).GoModuleScopeIDs(context.Background(), []string{"scip-go gomod github.com/acme/libext/x Do()."})
	if err != nil {
		t.Fatalf("GoModuleScopeIDs() error = %v, want nil", err)
	}
	if want := []string{"scope-ext"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
}
