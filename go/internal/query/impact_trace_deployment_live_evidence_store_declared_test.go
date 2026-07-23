// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"
)

// TestKubernetesPodTemplateDeclaredObjectQueryUsesActiveFactReadModel proves
// the declared-object SQL variant carries the same ACTIVE-generation join
// and is_tombstone predicate as the ArgoCD tracking-id sibling, plus the
// group_version_resource/namespace/name equality predicate (#5639) and the
// same optional image-ref intersection.
func TestKubernetesPodTemplateDeclaredObjectQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'group_version_resource' = $2",
		"fact.payload->>'namespace' = $3",
		"fact.payload->>'name' = $4",
		"fact.payload->'image_refs' ?| $6",
		"LIMIT 1",
	} {
		if !strings.Contains(hasLiveKubernetesPodTemplateDeclaredObjectIdentityQuery, want) {
			t.Fatalf("hasLiveKubernetesPodTemplateDeclaredObjectIdentityQuery missing %q:\n%s", want, hasLiveKubernetesPodTemplateDeclaredObjectIdentityQuery)
		}
	}
	// The declared-object query must never anchor on the ArgoCD annotation
	// predicate -- it is a genuinely different identity signal.
	if strings.Contains(hasLiveKubernetesPodTemplateDeclaredObjectIdentityQuery, "annotations") {
		t.Fatal("hasLiveKubernetesPodTemplateDeclaredObjectIdentityQuery must not reference the annotations predicate")
	}
}

// TestKubernetesPodTemplateDeclaredObjectScopedQueryCarriesAccessPredicate
// proves the scoped variant adds the #5167 access-scoping predicate.
func TestKubernetesPodTemplateDeclaredObjectScopedQueryCarriesAccessPredicate(t *testing.T) {
	t.Parallel()

	if !strings.Contains(hasLiveKubernetesPodTemplateDeclaredObjectIdentityScopedQuery, "fact.scope_id = ANY($7) OR fact.scope_id = ANY($8)") {
		t.Fatalf("hasLiveKubernetesPodTemplateDeclaredObjectIdentityScopedQuery missing #5167 access-scoping predicate:\n%s", hasLiveKubernetesPodTemplateDeclaredObjectIdentityScopedQuery)
	}
}

// TestListLiveDeclaredObjectIdentityMatchesQueryShape mirrors
// TestListLiveIdentityMatchesQueriesUseSelectColumnsShape for the
// declared-object variant.
func TestListLiveDeclaredObjectIdentityMatchesQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'group_version_resource' = $2",
		"fact.payload->>'namespace' = $3",
		"fact.payload->>'name' = $4",
		"fact.payload->'image_refs' ?| $6",
		"fact.payload->>'cluster_id' AS cluster_id",
		"fact.payload->>'object_id' AS object_id",
		"fact.payload->>'group_version_resource' AS group_version_resource",
		"(fact.payload->>'ready_replicas')::int AS ready_replicas",
		"ORDER BY fact.payload->>'object_id'",
		"LIMIT $7",
	} {
		if !strings.Contains(listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesQuery, want) {
			t.Fatalf("listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesQuery missing %q:\n%s", want, listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesQuery)
		}
	}
	if strings.Contains(listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesQuery, "LIMIT 1") {
		t.Fatal("listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesQuery must not reuse the existence-check LIMIT 1")
	}
}

// TestListLiveDeclaredObjectIdentityMatchesScopedQueryShape mirrors the
// scoped ArgoCD sibling: the scope predicate is added and LIMIT shifts to
// make room for the two scope-id array parameters.
func TestListLiveDeclaredObjectIdentityMatchesScopedQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.scope_id = ANY($7) OR fact.scope_id = ANY($8)",
		"LIMIT $9",
	} {
		if !strings.Contains(listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesScopedQuery, want) {
			t.Fatalf("listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesScopedQuery missing %q:\n%s", want, listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesScopedQuery)
		}
	}
}

