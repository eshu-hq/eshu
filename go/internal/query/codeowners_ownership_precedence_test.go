// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
)

// fakeCodeownersCorrelationStore is a test double for
// ServiceCatalogCorrelationStore that returns a fixed slice of rows and
// records the filter the resolver passed.
type fakeCodeownersCorrelationStore struct {
	rows   []ServiceCatalogCorrelationRow
	filter ServiceCatalogCorrelationFilter
	err    error
}

func (f *fakeCodeownersCorrelationStore) ListServiceCatalogCorrelations(
	_ context.Context,
	filter ServiceCatalogCorrelationFilter,
) ([]ServiceCatalogCorrelationRow, error) {
	f.filter = filter
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

// recordingCodeownersLastMatchGraphReader is a GraphQuery test double that
// returns a fixed RunSingle row and records the last cypher/params it saw.
type recordingCodeownersLastMatchGraphReader struct {
	row        map[string]any
	err        error
	lastCypher string
	lastParams map[string]any
	called     bool
}

func (r *recordingCodeownersLastMatchGraphReader) Run(
	context.Context,
	string,
	map[string]any,
) ([]map[string]any, error) {
	return nil, nil
}

func (r *recordingCodeownersLastMatchGraphReader) RunSingle(
	_ context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	r.called = true
	r.lastCypher = cypher
	r.lastParams = params
	if r.err != nil {
		return nil, r.err
	}
	return r.row, nil
}

func TestResolveEffectiveRepositoryOwnerPrefersManifestOwner(t *testing.T) {
	t.Parallel()

	correlations := &fakeCodeownersCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{
			{RepositoryID: "repo-1", OwnerRef: "", Outcome: "exact"},
			{RepositoryID: "repo-1", OwnerRef: "team-manifest", Outcome: "exact"},
		},
	}
	graph := &recordingCodeownersLastMatchGraphReader{row: map[string]any{"owner_ref": "team-codeowners"}}

	got, err := resolveEffectiveRepositoryOwner(context.Background(), graph, correlations, "repo-1")
	if err != nil {
		t.Fatalf("resolveEffectiveRepositoryOwner() error = %v, want nil", err)
	}
	if want := (EffectiveRepositoryOwner{OwnerRef: "team-manifest", Source: EffectiveOwnerSourceServiceCatalog}); got != want {
		t.Fatalf("resolveEffectiveRepositoryOwner() = %+v, want %+v", got, want)
	}
	if graph.called {
		t.Fatal("graph.RunSingle was called; manifest owner should short-circuit the codeowners fallback")
	}
	if got, want := correlations.filter.RepositoryID, "repo-1"; got != want {
		t.Fatalf("filter.RepositoryID = %q, want %q", got, want)
	}
}

func TestResolveEffectiveRepositoryOwnerIgnoresAmbiguousOutcomeAndFallsBackToCodeowners(t *testing.T) {
	t.Parallel()

	correlations := &fakeCodeownersCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{
			// Non-empty owner_ref but an "ambiguous" outcome must not win: only
			// exact/derived outcomes count as a resolved manifest declaration.
			{RepositoryID: "repo-1", OwnerRef: "team-ambiguous", Outcome: "ambiguous"},
		},
	}
	graph := &recordingCodeownersLastMatchGraphReader{row: map[string]any{"owner_ref": "team-codeowners"}}

	got, err := resolveEffectiveRepositoryOwner(context.Background(), graph, correlations, "repo-1")
	if err != nil {
		t.Fatalf("resolveEffectiveRepositoryOwner() error = %v, want nil", err)
	}
	if want := (EffectiveRepositoryOwner{OwnerRef: "team-codeowners", Source: EffectiveOwnerSourceCodeowners}); got != want {
		t.Fatalf("resolveEffectiveRepositoryOwner() = %+v, want %+v", got, want)
	}
	if !graph.called {
		t.Fatal("graph.RunSingle was not called; expected the codeowners last-match fallback to run")
	}
	if got, want := graph.lastParams["repo_id"], "repo-1"; got != want {
		t.Fatalf("graph params[repo_id] = %#v, want %#v", got, want)
	}
}

func TestResolveEffectiveRepositoryOwnerEmptyWhenNeitherSourceResolves(t *testing.T) {
	t.Parallel()

	correlations := &fakeCodeownersCorrelationStore{}
	graph := &recordingCodeownersLastMatchGraphReader{row: nil}

	got, err := resolveEffectiveRepositoryOwner(context.Background(), graph, correlations, "repo-1")
	if err != nil {
		t.Fatalf("resolveEffectiveRepositoryOwner() error = %v, want nil", err)
	}
	if want := (EffectiveRepositoryOwner{}); got != want {
		t.Fatalf("resolveEffectiveRepositoryOwner() = %+v, want zero value %+v", got, want)
	}
}

func TestResolveEffectiveRepositoryOwnerPropagatesCorrelationStoreError(t *testing.T) {
	t.Parallel()

	correlations := &fakeCodeownersCorrelationStore{err: errors.New("boom")}
	graph := &recordingCodeownersLastMatchGraphReader{}

	_, err := resolveEffectiveRepositoryOwner(context.Background(), graph, correlations, "repo-1")
	if err == nil {
		t.Fatal("resolveEffectiveRepositoryOwner() error = nil, want non-nil on correlation store failure")
	}
}

func TestResolveEffectiveRepositoryOwnerPropagatesGraphError(t *testing.T) {
	t.Parallel()

	correlations := &fakeCodeownersCorrelationStore{}
	graph := &recordingCodeownersLastMatchGraphReader{err: errors.New("boom")}

	_, err := resolveEffectiveRepositoryOwner(context.Background(), graph, correlations, "repo-1")
	if err == nil {
		t.Fatal("resolveEffectiveRepositoryOwner() error = nil, want non-nil on graph read failure")
	}
}
