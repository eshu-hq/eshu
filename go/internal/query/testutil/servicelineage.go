// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testutil

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// ServiceLineageFixtureRow is one service lineage (one generation chain of
// service_materialization_generations for one service id) reduced to what the
// service changed-since resolve and prior statements read (#6475): the service
// id, the lineage's scope_id with the two ingestion_scopes columns its
// repository-grant arm joins through, and the chain's prior and current
// generation ids. An empty ScopeID is the unattributed legacy lineage
// (scope_id IS NULL).
type ServiceLineageFixtureRow struct {
	ServiceID           string
	ScopeID             string
	ScopeKind           string
	SourceKey           string
	PriorGenerationID   string
	CurrentGenerationID string
}

// Service ids of the #6475 two-tenant service lineage fixture.
const (
	// ServiceLineageSharedID is declared by both tenants' catalogs, so it holds
	// one lineage in scope-a and one in scope-b, plus an unattributed legacy
	// lineage the writer never supersedes.
	ServiceLineageSharedID = "component:default/api"
	// ServiceLineageLegacyID holds only an unattributed legacy lineage.
	ServiceLineageLegacyID = "component:default/legacy"
	// ServiceLineageTenantAOnlyID holds only tenant A's lineage.
	ServiceLineageTenantAOnlyID = "component:default/tenant-a-only"
	// ServiceLineageMigratedID holds one attributed lineage (scope-a) beside an
	// unattributed legacy one the writer never superseded: the shape a service
	// takes after its first re-materialization under #6475.
	ServiceLineageMigratedID = "component:default/migrated"
)

// TwoTenantServiceLineageRows returns the #6475 fixture: two repository-kind
// scopes that both hold a lineage for ServiceLineageSharedID, an unattributed
// legacy lineage beside them, a legacy-only service, a tenant-A-only one, and a
// service holding one attributed lineage beside a legacy one.
func TwoTenantServiceLineageRows() []ServiceLineageFixtureRow {
	return []ServiceLineageFixtureRow{
		{ServiceID: ServiceLineageSharedID, ScopeID: "scope-a", ScopeKind: "repository", SourceKey: "repo-a", PriorGenerationID: "gen-a-prior", CurrentGenerationID: "gen-a-current"},
		{ServiceID: ServiceLineageSharedID, ScopeID: "scope-b", ScopeKind: "repository", SourceKey: "repo-b", PriorGenerationID: "gen-b-prior", CurrentGenerationID: "gen-b-current"},
		{ServiceID: ServiceLineageSharedID, PriorGenerationID: "gen-api-legacy-prior", CurrentGenerationID: "gen-api-legacy"},
		{ServiceID: ServiceLineageLegacyID, PriorGenerationID: "gen-legacy-prior", CurrentGenerationID: "gen-legacy-current"},
		{ServiceID: ServiceLineageTenantAOnlyID, ScopeID: "scope-a", ScopeKind: "repository", SourceKey: "repo-a", PriorGenerationID: "gen-a-only-prior", CurrentGenerationID: "gen-a-only-current"},
		{ServiceID: ServiceLineageMigratedID, PriorGenerationID: "gen-migrated-legacy-prior", CurrentGenerationID: "gen-migrated-legacy"},
		{ServiceID: ServiceLineageMigratedID, ScopeID: "scope-a", ScopeKind: "repository", SourceKey: "repo-a", PriorGenerationID: "gen-migrated-a-prior", CurrentGenerationID: "gen-migrated-a-current"},
	}
}

// GrantMirroringServiceChangedSince is the #6475 two-tenant fake for GET
// /api/v0/freshness/services/changed-since. Like GrantMirroringChangedSince it
// does not merely record the filter: it applies the same intersection
// resolveServiceChangedSinceScopeQuery applies
// (go/internal/storage/postgres/service_changed_since_sql.go) and the same
// lineage choice ComputeServiceChangedSinceDelta makes, so a handler that stops
// binding the caller's grant reads the other tenant's lineage here exactly as
// it would in Postgres. The real SQL is proven against Postgres by
// TestServiceChangedSinceBindsGrantToLineageScopeLive.
//
//	$2 = '' OR g.scope_id = $2                                      -> selector
//	$3::boolean = false                                             -> unbounded
//	g.scope_id = ANY($5)                                            -> scope grant
//	scope.scope_kind = 'repository' AND scope.source_key = ANY($4)  -> repository grant
//
// A NULL scope_id satisfies neither grant arm nor a non-empty selector.
type GrantMirroringServiceChangedSince struct {
	Rows       []ServiceLineageFixtureRow
	LastFilter status.ServiceChangedSinceFilter
	Called     bool
}

// ComputeServiceChangedSinceDelta implements freshness.ServiceChangedSinceReader.
func (g *GrantMirroringServiceChangedSince) ComputeServiceChangedSinceDelta(
	_ context.Context, filter status.ServiceChangedSinceFilter,
) (status.ServiceChangedSinceSummary, error) {
	g.LastFilter = filter
	g.Called = true

	exists := false
	var attributed []ServiceLineageFixtureRow
	var legacy *ServiceLineageFixtureRow
	for i, row := range g.Rows {
		if row.ServiceID != filter.ServiceID {
			continue
		}
		exists = true
		if filter.ScopeID != "" && (row.ScopeID == "" || row.ScopeID != filter.ScopeID) {
			continue
		}
		if filter.Scoped && !serviceLineageGrantAdmits(filter, row) {
			continue
		}
		if row.ScopeID == "" {
			legacy = &g.Rows[i]
			continue
		}
		attributed = append(attributed, row)
	}

	var chosen ServiceLineageFixtureRow
	switch {
	case len(attributed) == 1:
		chosen = attributed[0]
	case len(attributed) > 1:
		ids := make([]string, 0, len(attributed))
		for _, row := range attributed {
			ids = append(ids, row.ScopeID)
		}
		sort.Strings(ids)
		return status.ServiceChangedSinceSummary{
			ServiceID: filter.ServiceID, SampleLimit: filter.SampleLimit, AmbiguousScopeIDs: ids,
		}, nil
	case legacy != nil && !filter.Scoped && filter.ScopeID == "":
		chosen = *legacy
	default:
		return status.ServiceChangedSinceSummary{OutsideGrant: filter.Scoped && exists}, nil
	}

	summary := status.ServiceChangedSinceSummary{
		ServiceID:                 filter.ServiceID,
		ScopeID:                   chosen.ScopeID,
		Unattributed:              chosen.ScopeID == "",
		CurrentActiveGenerationID: chosen.CurrentGenerationID,
		SampleLimit:               filter.SampleLimit,
	}
	// The prior lookup is bound to the chosen lineage: another lineage's
	// generation id, including another tenant's, matches nothing.
	if filter.SinceGenerationID != chosen.PriorGenerationID {
		return summary, nil
	}
	summary.SinceGenerationID = chosen.PriorGenerationID
	summary.Categories = []status.ChangedSinceCategoryDelta{{
		Category: status.ChangedSinceCategoryOwnership,
		Counts:   status.ChangedSinceCounts{Updated: 1},
	}}
	return summary, nil
}

func serviceLineageGrantAdmits(filter status.ServiceChangedSinceFilter, row ServiceLineageFixtureRow) bool {
	if row.ScopeID == "" {
		return false
	}
	for _, granted := range filter.AllowedScopeIDs {
		if granted == row.ScopeID {
			return true
		}
	}
	if row.ScopeKind == "repository" {
		for _, granted := range filter.AllowedRepositoryIDs {
			if granted == row.SourceKey {
				return true
			}
		}
	}
	return false
}
