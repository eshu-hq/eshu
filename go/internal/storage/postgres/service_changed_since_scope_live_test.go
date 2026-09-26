// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
)

// TestServiceChangedSinceBindsGrantToLineageScopeLive drives the production
// ComputeServiceChangedSinceDelta against the bootstrapped schema (#6475 part
// B). One catalog service id is held by two ingestion scopes plus an
// unattributed legacy row, the shape two tenants that both declare
// `component:default/api` produce, and a second id is held only by an
// unattributed legacy lineage. It proves the reader binds the caller's grant
// on the lineage row's scope_id in SQL:
//
//   - a scoped caller resolves only its own scope's lineage;
//   - another scope's prior generation id is answered exactly like an unknown
//     one;
//   - an unattributed lineage is invisible to every scoped caller and visible
//     to an unscoped one;
//   - a grant spanning both scopes, with no selector, gets the admitted scope
//     ids and no diff;
//   - an explicit selector outside the grant resolves nothing;
//   - a repository grant binds through ingestion_scopes.source_key.
//
// Locally:
//
//	ESHU_POSTGRES_DSN=postgresql://user:pass@localhost:<port>/eshu \
//	go test ./internal/storage/postgres \
//	  -run TestServiceChangedSinceBindsGrantToLineageScopeLive -count=1 -v
func TestServiceChangedSinceBindsGrantToLineageScopeLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	db, _ := openServiceLineageSchemaLive(ctx, t, "eshu_6475_lineage_reader")
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
VALUES
  ('scope-a', 'repository', 'git', 'repo-a', 'git', 'p', now(), now(), 'active'),
  ('scope-b', 'repository', 'git', 'repo-b', 'git', 'p', now(), now(), 'active');
INSERT INTO service_materialization_generations
  (generation_id, service_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, superseded_at)
