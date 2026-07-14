// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// listAppliedPagerDutyServiceRoutingQuery selects the applied PagerDuty service
// routing facts for one scope generation. Only resource_class='service' rows are
// edge-anchorable: they carry the real PagerDuty provider service id
// (provider_object_id) plus the Terraform backend (kind, locator_hash) the
// tfstatebackend resolver maps to an owning repository. Tombstoned rows and rows
// of other resource classes (escalation policies, integrations, ...) are
// excluded. The generation is the intent generation, so the correlation reflects
// the same applied snapshot the incident-routing materialization saw.
//
// Decision (#4683): this stays raw SQL permanently, it is not a pending
// decode-in-Go conversion. The resource_class='service' predicate and the
// provider_object_id ordering push directly into the partial expression index
// fact_records_incident_routing_applied_service_idx
// (schema_fact_records_incident_indexes.go); decode-in-Go would drop that
// predicate from the query text, which makes the partial index structurally
// ineligible (a partial index only matches a query whose WHERE clause implies
// the index's own predicate), forcing a Seq Scan over every row of this fact
// kind in scope instead. Measured on a throwaway 45k-row seed: the indexed
// shape ran in 1.121ms over 459 buffers (Index Scan); the decode-in-Go shape
// (no resource_class/provider_object_id pushdown) ran in 11.454ms over 1553
// buffers (Seq Scan, 40000 rows removed by filter). See
// docs/internal/design/4683-incident-routing-sql-decision.md for the full
// EXPLAIN ANALYZE evidence and rationale. The compensating governance for the
// #4573 payload-usage manifest gate's resulting blind spot is
// TestIncidentRoutingSQLProjectedFieldsAreSchemaDeclared
// (incident_routing_sql_schema_lockstep_test.go), not a future migration onto
// the typed decode seam.
const listAppliedPagerDutyServiceRoutingQuery = `
SELECT
    fact.fact_id,
    fact.stable_fact_key,
    fact.payload->>'provider_object_id'  AS provider_object_id,
    fact.payload->>'name_fingerprint'    AS name_fingerprint,
    fact.payload->>'backend_kind'        AS backend_kind,
    fact.payload->>'locator_hash'        AS locator_hash
FROM fact_records AS fact
WHERE fact.fact_kind = 'incident_routing.applied_pagerduty_resource'
  AND fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.is_tombstone = FALSE
  AND fact.payload->>'resource_class' = 'service'
ORDER BY fact.payload->>'provider_object_id' ASC, fact.fact_id ASC
`

// PostgresAppliedPagerDutyServiceRoutingLoader reads applied PagerDuty service
// routing facts for the incident-repository correlation reducer domain. It is
// the durable, name-free input: the rows carry the provider service id and
// backend locator, never the service name as a join key.
type PostgresAppliedPagerDutyServiceRoutingLoader struct {
	DB Queryer
}

// LoadAppliedPagerDutyServiceRouting implements
// reducer.AppliedPagerDutyServiceRoutingLoader. Rows without a provider service
// id are still returned (their ProviderObjectID is blank) so the builder can
// record them as provenance-only rejected decisions rather than the loader
// silently hiding partial coverage.
func (l PostgresAppliedPagerDutyServiceRoutingLoader) LoadAppliedPagerDutyServiceRouting(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]reducer.AppliedPagerDutyServiceRouting, error) {
	if l.DB == nil {
		return nil, fmt.Errorf("applied pagerduty service routing database is required")
	}
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" || generationID == "" {
		return nil, fmt.Errorf("scope id and generation id must not be blank")
	}

	rows, err := l.DB.QueryContext(ctx, listAppliedPagerDutyServiceRoutingQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list applied pagerduty service routing: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]reducer.AppliedPagerDutyServiceRouting, 0)
	for rows.Next() {
		var (
			factID           string
			stableFactKey    string
			providerObjectID *string
			nameFingerprint  *string
			backendKind      *string
			locatorHash      *string
		)
		if err := rows.Scan(
			&factID, &stableFactKey, &providerObjectID, &nameFingerprint, &backendKind, &locatorHash,
		); err != nil {
			return nil, fmt.Errorf("scan applied pagerduty service routing: %w", err)
		}
		providerID := derefTrim(providerObjectID)
		out = append(out, reducer.AppliedPagerDutyServiceRouting{
			FactID:           strings.TrimSpace(factID),
			StableFactKey:    strings.TrimSpace(stableFactKey),
			ProviderObjectID: providerID,
			NameFingerprint:  derefTrim(nameFingerprint),
			BackendKind:      derefTrim(backendKind),
			LocatorHash:      derefTrim(locatorHash),
			// A non-blank provider id in an applied service fact is the raw
			// PagerDuty service id; the incident-context read path matches
			// incident.Service.ID against it by equality, so a present id is an
			// exact provider-id match by construction.
			ProviderIDExact: providerID != "",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied pagerduty service routing: %w", err)
	}
	return out, nil
}

func derefTrim(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// BackendRepositoryResolverAdapter bridges the tfstatebackend resolver to the
// reducer.BackendRepositoryResolver contract. It translates the resolver's
// sentinel errors into the data-only BackendRepositoryResolution the pure
// correlation builder classifies: no owner becomes a blank resolution
// (unresolved), an ambiguous owner becomes Ambiguous=true, and a single owner
// becomes the resolved repository id. Any other error propagates.
type BackendRepositoryResolverAdapter struct {
	Resolver *tfstatebackend.Resolver
}

// ResolveBackendRepository implements reducer.BackendRepositoryResolver.
func (a BackendRepositoryResolverAdapter) ResolveBackendRepository(
	ctx context.Context,
	backendKind string,
	locatorHash string,
) (reducer.BackendRepositoryResolution, error) {
	if a.Resolver == nil {
		return reducer.BackendRepositoryResolution{}, nil
	}
	anchor, err := a.Resolver.ResolveConfigCommitForBackend(ctx, backendKind, locatorHash)
	switch {
	case errors.Is(err, tfstatebackend.ErrNoConfigRepoOwnsBackend):
		return reducer.BackendRepositoryResolution{}, nil
	case errors.Is(err, tfstatebackend.ErrAmbiguousBackendOwner):
		return reducer.BackendRepositoryResolution{Ambiguous: true}, nil
	case err != nil:
		return reducer.BackendRepositoryResolution{}, fmt.Errorf(
			"resolve config commit for backend %s/%s: %w", backendKind, locatorHash, err,
		)
	}
	return reducer.BackendRepositoryResolution{RepositoryID: strings.TrimSpace(anchor.RepoID)}, nil
}

// ensure the applied-routing fact kind constant referenced by the query stays in
// lockstep with the facts package; a compile-time reference prevents silent
// drift if the kind string is renamed.
var _ = facts.IncidentRoutingAppliedPagerDutyResourceFactKind
