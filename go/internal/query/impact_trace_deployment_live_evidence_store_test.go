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

func TestKubernetesPodTemplateHasLiveIdentityMatchQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->'annotations'->>$2 = $3",
		"fact.payload->'image_refs' ?| $5",
		"LIMIT 1",
	} {
		if !strings.Contains(hasLiveKubernetesPodTemplateIdentityQuery, want) {
			t.Fatalf("hasLiveKubernetesPodTemplateIdentityQuery missing %q:\n%s", want, hasLiveKubernetesPodTemplateIdentityQuery)
		}
	}
}

func TestKubernetesPodTemplateFilterRejectsNilDB(t *testing.T) {
	t.Parallel()

	store := PostgresKubernetesPodTemplateStore{DB: nil}
	_, err := store.HasLiveIdentityMatch(context.Background(), KubernetesPodTemplateFilter{
		TrackingID: "app:apps/Deployment:ns/name",
		AllScopes:  true,
	})
	if err == nil {
		t.Fatal("HasLiveIdentityMatch() error = nil, want non-nil for nil DB")
	}
	if want := "database is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("HasLiveIdentityMatch() error = %q, want it to contain %q", err.Error(), want)
	}
}

// failingKubernetesPodTemplateQueryer fails the test if any query reaches
// the database. It proves scope/anchor validation rejects an unbounded read
// before a SQL statement is ever issued.
type failingKubernetesPodTemplateQueryer struct {
	t *testing.T
}

func (q failingKubernetesPodTemplateQueryer) QueryContext(
	context.Context,
	string,
	...any,
) (*sql.Rows, error) {
	q.t.Helper()
	q.t.Fatal("QueryContext called: unbounded scope reached the database instead of being rejected")
	return nil, nil
}

func TestKubernetesPodTemplateFilterRejectsUnboundedScope(t *testing.T) {
	t.Parallel()

	store := PostgresKubernetesPodTemplateStore{DB: failingKubernetesPodTemplateQueryer{t: t}}
	_, err := store.HasLiveIdentityMatch(context.Background(), KubernetesPodTemplateFilter{AllScopes: true})
	if err == nil {
		t.Fatal("HasLiveIdentityMatch() error = nil, want non-nil for unbounded (empty tracking_id) scope")
	}
	if want := "is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("HasLiveIdentityMatch() error = %q, want it to contain %q", err.Error(), want)
	}
}

func TestKubernetesPodTemplateFilterScopedEmptyGrantReturnsNoMatchWithoutQuery(t *testing.T) {
	t.Parallel()

	store := PostgresKubernetesPodTemplateStore{DB: failingKubernetesPodTemplateQueryer{t: t}}
	matched, err := store.HasLiveIdentityMatch(context.Background(), KubernetesPodTemplateFilter{
		TrackingID: "app:apps/Deployment:ns/name",
		AllScopes:  false, // scoped, but no grants below
	})
	if err != nil {
		t.Fatalf("HasLiveIdentityMatch() error = %v, want nil", err)
	}
	if matched {
		t.Fatal("HasLiveIdentityMatch() = true, want false for scoped caller with no grants")
	}
}