// TestKubernetesPodTemplateFilterHasScopeDeclaredObjectRequiresAllThreeFields
// proves the declared-object anchor is unbounded (rejected before any query)
// unless GroupVersionResource, Namespace, AND Name are all non-empty --
// matching the ArgoCD tracking-id sibling's "reject before query" discipline.
func TestKubernetesPodTemplateFilterHasScopeDeclaredObjectRequiresAllThreeFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		filter KubernetesPodTemplateFilter
		want   bool
	}{
		{
			name: "all three present",
			filter: KubernetesPodTemplateFilter{
				AnchorKind: liveIdentityAnchorDeclaredObject, GroupVersionResource: "apps/v1/deployments", Namespace: "ns", Name: "workload-a",
			},
			want: true,
		},
		{
			name:   "missing group_version_resource",
			filter: KubernetesPodTemplateFilter{AnchorKind: liveIdentityAnchorDeclaredObject, Namespace: "ns", Name: "workload-a"},
			want:   false,
		},
		{
			name:   "missing namespace",
			filter: KubernetesPodTemplateFilter{AnchorKind: liveIdentityAnchorDeclaredObject, GroupVersionResource: "apps/v1/deployments", Name: "workload-a"},
			want:   false,
		},
		{
			name:   "missing name",
			filter: KubernetesPodTemplateFilter{AnchorKind: liveIdentityAnchorDeclaredObject, GroupVersionResource: "apps/v1/deployments", Namespace: "ns"},
			want:   false,
		},
	}
	for _, tc := range cases {
		if got := tc.filter.hasScope(); got != tc.want {
			t.Errorf("%s: hasScope() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestKubernetesPodTemplateFilterRejectsUnboundedDeclaredObjectScope proves
// HasLiveIdentityMatch rejects an unbounded declared-object filter before
// issuing a query, mirroring TestKubernetesPodTemplateFilterRejectsUnboundedScope.
func TestKubernetesPodTemplateFilterRejectsUnboundedDeclaredObjectScope(t *testing.T) {
	t.Parallel()

	store := PostgresKubernetesPodTemplateStore{DB: failingKubernetesPodTemplateQueryer{t: t}}
	_, err := store.HasLiveIdentityMatch(context.Background(), KubernetesPodTemplateFilter{
		AnchorKind: liveIdentityAnchorDeclaredObject,
		AllScopes:  true,
	})
	if err == nil {
		t.Fatal("HasLiveIdentityMatch() error = nil, want non-nil for unbounded declared-object scope")
	}
	if want := "required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("HasLiveIdentityMatch() error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestKubernetesPodTemplateDeclaredObjectScopedEmptyGrantReturnsNoMatchWithoutQuery
// mirrors the #5167 defense-in-depth short-circuit for the declared-object
// anchor.
func TestKubernetesPodTemplateDeclaredObjectScopedEmptyGrantReturnsNoMatchWithoutQuery(t *testing.T) {
	t.Parallel()

	store := PostgresKubernetesPodTemplateStore{DB: failingKubernetesPodTemplateQueryer{t: t}}
	matched, err := store.HasLiveIdentityMatch(context.Background(), KubernetesPodTemplateFilter{
		AnchorKind:           liveIdentityAnchorDeclaredObject,
		GroupVersionResource: "apps/v1/deployments",
		Namespace:            "ns",
		Name:                 "workload-a",
		AllScopes:            false, // scoped, but no grants below
	})
	if err != nil {
		t.Fatalf("HasLiveIdentityMatch() error = %v, want nil", err)
	}
	if matched {
		t.Fatal("HasLiveIdentityMatch() = true, want false for scoped caller with no grants")
	}
}

// TestKubernetesPodTemplateHasLiveIdentityMatchDeclaredObjectScopedGrantHitsRealStore
// proves the declared-object anchor dispatches to a real *sql.DB with the
// declared-object predicate and access-scoping args bound.
func TestKubernetesPodTemplateHasLiveIdentityMatchDeclaredObjectScopedGrantHitsRealStore(t *testing.T) {
	t.Parallel()

	db, recorder := openScopeQueryerTestDB(t, []string{"?column?"}, [][]driver.Value{{int64(1)}})
	store := NewPostgresKubernetesPodTemplateStore(db)

	matched, err := store.HasLiveIdentityMatch(context.Background(), KubernetesPodTemplateFilter{
		AnchorKind:           liveIdentityAnchorDeclaredObject,
		GroupVersionResource: "apps/v1/deployments",
		Namespace:            "production",
		Name:                 "deployable-source",
		ImageRefs:            []string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		AllScopes:            false,
		AllowedRepositoryIDs: []string{"repo-tenant-a"},
		AllowedScopeIDs:      []string{"cluster-scope:tenant-a"},
	})
	if err != nil {
		t.Fatalf("HasLiveIdentityMatch() error = %v, want nil", err)
	}
	if !matched {
		t.Fatal("HasLiveIdentityMatch() = false, want true (fake driver returned one row)")
	}
	if got, want := recorder.calls(), 1; got != want {
		t.Fatalf("queryer received %d queries, want exactly %d", got, want)
	}
	if !strings.Contains(recorder.queries[0], "fact.payload->>'group_version_resource' = $2") {
		t.Fatalf("dispatched query missing declared-object predicate:\n%s", recorder.queries[0])
	}
	if !strings.Contains(recorder.queries[0], "fact.scope_id = ANY($7) OR fact.scope_id = ANY($8)") {
		t.Fatalf("dispatched query missing #5167 access-scoping predicate:\n%s", recorder.queries[0])
	}
}

// TestKubernetesPodTemplateHasLiveIdentityMatchDeclaredObjectNoMatch proves
// the negative case for the declared-object anchor.
func TestKubernetesPodTemplateHasLiveIdentityMatchDeclaredObjectNoMatch(t *testing.T) {
	t.Parallel()

	db, _ := openScopeQueryerTestDB(t, []string{"?column?"}, nil)
	store := NewPostgresKubernetesPodTemplateStore(db)

	matched, err := store.HasLiveIdentityMatch(context.Background(), KubernetesPodTemplateFilter{
		AnchorKind:           liveIdentityAnchorDeclaredObject,
		GroupVersionResource: "apps/v1/deployments",
		Namespace:            "production",
		Name:                 "deployable-source",
		AllScopes:            true,
	})
	if err != nil {
		t.Fatalf("HasLiveIdentityMatch() error = %v, want nil", err)
	}
	if matched {
		t.Fatal("HasLiveIdentityMatch() = true, want false when the driver returns no rows")
	}
}

// TestListLiveIdentityMatchesDeclaredObjectRejectsUnboundedScope mirrors the
// ArgoCD sibling for ListLiveIdentityMatches.
func TestListLiveIdentityMatchesDeclaredObjectRejectsUnboundedScope(t *testing.T) {
	t.Parallel()

	store := PostgresKubernetesPodTemplateStore{DB: failingKubernetesPodTemplateQueryer{t: t}}
	_, err := store.ListLiveIdentityMatches(context.Background(), KubernetesPodTemplateFilter{
		AnchorKind: liveIdentityAnchorDeclaredObject,
		AllScopes:  true,
	})
	if err == nil {
		t.Fatal("ListLiveIdentityMatches() error = nil, want non-nil for unbounded declared-object scope")
	}
	if want := "required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ListLiveIdentityMatches() error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestListLiveIdentityMatchesDeclaredObjectReturnsRows proves the positive
// case for the row-returning declared-object variant.
func TestListLiveIdentityMatchesDeclaredObjectReturnsRows(t *testing.T) {
	t.Parallel()

	db, recorder := openScopeQueryerTestDB(t, listLiveIdentityMatchesColumns, [][]driver.Value{
		{"supply-chain-demo", "kubernetes_live:supply-chain-demo:apps/v1/deployments:production:deployable-source", "apps/v1/deployments", int64(3)},
	})
	store := NewPostgresKubernetesPodTemplateStore(db)

	matches, err := store.ListLiveIdentityMatches(context.Background(), KubernetesPodTemplateFilter{
		AnchorKind:           liveIdentityAnchorDeclaredObject,
		GroupVersionResource: "apps/v1/deployments",
		Namespace:            "production",
		Name:                 "deployable-source",
		ImageRefs:            []string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		AllScopes:            true,
	})
	if err != nil {
		t.Fatalf("ListLiveIdentityMatches() error = %v, want nil", err)
	}
	if len(matches) != 1 {
		t.Fatalf("ListLiveIdentityMatches() = %d matches, want 1", len(matches))
	}
	if matches[0].ReadyReplicas == nil || *matches[0].ReadyReplicas != 3 {
		t.Fatalf("matches[0].ReadyReplicas = %v, want *3", matches[0].ReadyReplicas)
	}
	if got, want := recorder.calls(), 1; got != want {
		t.Fatalf("queryer received %d queries, want exactly %d", got, want)
	}
}

// declaredObjectQueryerSpy fails the test if the query text is not the
// declared-object variant -- proves HasLiveIdentityMatch/ListLiveIdentityMatches
// actually dispatch on filter.AnchorKind rather than always issuing the
// ArgoCD tracking-id query text.
type declaredObjectQueryerSpy struct {
	t       *testing.T
	columns []string
	rows    [][]driver.Value
}

func (q declaredObjectQueryerSpy) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.t.Helper()
	if strings.Contains(query, "annotations") {
		q.t.Fatalf("declared-object filter dispatched the ArgoCD annotation query instead:\n%s", query)
	}
	db, _ := openScopeQueryerTestDB(q.t, q.columns, q.rows)
	return db.QueryContext(ctx, query, args...)
}

func TestKubernetesPodTemplateHasLiveIdentityMatchDispatchesOnAnchorKind(t *testing.T) {
	t.Parallel()

	store := PostgresKubernetesPodTemplateStore{DB: declaredObjectQueryerSpy{t: t, columns: []string{"?column?"}, rows: [][]driver.Value{{int64(1)}}}}
	matched, err := store.HasLiveIdentityMatch(context.Background(), KubernetesPodTemplateFilter{
		AnchorKind:           liveIdentityAnchorDeclaredObject,
		GroupVersionResource: "apps/v1/deployments",
		Namespace:            "production",
		Name:                 "deployable-source",
		AllScopes:            true,
	})
	if err != nil {
		t.Fatalf("HasLiveIdentityMatch() error = %v, want nil", err)
	}
	if !matched {
		t.Fatal("HasLiveIdentityMatch() = false, want true")
	}
}
