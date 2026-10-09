// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakeRows serves (scope_id, content, tag_outcome) triples. A nil content
// models the NULL manifest of a dirty scope (#7609); the outcome names the
// tag rule that produced it (#7760).
type fakeRows struct {
	rows [][3]any
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
	*(dest[2].(*string)) = row[2].(string)
	return nil
}
func (f *fakeRows) Err() error   { return f.err }
func (f *fakeRows) Close() error { return nil }

type fakeQueryer struct {
	queries []string
	rows    [][3]any
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

	q := &fakeQueryer{rows: [][3]any{
		{"scope-lib", "module github.com/acme/lib\n", "clean"},
		{"scope-lib", "module github.com/acme/lib\n", "clean"}, // nested go.mod of the same repository
		{"scope-v2", "module github.com/acme/lib/v2\n", "clean"},
		{"scope-ext", "module github.com/acme/libext\n", "clean"},
		{"scope-mono", "module github.com/acme/mono\r\n", "clean"},
		{"scope-broken", "go 1.24\n", "clean"},
		{"scope-commented", "module github.com/acme/lib // old\n", "clean"},
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

	q := &fakeQueryer{rows: [][3]any{
		{"scope-a", `{"name":"@acme/logging"}`, "clean"},
		{"scope-b", `{"name":"@acme/other"}`, "clean"},
		{"scope-c", `{"name":"@acme/logging"}`, "clean"},
		{"scope-bad", `{"name":`, "clean"},
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

	q := &fakeQueryer{rows: [][3]any{
		{"scope-dirty", nil, "dangling_tag"},
		{"scope-other", `{"name":"@acme/unrelated"}`, "clean"},
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

	q := &fakeQueryer{rows: [][3]any{
		{"scope-lib", "module github.com/acme/lib\n", "clean"},
		{"scope-ext", "module github.com/acme/libext\n", "clean"},
	}}
	got, err := New(q).GoModuleScopeIDs(context.Background(), []string{"scip-go gomod github.com/acme/libext/x Do()."})
	if err != nil {
		t.Fatalf("GoModuleScopeIDs() error = %v, want nil", err)
	}
	if want := []string{"scope-ext"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
}

// TestScopeIDsRecordTagOutcomes proves each manifest read counts every row it
// saw under kind and the tag outcome the SQL rule assigned, so an operator
// can watch dirty-tagged manifests without reading the database (#7760). It
// covers both reads, pinning that "go module" normalizes to gomod.
func TestScopeIDsRecordTagOutcomes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	q := &fakeQueryer{rows: [][3]any{
		{"scope-clean", `{"name":"@acme/shared"}`, "clean"},
		{"scope-dirty", nil, "dangling_tag"},
		{"scope-less", nil, "manifest_less"},
	}}
	if _, err := New(q).WithInstruments(instruments).PackageScopeIDs(context.Background(), []string{"package:@acme/shared#Thing"}); err != nil {
		t.Fatalf("PackageScopeIDs() error = %v, want nil", err)
	}
	// The go.mod read passes "go module" and must still land on gomod.
	g := &fakeQueryer{rows: [][3]any{
		{"scope-lib", "module github.com/acme/lib\n", "clean"},
		{"scope-dirty", nil, "unactivated_tag"},
	}}
	if _, err := New(g).WithInstruments(instruments).GoModuleScopeIDs(context.Background(), []string{"scip-go gomod github.com/acme/lib/client Client#Request()."}); err != nil {
		t.Fatalf("GoModuleScopeIDs() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_producer_manifest_tag_outcomes_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", m.Name, m.Data)
			}
			for _, point := range sum.DataPoints {
				kind, _ := point.Attributes.Value(telemetry.MetricDimensionKind)
				outcome, _ := point.Attributes.Value(telemetry.MetricDimensionOutcome)
				got[kind.AsString()+"/"+outcome.AsString()] += point.Value
			}
		}
	}
	want := map[string]int64{"package/clean": 1, "package/dangling_tag": 1, "package/manifest_less": 1, "gomod/clean": 1, "gomod/unactivated_tag": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tag outcomes = %v, want %v", got, want)
	}
}