VALUES
  ('gen-a-prior',   'component:default/api', 'scope-a', 'service_catalog_correlation', now() - interval '2 hours', now(), 'superseded', now() - interval '2 hours', now() - interval '1 hour'),
  ('gen-a-current', 'component:default/api', 'scope-a', 'service_catalog_correlation', now() - interval '1 hour',  now(), 'active',     now() - interval '1 hour', NULL),
  ('gen-b-prior',   'component:default/api', 'scope-b', 'service_catalog_correlation', now() - interval '2 hours', now(), 'superseded', now() - interval '2 hours', now() - interval '1 hour'),
  ('gen-b-current', 'component:default/api', 'scope-b', 'service_catalog_correlation', now() - interval '1 hour',  now(), 'active',     now() - interval '1 hour', NULL),
  ('gen-api-legacy', 'component:default/api', NULL,     'service_catalog_correlation', now() - interval '30 days', now(), 'active',     now() - interval '30 days', NULL),
  ('gen-legacy-prior',   'component:default/legacy', NULL, 'service_catalog_correlation', now() - interval '31 days', now(), 'superseded', now() - interval '31 days', now() - interval '30 days'),
  ('gen-legacy-current', 'component:default/legacy', NULL, 'service_catalog_correlation', now() - interval '30 days', now(), 'active',     now() - interval '30 days', NULL),
  ('gen-stale-a-old',    'component:default/stale-scope', 'scope-a', 'service_catalog_correlation', now() - interval '40 days', now(), 'superseded', now() - interval '40 days', now() - interval '35 days'),
  ('gen-stale-legacy-prior', 'component:default/stale-scope', NULL,  'service_catalog_correlation', now() - interval '31 days', now(), 'superseded', now() - interval '31 days', now() - interval '30 days'),
  ('gen-stale-legacy',   'component:default/stale-scope', NULL,      'service_catalog_correlation', now() - interval '30 days', now(), 'active',     now() - interval '30 days', NULL)`); err != nil {
		t.Fatalf("seed lineage: %v", err)
	}

	store := NewStatusStore(SQLDB{DB: db})
	compute := func(t *testing.T, filter changedsince.ServiceFilter) changedsince.ServiceSummary {
		t.Helper()
		summary, err := store.ComputeServiceChangedSinceDelta(ctx, filter)
		if err != nil {
			t.Fatalf("ComputeServiceChangedSinceDelta(%+v): %v", filter, err)
		}
		return summary
	}
	tenantA := func(serviceID, since string) changedsince.ServiceFilter {
		return changedsince.ServiceFilter{
			ServiceID: serviceID, SinceGenerationID: since,
			Scoped: true, AllowedScopeIDs: []string{"scope-a"},
		}
	}

	t.Run("tenant A resolves only its own lineage", func(t *testing.T) {
		got := compute(t, tenantA("component:default/api", "gen-a-prior"))
		if got.ScopeID != "scope-a" || got.CurrentActiveGenerationID != "gen-a-current" ||
			got.SinceGenerationID != "gen-a-prior" || got.Ambiguous() || got.Unavailable {
			t.Fatalf("summary = %+v; want scope-a lineage gen-a-prior -> gen-a-current", got)
		}
	})

	t.Run("another scope's prior id is answered like an unknown id", func(t *testing.T) {
		foreign := compute(t, tenantA("component:default/api", "gen-b-prior"))
		unknown := compute(t, tenantA("component:default/api", "gen-does-not-exist"))
		if foreign.SinceGenerationID != "" || foreign.ServiceID == "" || foreign.Unavailable {
			t.Fatalf("foreign prior summary = %+v; want resolved service with no since generation", foreign)
		}
		if !reflect.DeepEqual(foreign, unknown) {
			t.Fatalf("foreign prior is distinguishable from unknown prior:\n foreign: %+v\n unknown: %+v", foreign, unknown)
		}
	})

	t.Run("unattributed lineage is invisible to a scoped caller", func(t *testing.T) {
		for _, filter := range []changedsince.ServiceFilter{
			tenantA("component:default/legacy", "gen-legacy-prior"),
			{
				ServiceID: "component:default/legacy", SinceGenerationID: "gen-legacy-prior",
				Scoped: true, AllowedScopeIDs: []string{"scope-a", "scope-b"}, AllowedRepositoryIDs: []string{"repo-a", "repo-b"},
			},
		} {
			got := compute(t, filter)
			if got.ServiceID != "" || got.CurrentActiveGenerationID != "" || got.Ambiguous() {
				t.Fatalf("summary = %+v; want not-found for a scoped caller", got)
			}
			if !got.OutsideGrant {
				t.Fatalf("OutsideGrant = false; the service holds lineage, so the refusal must be visible to telemetry")
			}
		}
	})

	t.Run("unattributed lineage is visible to an unscoped caller", func(t *testing.T) {
		got := compute(t, changedsince.ServiceFilter{
			ServiceID: "component:default/legacy", SinceGenerationID: "gen-legacy-prior",
		})
		if !got.Unattributed || got.ScopeID != "" || got.CurrentActiveGenerationID != "gen-legacy-current" ||
			got.SinceGenerationID != "gen-legacy-prior" {
			t.Fatalf("summary = %+v; want the unattributed legacy lineage", got)
		}
	})

	t.Run("an attributed lineage with no active generation does not shadow the legacy one", func(t *testing.T) {
		// The ruling is "unattributed is served only when no attributed ACTIVE
		// lineage exists". scope-a's chain for this id holds only a superseded
		// generation, so the unscoped caller is served the legacy active one.
		got := compute(t, changedsince.ServiceFilter{
			ServiceID: "component:default/stale-scope", SinceGenerationID: "gen-stale-legacy-prior",
		})
		if !got.Unattributed || got.CurrentActiveGenerationID != "gen-stale-legacy" ||
			got.SinceGenerationID != "gen-stale-legacy-prior" || got.Unavailable {
			t.Fatalf("summary = %+v; want the legacy active lineage, not scope-a's inactive one", got)
		}
	})

	t.Run("grant spanning both scopes gets the admitted scope ids", func(t *testing.T) {
		got := compute(t, changedsince.ServiceFilter{
			ServiceID: "component:default/api", SinceGenerationID: "gen-a-prior",
			Scoped: true, AllowedScopeIDs: []string{"scope-b", "scope-a"},
		})
		if want := []string{"scope-a", "scope-b"}; !reflect.DeepEqual(got.AmbiguousScopeIDs, want) {
			t.Fatalf("AmbiguousScopeIDs = %v, want %v; summary = %+v", got.AmbiguousScopeIDs, want, got)
		}
		if got.CurrentActiveGenerationID != "" || got.SinceGenerationID != "" || len(got.Categories) != 0 {
			t.Fatalf("an ambiguous answer computed a diff: %+v", got)
		}
	})

	t.Run("unscoped caller gets both attributed scopes and never the legacy row", func(t *testing.T) {
		got := compute(t, changedsince.ServiceFilter{
			ServiceID: "component:default/api", SinceGenerationID: "gen-a-prior",
		})
		if want := []string{"scope-a", "scope-b"}; !reflect.DeepEqual(got.AmbiguousScopeIDs, want) {
			t.Fatalf("AmbiguousScopeIDs = %v, want %v; summary = %+v", got.AmbiguousScopeIDs, want, got)
		}
	})

	t.Run("explicit selector inside the grant serves that lineage", func(t *testing.T) {
		got := compute(t, changedsince.ServiceFilter{
			ServiceID: "component:default/api", ScopeID: "scope-b", SinceGenerationID: "gen-b-prior",
			Scoped: true, AllowedScopeIDs: []string{"scope-a", "scope-b"},
		})
		if got.ScopeID != "scope-b" || got.CurrentActiveGenerationID != "gen-b-current" || got.SinceGenerationID != "gen-b-prior" {
			t.Fatalf("summary = %+v; want scope-b lineage", got)
		}
	})

	t.Run("explicit selector outside the grant resolves nothing", func(t *testing.T) {
		filter := tenantA("component:default/api", "gen-b-prior")
		filter.ScopeID = "scope-b"
		got := compute(t, filter)
		if got.ServiceID != "" || got.CurrentActiveGenerationID != "" {
			t.Fatalf("summary = %+v; want not-found for an ungranted selector", got)
		}
	})

	t.Run("repository grant binds through source_key", func(t *testing.T) {
		got := compute(t, changedsince.ServiceFilter{
			ServiceID: "component:default/api", SinceGenerationID: "gen-b-prior",
			Scoped: true, AllowedRepositoryIDs: []string{"repo-b"},
		})
		if got.ScopeID != "scope-b" || got.CurrentActiveGenerationID != "gen-b-current" || got.SinceGenerationID != "gen-b-prior" {
			t.Fatalf("summary = %+v; want scope-b lineage through the repository grant", got)
		}
	})

	t.Run("empty scoped grant resolves nothing", func(t *testing.T) {
		got := compute(t, changedsince.ServiceFilter{
			ServiceID: "component:default/api", SinceGenerationID: "gen-a-prior", Scoped: true,
		})
		if got.ServiceID != "" {
			t.Fatalf("summary = %+v; want not-found for an empty grant", got)
		}
	})

	t.Run("unscoped selector cannot diff against another lineage's prior", func(t *testing.T) {
		got := compute(t, changedsince.ServiceFilter{
			ServiceID: "component:default/api", ScopeID: "scope-a", SinceGenerationID: "gen-api-legacy",
		})
		if got.ScopeID != "scope-a" || got.SinceGenerationID != "" {
			t.Fatalf("summary = %+v; want the prior lookup bound to scope-a's lineage", got)
		}
	})
}
