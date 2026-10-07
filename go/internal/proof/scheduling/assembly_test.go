// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeAssemblyRows struct {
	values  []assembledDiagnosticRow
	index   int
	closed  bool
	scanErr error
	iterErr error
}

func (rows *fakeAssemblyRows) Close()                                       { rows.closed = true }
func (rows *fakeAssemblyRows) Err() error                                   { return rows.iterErr }
func (rows *fakeAssemblyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *fakeAssemblyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *fakeAssemblyRows) Values() ([]any, error)                       { return nil, errors.New("not used") }
func (rows *fakeAssemblyRows) RawValues() [][]byte                          { return nil }
func (rows *fakeAssemblyRows) Conn() *pgx.Conn                              { return nil }

func (rows *fakeAssemblyRows) Next() bool {
	if rows.index >= len(rows.values) {
		rows.Close()
		return false
	}
	rows.index++
	return true
}

func (rows *fakeAssemblyRows) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	if len(dest) != 12 {
		return errors.New("wrong assembly column count")
	}
	row := rows.values[rows.index-1]
	*dest[0].(*string) = row.SourceKind
	*dest[1].(**string) = row.RepoID
	*dest[2].(**string) = row.RelativePath
	*dest[3].(**string) = row.EntityID
	*dest[4].(**string) = row.EntityName
	*dest[5].(**string) = row.EntityType
	*dest[6].(**string) = row.Language
	*dest[7].(**int32) = row.StartLine
	*dest[8].(**int32) = row.EndLine
	*dest[9].(**string) = row.MatchedTerms
	*dest[10].(*int32) = row.Score
	*dest[11].(*bool) = row.PoolTruncated
	return nil
}

func TestQueryAssembledRowsUsesProductionSQLAndPreservesTypedOrder(t *testing.T) {
	empty := ""
	privateID := "private-entity-id"
	startLine := int32(7)
	want := []assembledDiagnosticRow{
		{SourceKind: "entity", RepoID: nil, RelativePath: &empty, EntityID: &privateID, StartLine: &startLine, Score: 2, PoolTruncated: true},
		{SourceKind: "file", RepoID: &empty, RelativePath: nil, Score: 1},
	}
	result := &fakeAssemblyRows{values: want}
	payload := []byte(`[{"source_kind":"entity"}]`)
	called := false
	got, err := queryAssembledRows(context.Background(), func(_ context.Context, query string, args ...any) (pgx.Rows, error) {
		called = true
		if query != codetopicparallel.AssemblySQL(candidateCap) {
			t.Fatal("assembly query changed from the production SQL")
		}
		if len(args) != 3 || args[0] != string(payload) || args[1] != 26 || args[2] != 0 {
			t.Fatalf("assembly arguments changed: %#v", args)
		}
		return result, nil
	}, payload)
	if err != nil || !called || !result.closed || !reflect.DeepEqual(got, want) {
		t.Fatalf("direct assembly got=%+v called=%t closed=%t err=%v", got, called, result.closed, err)
	}
	page, err := summarizeDiagnosticPage(got)
	if err != nil || page.fullHash == "" || page.visibleHash == "" || page.lookaheadPresent {
		t.Fatalf("typed assembly summary=%+v err=%v", page, err)
	}
}

func TestQueryAssembledRowsClosesAndSuppressesFailures(t *testing.T) {
	privateError := errors.New("private-entity-id")
	for _, test := range []struct {
		name     string
		queryErr error
		rows     *fakeAssemblyRows
	}{
		{name: "query", queryErr: privateError},
		{name: "scan", rows: &fakeAssemblyRows{values: []assembledDiagnosticRow{{}}, scanErr: privateError}},
		{name: "iteration", rows: &fakeAssemblyRows{iterErr: privateError}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := queryAssembledRows(context.Background(), func(context.Context, string, ...any) (pgx.Rows, error) {
				return test.rows, test.queryErr
			}, []byte("[]"))
			if err == nil || strings.Contains(err.Error(), privateError.Error()) {
				t.Fatalf("failure lost or private data leaked: %v", err)
			}
			if test.rows != nil && !test.rows.closed {
				t.Fatal("failed direct assembly left rows open")
			}
		})
	}
}

func TestQueryAssembledRowsRetainsLookaheadAndRejectsOverflow(t *testing.T) {
	values := make([]assembledDiagnosticRow, 26)
	for index := range values {
		values[index] = assembledDiagnosticRow{SourceKind: "entity", Score: int32(26 - index)}
	}
	query := func(rows *fakeAssemblyRows) func(context.Context, string, ...any) (pgx.Rows, error) {
		return func(context.Context, string, ...any) (pgx.Rows, error) { return rows, nil }
	}
	result := &fakeAssemblyRows{values: values}
	got, err := queryAssembledRows(context.Background(), query(result), []byte("[]"))
	if err != nil || !result.closed || len(got) != 26 {
		t.Fatalf("26-row direct assembly got=%d closed=%t err=%v", len(got), result.closed, err)
	}
	page, err := summarizeDiagnosticPage(got)
	if err != nil || !page.lookaheadPresent || page.lookaheadHash == "" || page.visibleHash == "" {
		t.Fatalf("lookahead summary=%+v err=%v", page, err)
	}
	result = &fakeAssemblyRows{values: append(values, assembledDiagnosticRow{Score: 0})}
	if _, err := queryAssembledRows(context.Background(), query(result), []byte("[]")); err == nil || !result.closed {
		t.Fatalf("oversized direct page accepted or leaked rows: closed=%t err=%v", result.closed, err)
	}
}
