// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphschemacompat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestRequireCompatibleAcceptsExactGraphSchema(t *testing.T) {
	t.Parallel()

	app, err := graph.SchemaApplicationForBackend(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaApplicationForBackend() error = %v, want nil", err)
	}
	db := &fakeGraphSchemaQueryer{
		rows: fakeGraphSchemaRows{
			values: [][]any{{app.Fingerprint, []byte(`[]`)}},
		},
	}

	result, err := RequireCompatible(context.Background(), db, graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("RequireCompatible() error = %v, want nil", err)
	}
	if got, want := result.ExpectedFingerprint, app.Fingerprint; got != want {
		t.Fatalf("ExpectedFingerprint = %q, want %q", got, want)
	}
	if got, want := result.AppliedFingerprint, app.Fingerprint; got != want {
		t.Fatalf("AppliedFingerprint = %q, want %q", got, want)
	}
	if got := len(result.CompatibleFingerprints); got != 0 {
		t.Fatalf("CompatibleFingerprints length = %d, want 0", got)
	}
}

func TestRequireCompatibleRejectsLatestIncompatibleGraphSchema(t *testing.T) {
	t.Parallel()

	app, err := graph.SchemaApplicationForBackend(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaApplicationForBackend() error = %v, want nil", err)
	}
	db := &fakeGraphSchemaQueryer{
		rows: fakeGraphSchemaRows{
			values: [][]any{{"different-fingerprint", []byte(`["older-compatible"]`)}},
		},
	}

	_, err = RequireCompatible(context.Background(), db, graph.SchemaBackendNornicDB)
	if err == nil {
		t.Fatal("RequireCompatible() error = nil, want incompatible schema error")
	}
	for _, want := range []string{
		"graph schema incompatible",
		app.Fingerprint[:12],
		"different-fingerprint",
		"run eshu-bootstrap-data-plane",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("RequireCompatible() error = %q, want substring %q", err, want)
		}
	}
}

func TestRequireCompatibleAcceptsLatestCompatibleGraphSchema(t *testing.T) {
	t.Parallel()

	app, err := graph.SchemaApplicationForBackend(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaApplicationForBackend() error = %v, want nil", err)
	}
	db := &fakeGraphSchemaQueryer{
		rows: fakeGraphSchemaRows{
			values: [][]any{{
				"future-additive-fingerprint",
				[]byte(fmt.Sprintf("[%q]", app.Fingerprint)),
			}},
		},
	}

	result, err := RequireCompatible(context.Background(), db, graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("RequireCompatible() error = %v, want nil", err)
	}
	if got, want := result.AppliedFingerprint, "future-additive-fingerprint"; got != want {
		t.Fatalf("AppliedFingerprint = %q, want %q", got, want)
	}
	if got := db.args[0]; got != string(graph.SchemaBackendNornicDB) {
		t.Fatalf("backend query arg = %q, want %q", got, graph.SchemaBackendNornicDB)
	}
}

func TestRequireCompatibleRejectsMissingGraphSchemaMarker(t *testing.T) {
	t.Parallel()

	_, err := RequireCompatible(
		context.Background(),
		&fakeGraphSchemaQueryer{},
		graph.SchemaBackendNornicDB,
	)
	if err == nil {
		t.Fatal("RequireCompatible() error = nil, want missing marker error")
	}
	if !errors.Is(err, ErrMissingMarker) {
		t.Fatalf("RequireCompatible() error = %v, want ErrMissingMarker", err)
	}
	if !strings.Contains(err.Error(), "graph schema marker missing") {
		t.Fatalf("RequireCompatible() error = %q, want missing marker", err)
	}
}

func TestRequireCompatibleRejectsNilRows(t *testing.T) {
	t.Parallel()

	_, err := RequireCompatible(
		context.Background(),
		nilRowsGraphSchemaQueryer{},
		graph.SchemaBackendNornicDB,
	)
	if err == nil {
		t.Fatal("RequireCompatible() error = nil, want nil rows error")
	}
	if !strings.Contains(err.Error(), "rows are required") {
		t.Fatalf("RequireCompatible() error = %q, want rows required", err)
	}
}

