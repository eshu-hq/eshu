// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/docs"
	"github.com/eshu-hq/eshu/go/internal/facts/semantic"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestBuildDocumentationFactsSQLBindsCollectedKindsLiterally pins the #7098
// slice-B statement shape: a single-kind collected read must carry its
// fact_kind as a literal, the way documentation_source already does (#7128).
// A parameterized `fact_kind = $n` keeps the planner from proving the partial
// GIN predicate of its branch (fact_records_documentation_collected_target_refs_idx
// for document, section, and link; fact_records_documentation_target_refs_idx
// for entity_mention and claim_candidate;
// fact_records_documentation_semantic_target_refs_idx for
// semantic.documentation_observation) in a generic plan, which is the plan
// database/sql clients get, so the repo-anchored read falls back to a heap
// scan with an active_join over every scope.
func TestBuildDocumentationFactsSQLBindsCollectedKindsLiterally(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{
		docs.DocumentFactKind,
		docs.SectionFactKind,
		docs.LinkFactKind,
		docs.EntityMentionFactKind,
		docs.ClaimCandidateFactKind,
		semantic.SemanticDocumentationObservationFactKind,
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			query, _ := buildDocumentationFactsSQL(documentationFactFilter{
				FactKind:   kind,
				Repository: "repository:probe",
				Limit:      10,
			})
			if want := "fact_records.fact_kind = '" + kind + "'"; !strings.Contains(query, want) {
				t.Fatalf("single-kind %s read missing literal kind predicate %q:\n%s", kind, want, query)
			}
			if strings.Contains(query, "fact_records.fact_kind = $") {
				t.Fatalf("single-kind %s read binds fact_kind as a parameter, hiding the partial index predicate:\n%s", kind, query)
			}
		})
	}
}

// stubDocRepoScopeRows is a canned db.Rows for the repository-scope resolver
// proof. Each row is a (scope_id, canonical_repo_id) pair. scanErr surfaces
// from Scan only and iterErr from Err only (after iterAfter successful scans),
// so each resolver guard is pinned by exactly one subtest. A test can yield
// partial rows before the failure the way a real cursor does.
type stubDocRepoScopeRows struct {
	rows      [][]string
	pos       int
	scanErr   error
	iterErr   error
	iterAfter int
}

func (r *stubDocRepoScopeRows) Next() bool {
	r.pos++
	return r.pos <= len(r.rows)
}

func (r *stubDocRepoScopeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if len(dest) != 2 {
		return errors.New("stubDocRepoScopeRows.Scan wants 2 destinations")
	}
	scanDocRepoScopeString(dest[0], r.rows[r.pos-1][0])
	scanDocRepoScopeString(dest[1], r.rows[r.pos-1][1])
	return nil
}

func (r *stubDocRepoScopeRows) Err() error {
	if r.iterErr != nil && r.pos > r.iterAfter {
		return r.iterErr
	}
	return nil
}

func (r *stubDocRepoScopeRows) Close() error { return nil }

func scanDocRepoScopeString(dest any, value string) {
	ptr, ok := dest.(*string)
	if !ok {
		panic("stubDocRepoScopeRows.Scan destination is not *string")
	}
	*ptr = value
}

// stubDocRepoScopeReadStore answers the resolver lookup with canned scope
// rows. It implements only the Queryer surface the resolver uses; every other
// ReadStore method fails the test when touched.
type stubDocRepoScopeReadStore struct {
	rows      [][]string
	queryErr  error
	scanErr   error
	iterErr   error
	iterAfter int
	queries   int
}

func (s *stubDocRepoScopeReadStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	s.queries++
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	return &stubDocRepoScopeRows{rows: s.rows, scanErr: s.scanErr, iterErr: s.iterErr, iterAfter: s.iterAfter}, nil
}

func (s *stubDocRepoScopeReadStore) QueryRowContext(context.Context, string, ...any) db.Row {
	panic("stubDocRepoScopeReadStore.QueryRowContext must not be used by the resolver")
}

func (s *stubDocRepoScopeReadStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	panic("stubDocRepoScopeReadStore.BeginReadOnlySnapshot must not be used by the resolver")
}

