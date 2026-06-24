// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// stubAppliedRoutingQueryer returns canned applied-routing rows. Each row is a
// []any matching the loader's SELECT column order: fact_id, stable_fact_key,
// provider_object_id, name_fingerprint, backend_kind, locator_hash. Nullable
// columns are *string (nil for SQL NULL) so the test exercises the loader's
// pointer scan + null handling.
type stubAppliedRoutingQueryer struct {
	rows [][]any
	err  error
}

func (s stubAppliedRoutingQueryer) QueryContext(
	context.Context, string, ...any,
) (Rows, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &stubAppliedRoutingRows{rows: s.rows}, nil
}

type stubAppliedRoutingRows struct {
	rows  [][]any
	index int
}

func (r *stubAppliedRoutingRows) Next() bool { return r.index < len(r.rows) }

func (r *stubAppliedRoutingRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			v, _ := row[i].(string)
			*target = v
		case **string:
			if row[i] == nil {
				*target = nil
				continue
			}
			v := row[i].(string)
			*target = &v
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	r.index++
	return nil
}

func (r *stubAppliedRoutingRows) Err() error   { return nil }
func (r *stubAppliedRoutingRows) Close() error { return nil }

func TestLoadAppliedPagerDutyServiceRoutingDecodesRows(t *testing.T) {
	t.Parallel()
	loader := PostgresAppliedPagerDutyServiceRoutingLoader{
		DB: stubAppliedRoutingQueryer{rows: [][]any{
			{"fact-1", "stable-1", "PDSVC1", "fp1", "s3", "loc-1"},
			// A row with a NULL provider id keeps ProviderObjectID blank so the
			// builder records it as provenance-only rejected, not silently dropped.
			{"fact-2", "stable-2", nil, "fp2", "s3", "loc-2"},
		}},
	}

	rows, err := loader.LoadAppliedPagerDutyServiceRouting(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("load: unexpected error %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].ProviderObjectID != "PDSVC1" || rows[0].BackendKind != "s3" || rows[0].LocatorHash != "loc-1" {
		t.Fatalf("row 0 decoded wrong: %+v", rows[0])
	}
	if !rows[0].ProviderIDExact {
		t.Fatalf("row 0 with a provider id must be exact")
	}
	if rows[1].ProviderObjectID != "" || rows[1].ProviderIDExact {
		t.Fatalf("row 1 with null provider id must be blank and non-exact: %+v", rows[1])
	}
	if rows[0].StableFactKey != "stable-1" {
		t.Fatalf("row 0 stable fact key = %q, want stable-1", rows[0].StableFactKey)
	}
}

func TestLoadAppliedPagerDutyServiceRoutingRejectsBlankScope(t *testing.T) {
	t.Parallel()
	loader := PostgresAppliedPagerDutyServiceRoutingLoader{DB: stubAppliedRoutingQueryer{}}
	if _, err := loader.LoadAppliedPagerDutyServiceRouting(context.Background(), "", "gen"); err == nil {
		t.Fatalf("expected blank scope rejection")
	}
}

func TestLoadAppliedPagerDutyServiceRoutingPropagatesQueryError(t *testing.T) {
	t.Parallel()
	loader := PostgresAppliedPagerDutyServiceRoutingLoader{
		DB: stubAppliedRoutingQueryer{err: errors.New("db down")},
	}
	if _, err := loader.LoadAppliedPagerDutyServiceRouting(context.Background(), "s", "g"); err == nil {
		t.Fatalf("expected query error to propagate")
	}
}

// stubBackendQuery feeds the real tfstatebackend.Resolver canned rows so the
// adapter's error translation is exercised end to end through the resolver.
type stubBackendQuery struct {
	rows []tfstatebackend.TerraformBackendRow
	err  error
}

func (s stubBackendQuery) ListTerraformBackendsByLocator(
	context.Context, string, string,
) ([]tfstatebackend.TerraformBackendRow, error) {
	return s.rows, s.err
}

func TestBackendRepositoryResolverAdapterSingleOwner(t *testing.T) {
	t.Parallel()
	adapter := BackendRepositoryResolverAdapter{
		Resolver: tfstatebackend.NewResolver(stubBackendQuery{rows: []tfstatebackend.TerraformBackendRow{
			{RepoID: "repo-a", BackendKind: "s3", LocatorHash: "loc"},
		}}),
	}
	res, err := adapter.ResolveBackendRepository(context.Background(), "s3", "loc")
	if err != nil {
		t.Fatalf("resolve: unexpected error %v", err)
	}
	if res.RepositoryID != "repo-a" || res.Ambiguous {
		t.Fatalf("single owner resolution wrong: %+v", res)
	}
}

func TestBackendRepositoryResolverAdapterAmbiguousOwner(t *testing.T) {
	t.Parallel()
	adapter := BackendRepositoryResolverAdapter{
		Resolver: tfstatebackend.NewResolver(stubBackendQuery{rows: []tfstatebackend.TerraformBackendRow{
			{RepoID: "repo-a", BackendKind: "s3", LocatorHash: "loc"},
			{RepoID: "repo-b", BackendKind: "s3", LocatorHash: "loc"},
		}}),
	}
	res, err := adapter.ResolveBackendRepository(context.Background(), "s3", "loc")
	if err != nil {
		t.Fatalf("resolve: unexpected error %v", err)
	}
	if !res.Ambiguous || res.RepositoryID != "" {
		t.Fatalf("ambiguous owner must set Ambiguous with no repo: %+v", res)
	}
}

func TestBackendRepositoryResolverAdapterNoOwner(t *testing.T) {
	t.Parallel()
	adapter := BackendRepositoryResolverAdapter{
		Resolver: tfstatebackend.NewResolver(stubBackendQuery{rows: nil}),
	}
	res, err := adapter.ResolveBackendRepository(context.Background(), "s3", "loc")
	if err != nil {
		t.Fatalf("resolve: unexpected error %v", err)
	}
	if res.Ambiguous || res.RepositoryID != "" {
		t.Fatalf("no owner must be a blank unresolved resolution: %+v", res)
	}
}

// compile-time assertions that the adapters satisfy the reducer ports.
var (
	_ reducer.AppliedPagerDutyServiceRoutingLoader = PostgresAppliedPagerDutyServiceRoutingLoader{}
	_ reducer.BackendRepositoryResolver            = BackendRepositoryResolverAdapter{}
)