func TestRequireCompatibleForRuntimeSkipsLocalLightweightProfile(t *testing.T) {
	t.Parallel()

	db := &fakeGraphSchemaQueryer{
		err: errors.New("compatibility marker should not be queried"),
	}
	getenv := func(key string) string {
		if key == "ESHU_QUERY_PROFILE" {
			return "local_lightweight"
		}
		return ""
	}

	if _, err := RequireCompatibleForRuntime(context.Background(), db, getenv); err != nil {
		t.Fatalf("RequireCompatibleForRuntime() error = %v, want nil", err)
	}
	if db.query != "" {
		t.Fatalf("compatibility query = %q, want no query", db.query)
	}
}

func TestMarkAppliedRecordsCompatibleFingerprints(t *testing.T) {
	t.Parallel()

	db := &fakeGraphSchemaExecutor{}
	app := graph.SchemaApplication{
		Backend:                graph.SchemaBackendNornicDB,
		Fingerprint:            "current-fingerprint",
		StatementCount:         42,
		CompatibleFingerprints: []string{"older-fingerprint"},
	}

	if err := MarkApplied(context.Background(), db, app); err != nil {
		t.Fatalf("MarkApplied() error = %v, want nil", err)
	}
	if got := db.args[0]; got != string(graph.SchemaBackendNornicDB) {
		t.Fatalf("backend arg = %q, want %q", got, graph.SchemaBackendNornicDB)
	}
	if got := db.args[1]; got != "current-fingerprint" {
		t.Fatalf("fingerprint arg = %q, want current-fingerprint", got)
	}
	if got := db.args[2]; got != 42 {
		t.Fatalf("statement count arg = %v, want 42", got)
	}
	if got := db.args[3]; got != `["older-fingerprint"]` {
		t.Fatalf("compatible fingerprint arg = %q, want JSON list", got)
	}
}

func TestMarkAppliedNormalizesNilCompatibleFingerprints(t *testing.T) {
	t.Parallel()

	db := &fakeGraphSchemaExecutor{}
	app := graph.SchemaApplication{
		Backend:        graph.SchemaBackendNornicDB,
		Fingerprint:    "current-fingerprint",
		StatementCount: 42,
	}

	if err := MarkApplied(context.Background(), db, app); err != nil {
		t.Fatalf("MarkApplied() error = %v, want nil", err)
	}
	if got := db.args[3]; got != `[]` {
		t.Fatalf("compatible fingerprint arg = %q, want empty JSON list", got)
	}
}

type fakeGraphSchemaQueryer struct {
	query string
	args  []any
	rows  fakeGraphSchemaRows
	err   error
}

func (f *fakeGraphSchemaQueryer) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (postgres.Rows, error) {
	f.query = query
	f.args = args
	if f.err != nil {
		return nil, f.err
	}
	return &f.rows, nil
}

type nilRowsGraphSchemaQueryer struct{}

func (nilRowsGraphSchemaQueryer) QueryContext(
	context.Context,
	string,
	...any,
) (postgres.Rows, error) {
	return nil, nil
}

type fakeGraphSchemaRows struct {
	values [][]any
	index  int
	err    error
}

func (r *fakeGraphSchemaRows) Next() bool {
	return r.index < len(r.values)
}

func (r *fakeGraphSchemaRows) Scan(dest ...any) error {
	if r.index >= len(r.values) {
		return errors.New("scan called without row")
	}
	row := r.values[r.index]
	if len(row) != len(dest) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want string", i, row[i])
			}
			*target = value
		case *[]byte:
			value, ok := row[i].([]byte)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []byte", i, row[i])
			}
			*target = value
		default:
			return fmt.Errorf("scan dest[%d] type = %T", i, dest[i])
		}
	}
	r.index++
	return nil
}

func (r *fakeGraphSchemaRows) Err() error {
	return r.err
}

func (r *fakeGraphSchemaRows) Close() error {
	return nil
}

type fakeGraphSchemaExecutor struct {
	query string
	args  []any
	err   error
}

func (f *fakeGraphSchemaExecutor) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.query = query
	f.args = args
	if f.err != nil {
		return nil, f.err
	}
	return fakeGraphSchemaResult(0), nil
}

type fakeGraphSchemaResult int64

func (r fakeGraphSchemaResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r fakeGraphSchemaResult) RowsAffected() (int64, error) {
	return int64(r), nil
}
