// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

var (
	catalogQueryBuildArgPattern = regexp.MustCompile(`'([a-z_]+)', x\."([a-z_]+)"`)
	catalogQueryColumnPattern   = regexp.MustCompile(`AS x\(([^)]*)\)`)
)

// catalogQueryKeys returns the keys a rendered catalog statement projects:
// the jsonb_build_object names, their double-quoted x."<column>" sources,
// and the double-quoted jsonb_to_record column list, in statement order.
func catalogQueryKeys(t *testing.T, query string) (names, sources, columns []string) {
	t.Helper()
	for _, match := range catalogQueryBuildArgPattern.FindAllStringSubmatch(query, -1) {
		names = append(names, match[1])
		sources = append(sources, match[2])
	}
	match := catalogQueryColumnPattern.FindStringSubmatch(query)
	if match == nil {
		t.Fatalf("no jsonb_to_record column list in:\n%s", query)
	}
	for _, column := range strings.Split(match[1], ",") {
		fields := strings.Fields(column)
		if len(fields) != 2 || fields[1] != "jsonb" ||
			len(fields[0]) < 3 || !strings.HasPrefix(fields[0], `"`) || !strings.HasSuffix(fields[0], `"`) {
			t.Fatalf(`column %q is not '"<name>" jsonb' in:`+"\n%s", column, query)
		}
		columns = append(columns, strings.Trim(fields[0], `"`))
	}
	return names, sources, columns
}

func TestListRepositoryCatalogQueryProjectsExactlyTheCatalogKeys(t *testing.T) {
	t.Parallel()

	want := relationships.RepositoryCatalogPayloadKeys()
	names, sources, columns := catalogQueryKeys(t, listRepositoryCatalogQuery)
	for label, got := range map[string][]string{
		"jsonb_build_object names": names,
		"jsonb_build_object x.col": sources,
		"jsonb_to_record columns":  columns,
	} {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", label, got, want)
		}
	}
	for _, fragment := range []string{
		"FROM fact_records AS f",
		"LATERAL jsonb_to_record(f.payload)",
		"f.fact_kind = 'repository'",
		"jsonb_typeof(f.payload) = 'object'",
		"ORDER BY f.observed_at DESC, f.fact_id DESC",
	} {
		if !strings.Contains(listRepositoryCatalogQuery, fragment) {
			t.Errorf("catalog statement lost %q:\n%s", fragment, listRepositoryCatalogQuery)
		}
	}
	if strings.Contains(listRepositoryCatalogQuery, "->") {
		t.Errorf("catalog statement uses ->, which detoasts the payload once per key:\n%s", listRepositoryCatalogQuery)
	}
}

func TestRenderRepositoryCatalogQueryFollowsItsKeyList(t *testing.T) {
	t.Parallel()

	keys := []string{"repo_id", "name", "seventh_key"}
	query := renderRepositoryCatalogQuery(keys)
	names, sources, columns := catalogQueryKeys(t, query)
	for label, got := range map[string][]string{"names": names, "sources": sources, "columns": columns} {
		if !reflect.DeepEqual(got, keys) {
			t.Errorf("%s = %v, want %v", label, got, keys)
		}
	}
}

func TestRenderRepositoryCatalogQueryQuotesReservedWordKeys(t *testing.T) {
	t.Parallel()

	query := renderRepositoryCatalogQuery([]string{"repo_id", "order", "user"})
	for _, fragment := range []string{
		`jsonb_build_object('repo_id', x."repo_id", 'order', x."order", 'user', x."user")`,
		`AS x("repo_id" jsonb, "order" jsonb, "user" jsonb)`,
	} {
		if !strings.Contains(query, fragment) {
			t.Errorf("rendered statement lacks %q:\n%s", fragment, query)
		}
	}
}

func TestRenderRepositoryCatalogQueryRejectsUnsafeKeys(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"", "repo-id", "name'; DROP TABLE fact_records; --", "Name", "1abc"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("renderRepositoryCatalogQuery accepted unsafe key %q", key)
				}
			}()
			_ = renderRepositoryCatalogQuery([]string{"repo_id", key})
		}()
	}
}

type catalogRowsFake struct {
	rows [][]any
	next int
}

func (r *catalogRowsFake) Next() bool { r.next++; return r.next <= len(r.rows) }
func (r *catalogRowsFake) Err() error { return nil }
func (r *catalogRowsFake) Close() error {
	return nil
}

func (r *catalogRowsFake) Scan(dest ...any) error {
	row := r.rows[r.next-1]
	*dest[0].(*[]byte) = row[0].([]byte)
	*dest[1].(*time.Time) = row[1].(time.Time)
	return nil
}

type catalogQueryerFake struct {
	query string
	rows  [][]any
}

func (q *catalogQueryerFake) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.query = query
	return &catalogRowsFake{rows: q.rows}, nil
}

func TestLoadRepositoryCatalogKeepsNewestRowPerRepositoryAndSkipsBlankProjection(t *testing.T) {
	t.Parallel()

	newest := time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC)
	older := newest.Add(-time.Hour)
	queryer := &catalogQueryerFake{rows: [][]any{
		{[]byte(`{"repo_id":"repo-a","graph_id":null,"name":"a-new","repo_name":null,"repo_slug":null,"remote_url":null}`), newest},
		{[]byte(`{"repo_id":null,"graph_id":null,"name":null,"repo_name":null,"repo_slug":null,"remote_url":null}`), newest},
		{[]byte(`{"repo_id":"repo-a","graph_id":null,"name":"a-old","repo_name":null,"repo_slug":null,"remote_url":null}`), older},
	}}

	catalog, observedAt, err := LoadRepositoryCatalog(context.Background(), queryer)
	if err != nil {
		t.Fatalf("LoadRepositoryCatalog() error = %v", err)
	}
	if queryer.query != listRepositoryCatalogQuery {
		t.Fatalf("loader ran %q, want listRepositoryCatalogQuery", queryer.query)
	}
	if len(catalog) != 1 || catalog[0].RepoID != "repo-a" || !reflect.DeepEqual(catalog[0].Aliases, []string{"repo-a", "a-new"}) {
		t.Fatalf("catalog = %#v, want one repo-a entry from the newest row", catalog)
	}
	if got := observedAt["repo-a"]; !got.Equal(newest) {
		t.Fatalf("observedAt[repo-a] = %v, want %v", got, newest)
	}
}
