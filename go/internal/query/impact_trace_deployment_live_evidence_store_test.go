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