// TestKubernetesPodTemplateHasLiveIdentityMatchScopedGrantHitsRealStore proves
// the #5167 access-scoping predicate against the ACTUAL production backend
// shape (PostgresKubernetesPodTemplateStore over a real *sql.DB), matching
// TestKubernetesListCorrelationsScopedGrantHitsRealStoreAndReturnsRowData's
// precedent: a scoped caller with a matching grant reaches the store and the
// dispatched SQL carries the access-scoping predicate with the caller's
// granted ids bound as args.
func TestKubernetesPodTemplateHasLiveIdentityMatchScopedGrantHitsRealStore(t *testing.T) {
	t.Parallel()

	db, recorder := openScopeQueryerTestDB(t, []string{"?column?"}, [][]driver.Value{{int64(1)}})
	store := NewPostgresKubernetesPodTemplateStore(db)

	matched, err := store.HasLiveIdentityMatch(context.Background(), KubernetesPodTemplateFilter{
		TrackingID:           "deployable-source:apps/Deployment:production/deployable-source",
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
	if !strings.Contains(recorder.queries[0], "fact.scope_id = ANY($6) OR fact.scope_id = ANY($7)") {
		t.Fatalf("dispatched query missing #5167 access-scoping predicate:\n%s", recorder.queries[0])
	}
}

// TestKubernetesPodTemplateHasLiveIdentityMatchNoMatch proves the negative
// case: the fake driver returns zero rows, so HasLiveIdentityMatch reports
// false with no error (config_only, not runtime_confirmed).
func TestKubernetesPodTemplateHasLiveIdentityMatchNoMatch(t *testing.T) {
	t.Parallel()

	db, _ := openScopeQueryerTestDB(t, []string{"?column?"}, nil)
	store := NewPostgresKubernetesPodTemplateStore(db)

	matched, err := store.HasLiveIdentityMatch(context.Background(), KubernetesPodTemplateFilter{
		TrackingID: "deployable-source:apps/Deployment:production/deployable-source",
		AllScopes:  true,
	})
	if err != nil {
		t.Fatalf("HasLiveIdentityMatch() error = %v, want nil", err)
	}
	if matched {
		t.Fatal("HasLiveIdentityMatch() = true, want false when the driver returns no rows")
	}
}

var listLiveIdentityMatchesColumns = []string{
	"cluster_id", "object_id", "group_version_resource", "ready_replicas",
}

// TestListLiveIdentityMatchesQueriesUseSelectColumnsShape proves the
// SELECT-columns query text carries the same ACTIVE-generation join,
// is_tombstone, and identity/image-refs predicate as the existence-check
// query, plus the four projected columns ListLiveIdentityMatches scans.
func TestListLiveIdentityMatchesQueriesUseSelectColumnsShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->'annotations'->>$2 = $3",
		"fact.payload->'image_refs' ?| $5",
		"fact.payload->>'cluster_id' AS cluster_id",
		"fact.payload->>'object_id' AS object_id",
		"fact.payload->>'group_version_resource' AS group_version_resource",
		"(fact.payload->>'ready_replicas')::int AS ready_replicas",
		"ORDER BY fact.payload->>'object_id'",
		"LIMIT $6",
	} {
		if !strings.Contains(listLiveKubernetesPodTemplateIdentityMatchesQuery, want) {
			t.Fatalf("listLiveKubernetesPodTemplateIdentityMatchesQuery missing %q:\n%s", want, listLiveKubernetesPodTemplateIdentityMatchesQuery)
		}
	}
	if strings.Contains(listLiveKubernetesPodTemplateIdentityMatchesQuery, "LIMIT 1") {
		t.Fatal("listLiveKubernetesPodTemplateIdentityMatchesQuery must not reuse the existence-check LIMIT 1")
	}
}

// TestListLiveIdentityMatchesScopedQueryCarriesAccessPredicate proves the
// scoped variant adds the #5167 access-scoping predicate and shifts LIMIT to
// $8 to make room for the two scope-id array parameters.
func TestListLiveIdentityMatchesScopedQueryCarriesAccessPredicate(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.scope_id = ANY($6) OR fact.scope_id = ANY($7)",
		"LIMIT $8",
	} {
		if !strings.Contains(listLiveKubernetesPodTemplateIdentityMatchesScopedQuery, want) {
			t.Fatalf("listLiveKubernetesPodTemplateIdentityMatchesScopedQuery missing %q:\n%s", want, listLiveKubernetesPodTemplateIdentityMatchesScopedQuery)
		}
	}
}