// TestResolveDocumentationRepositoryScopeBindsNameFormToScope pins the #7098
// slice-B name-form fix: a filter carrying only a repository name must resolve
// to that repository's scope before SQL is built, so the read binds through
// the scope's active generation (scalar subquery over the keyset index)
// instead of an active_join that probes every ingestion scope while matching
// zero rows.
// unresolvedDocRepoScopeResult stubs the #7098 repository-scope lookup with
// zero rows: the repository names no scope, so the read keeps the anchor-only
// SQL the test was written against. The ["scope_id"] columns declare the
// scope-id read for the fake driver's shape rule, standing down its
// incidental empty answer so this stub serves the lookup positionally.
func unresolvedDocRepoScopeResult() contentReaderQueryResult {
	return contentReaderQueryResult{
		columns:       []string{"scope_id"},
		queryContains: []string{"FROM ingestion_scopes", "LIMIT 2"},
	}
}

// TestBuildDocumentationFactsSQLKeepsScopeBoundKindsParameterized pins the
// #7098 boundary: a scope-bound (or otherwise non-anchor-only) single-kind
// read keeps the parameterized predicate the #7128 arg contract pins, where
// the keyset index needs no predicate proof. Only anchor-only reads over
// GIN-covered kinds bind literally.
func TestBuildDocumentationFactsSQLKeepsScopeBoundKindsParameterized(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{
		docs.DocumentFactKind,
		docs.SectionFactKind,
		docs.LinkFactKind,
		docs.EntityMentionFactKind,
		docs.ClaimCandidateFactKind,
		semantic.SemanticDocumentationObservationFactKind,
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			query, args := buildDocumentationFactsSQL(documentationFactFilter{
				FactKind: kind,
				ScopeID:  "scope:plan",
				Limit:    10,
			})
			if !strings.Contains(query, "fact_records.fact_kind = $1") {
				t.Fatalf("scope-bound %s read lost its parameterized kind:\n%s", kind, query)
			}
			if len(args) == 0 || args[0] != kind {
				t.Fatalf("scope-bound %s read kind is not the first argument: %#v", kind, args)
			}
		})
	}
}

func TestDocumentationLiteralKindsMatchCollectedSet(t *testing.T) {
	t.Parallel()

	// Every collected kind except documentation_source must have a SQL
	// literal, or a newly collected kind silently degrades to the slow
	// parameterized path. The collected list is the source of truth.
	var collected []string
	for _, tok := range strings.Split(documentationCollectedFactKindSQLList(), ",") {
		kind := strings.Trim(strings.TrimSpace(tok), "'")
		if kind == "" || kind == docs.SourceFactKind {
			continue
		}
		collected = append(collected, kind)
	}
	if len(collected) == 0 {
		t.Fatal("collected kind list parsed empty")
	}
	for _, kind := range collected {
		literal, ok := documentationLiteralCollectedFactKind(kind)
		if !ok {
			t.Errorf("collected kind %q has no SQL literal", kind)
		} else if literal != kind {
			t.Errorf("literal for %q = %q, want identical", kind, literal)
		}
	}
}

func TestResolveDocumentationRepositoryScopeBindsNameFormToScope(t *testing.T) {
	t.Parallel()

	store := &stubDocRepoScopeReadStore{rows: [][]string{{"scope:payments", "repository:payments"}}}
	got, resolved := resolveDocumentationRepositoryScope(
		context.Background(), store, documentationFactFilter{Repository: "payments", Limit: 10})
	if !resolved {
		t.Fatal("name-form repository did not resolve to a scope")
	}
	if got.ScopeID != "scope:payments" {
		t.Fatalf("ScopeID = %q, want %q", got.ScopeID, "scope:payments")
	}
	if got.Repository != "repository:payments" {
		t.Fatalf("Repository = %q, want canonical %q", got.Repository, "repository:payments")
	}
	if query, _ := buildDocumentationFactsSQL(got); !strings.Contains(
		query, "fact_records.generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)") {
		t.Fatalf("resolved read does not bind through the scope scalar subquery:\n%s", query)
	}
}

func TestResolveDocumentationRepositoryScopeKeepsAmbiguousAndUnknownRepos(t *testing.T) {
	t.Parallel()

	t.Run("ambiguous", func(t *testing.T) {
		t.Parallel()
		before := documentationFactFilter{Repository: "shared-name", Limit: 10}
		store := &stubDocRepoScopeReadStore{rows: [][]string{
			{"scope:a", "shared-name"},
			{"scope:b", "shared-name"},
		}}
		got, resolved := resolveDocumentationRepositoryScope(context.Background(), store, before)
		if resolved {
			t.Fatal("ambiguous repository name must not resolve")
		}
		if !reflect.DeepEqual(got, before) {
			t.Fatalf("ambiguous filter changed: %#v", got)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()
		before := documentationFactFilter{Repository: "repository:ghost", Limit: 10}
		store := &stubDocRepoScopeReadStore{}
		got, resolved := resolveDocumentationRepositoryScope(context.Background(), store, before)
		if resolved {
			t.Fatal("unknown repository must not resolve")
		}
		if !reflect.DeepEqual(got, before) {
			t.Fatalf("unknown filter changed: %#v", got)
		}
	})

	t.Run("already scoped", func(t *testing.T) {
		t.Parallel()
		before := documentationFactFilter{ScopeID: "scope:kept", Repository: "payments", Limit: 10}
		store := &stubDocRepoScopeReadStore{rows: [][]string{{"scope:payments", "repository:payments"}}}
		got, resolved := resolveDocumentationRepositoryScope(context.Background(), store, before)
		if resolved || store.queries != 0 {
			t.Fatalf("scoped filter must not touch the database: resolved=%v queries=%d", resolved, store.queries)
		}
		if !reflect.DeepEqual(got, before) {
			t.Fatalf("scoped filter changed: %#v", got)
		}
	})

	t.Run("shadowed repository anchor", func(t *testing.T) {
		t.Parallel()
		// ServiceID, TargetID, and GenerationID each shadow the repository in
		// the SQL: the repository then contributes nothing to the read, so
		// binding a scope would silently narrow result content. The filter
		// must stay byte-identical without touching the database.
		cases := map[string]documentationFactFilter{
			"service id": {Repository: "payments", ServiceID: "service:api", Limit: 10},
			"target id":  {Repository: "payments", TargetID: "target:db", Limit: 10},
			"generation": {Repository: "payments", GenerationID: "generation:7", Limit: 10},
		}
		for name, before := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				store := &stubDocRepoScopeReadStore{rows: [][]string{{"scope:payments", "repository:payments"}}}
				got, resolved := resolveDocumentationRepositoryScope(context.Background(), store, before)
				if resolved || store.queries != 0 {
					t.Fatalf("shadowed filter must not touch the database: resolved=%v queries=%d", resolved, store.queries)
				}
				if !reflect.DeepEqual(got, before) {
					t.Fatalf("shadowed filter changed: %#v", got)
				}
			})
		}
	})

	t.Run("lookup failure keeps filter", func(t *testing.T) {
		t.Parallel()
		before := documentationFactFilter{Repository: "payments", Limit: 10}
		store := &stubDocRepoScopeReadStore{queryErr: errors.New("scope probe boom")}
		got, resolved := resolveDocumentationRepositoryScope(context.Background(), store, before)
		if resolved {
			t.Fatal("failed lookup must not resolve")
		}
		if !reflect.DeepEqual(got, before) {
			t.Fatalf("failed-lookup filter changed: %#v", got)
		}
	})

	// Mid-scan failure must fail open even after a row was already yielded:
	// resolving on partial rows would silently narrow the read.
	t.Run("mid-scan rows.Err failure keeps filter", func(t *testing.T) {
		t.Parallel()
		before := documentationFactFilter{Repository: "payments", Limit: 10}
		store := &stubDocRepoScopeReadStore{
			rows:      [][]string{{"scope:payments", "repository:payments"}},
			iterErr:   errors.New("cursor lost mid-scan"),
			iterAfter: 1,
		}
		got, resolved := resolveDocumentationRepositoryScope(context.Background(), store, before)
		if resolved {
			t.Fatal("mid-scan rows.Err failure must not resolve on the partial row")
		}
		if !reflect.DeepEqual(got, before) {
			t.Fatalf("mid-scan-failure filter changed: %#v", got)
		}
	})

	t.Run("mid-scan Scan failure keeps filter", func(t *testing.T) {
		t.Parallel()
		before := documentationFactFilter{Repository: "payments", Limit: 10}
		store := &stubDocRepoScopeReadStore{
			rows:    [][]string{{"scope:payments", "repository:payments"}},
			scanErr: errors.New("row decode boom"),
		}
		got, resolved := resolveDocumentationRepositoryScope(context.Background(), store, before)
		if resolved {
			t.Fatal("mid-scan Scan failure must not resolve")
		}
		if !reflect.DeepEqual(got, before) {
			t.Fatalf("mid-scan-Scan-failure filter changed: %#v", got)
		}
	})
}