// TestListLiveIdentityMatchesRejectsNilDB mirrors
// TestKubernetesPodTemplateFilterRejectsNilDB for the list variant.
func TestListLiveIdentityMatchesRejectsNilDB(t *testing.T) {
	t.Parallel()

	store := PostgresKubernetesPodTemplateStore{DB: nil}
	_, err := store.ListLiveIdentityMatches(context.Background(), KubernetesPodTemplateFilter{
		TrackingID: "app:apps/Deployment:ns/name",
		AllScopes:  true,
	})
	if err == nil {
		t.Fatal("ListLiveIdentityMatches() error = nil, want non-nil for nil DB")
	}
	if want := "database is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ListLiveIdentityMatches() error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestListLiveIdentityMatchesRejectsUnboundedScope mirrors
// TestKubernetesPodTemplateFilterRejectsUnboundedScope for the list variant:
// an empty TrackingID must be rejected before a query is issued.
func TestListLiveIdentityMatchesRejectsUnboundedScope(t *testing.T) {
	t.Parallel()

	store := PostgresKubernetesPodTemplateStore{DB: failingKubernetesPodTemplateQueryer{t: t}}
	_, err := store.ListLiveIdentityMatches(context.Background(), KubernetesPodTemplateFilter{AllScopes: true})
	if err == nil {
		t.Fatal("ListLiveIdentityMatches() error = nil, want non-nil for unbounded (empty tracking_id) scope")
	}
	if want := "is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ListLiveIdentityMatches() error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestListLiveIdentityMatchesScopedEmptyGrantReturnsEmptyWithoutQuery proves
// the #5167 defense-in-depth short-circuit: a scoped caller with no granted
// repository or ingestion scope gets an empty, nil-error result without ever
// reaching the database.
func TestListLiveIdentityMatchesScopedEmptyGrantReturnsEmptyWithoutQuery(t *testing.T) {
	t.Parallel()

	store := PostgresKubernetesPodTemplateStore{DB: failingKubernetesPodTemplateQueryer{t: t}}
	matches, err := store.ListLiveIdentityMatches(context.Background(), KubernetesPodTemplateFilter{
		TrackingID: "app:apps/Deployment:ns/name",
		AllScopes:  false, // scoped, but no grants below
	})
	if err != nil {
		t.Fatalf("ListLiveIdentityMatches() error = %v, want nil", err)
	}
	if len(matches) != 0 {
		t.Fatalf("ListLiveIdentityMatches() = %d matches, want 0 for scoped caller with no grants", len(matches))
	}
}

// TestListLiveIdentityMatchesReturnsRows proves the positive case: the fake
// driver returns two matched facts (a Deployment with ready_replicas=3 and a
// bare Pod with no ready_replicas observation), and ListLiveIdentityMatches
// decodes both -- ReadyReplicas nil for the absent row, non-nil for the
// present one, never a fabricated zero.
func TestListLiveIdentityMatchesReturnsRows(t *testing.T) {
	t.Parallel()

	db, recorder := openScopeQueryerTestDB(t, listLiveIdentityMatchesColumns, [][]driver.Value{
		{"supply-chain-demo", "kubernetes_live:supply-chain-demo:apps/v1/deployments:default:demo", "apps/v1/deployments", int64(3)},
		{"supply-chain-demo", "kubernetes_live:supply-chain-demo:/v1/pods:default:demo-pod", "/v1/pods", nil},
	})
	store := NewPostgresKubernetesPodTemplateStore(db)

	matches, err := store.ListLiveIdentityMatches(context.Background(), KubernetesPodTemplateFilter{
		TrackingID: "deployable-source:apps/Deployment:production/deployable-source",
		ImageRefs:  []string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		AllScopes:  true,
	})
	if err != nil {
		t.Fatalf("ListLiveIdentityMatches() error = %v, want nil", err)
	}
	if len(matches) != 2 {
		t.Fatalf("ListLiveIdentityMatches() = %d matches, want 2", len(matches))
	}
	if matches[0].ReadyReplicas == nil || *matches[0].ReadyReplicas != 3 {
		t.Fatalf("matches[0].ReadyReplicas = %v, want *3", matches[0].ReadyReplicas)
	}
	if matches[0].ClusterID != "supply-chain-demo" {
		t.Fatalf("matches[0].ClusterID = %q, want supply-chain-demo", matches[0].ClusterID)
	}
	if matches[1].ReadyReplicas != nil {
		t.Fatalf("matches[1].ReadyReplicas = %v, want nil (absent, not a fabricated zero)", *matches[1].ReadyReplicas)
	}
	if got, want := recorder.calls(), 1; got != want {
		t.Fatalf("queryer received %d queries, want exactly %d", got, want)
	}
}

// TestListLiveIdentityMatchesReadyZeroIsPresentNotOmitted proves a real
// scaled-to-zero observation (ready_replicas = 0) round-trips as a non-nil
// pointer to 0, not as an absent value -- absent and present-zero are
// different truths and must never be conflated.
func TestListLiveIdentityMatchesReadyZeroIsPresentNotOmitted(t *testing.T) {
	t.Parallel()

	db, _ := openScopeQueryerTestDB(t, listLiveIdentityMatchesColumns, [][]driver.Value{
		{"supply-chain-demo", "kubernetes_live:supply-chain-demo:apps/v1/deployments:default:demo", "apps/v1/deployments", int64(0)},
	})
	store := NewPostgresKubernetesPodTemplateStore(db)

	matches, err := store.ListLiveIdentityMatches(context.Background(), KubernetesPodTemplateFilter{
		TrackingID: "deployable-source:apps/Deployment:production/deployable-source",
		ImageRefs:  []string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		AllScopes:  true,
	})
	if err != nil {
		t.Fatalf("ListLiveIdentityMatches() error = %v, want nil", err)
	}
	if len(matches) != 1 {
		t.Fatalf("ListLiveIdentityMatches() = %d matches, want 1", len(matches))
	}
	if matches[0].ReadyReplicas == nil {
		t.Fatal("matches[0].ReadyReplicas = nil, want a present pointer to 0")
	}
	if *matches[0].ReadyReplicas != 0 {
		t.Fatalf("matches[0].ReadyReplicas = %d, want 0", *matches[0].ReadyReplicas)
	}
}
