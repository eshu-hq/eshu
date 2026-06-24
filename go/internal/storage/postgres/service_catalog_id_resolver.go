// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// serviceCatalogIDForWorkloadQuery resolves the durable Eshu catalog service id
// for a workload id through reducer-owned reducer_service_catalog_correlation
// facts. Only exact/derived, non-provenance rows in the active generation are
// admissible, mirroring ServiceIncidentEvidenceLoader's admissibility gate so
// the resolver and the loader agree on which correlation truth is canonical.
//
// The query returns every distinct catalog service id mapped to the workload so
// the caller can fail closed when a workload resolves to more than one catalog
// service: ambiguous catalog ownership must never silently attribute
// service-scoped evidence (incidents) to a single arbitrary catalog service.
const serviceCatalogIDForWorkloadQuery = `
SELECT DISTINCT fact.payload->>'service_id' AS service_id
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_service_catalog_correlation'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'provenance_only' = 'false'
  AND fact.payload->>'outcome' IN ('exact', 'derived')
  AND fact.payload->>'workload_id' = $1
  AND NULLIF(fact.payload->>'service_id', '') IS NOT NULL
`

// ErrAmbiguousCatalogService reports that a workload maps to more than one
// active catalog service. Callers must treat it as "do not attribute" rather
// than picking one, so service-scoped evidence is never mis-attributed.
var ErrAmbiguousCatalogService = errors.New("workload maps to multiple active catalog services")

// ServiceCatalogIDResolver resolves a workload id to its durable Eshu catalog
// service id. It is the bridge the report's incident lane needs: the service
// story exposes a workload id (service_identity.service_id = workload id), but
// ServiceIncidentEvidenceLoader keys on the catalog service id. Passing the
// workload id straight through would silently return no incidents for a service
// that has them — a hidden wrong result — so the wiring resolves first.
type ServiceCatalogIDResolver struct {
	queryer Queryer
}

// NewServiceCatalogIDResolver constructs a read-only resolver over the shared
// query surface.
func NewServiceCatalogIDResolver(queryer Queryer) ServiceCatalogIDResolver {
	return ServiceCatalogIDResolver{queryer: queryer}
}

// ResolveCatalogServiceID returns the durable catalog service id for the given
// workload id. It returns an empty id with a nil error when no admissible
// correlation exists, so the caller leaves a dependent section unsupported
// rather than fabricating a false attribution. A blank workload id resolves to
// an empty id without issuing a query. It returns ErrAmbiguousCatalogService
// when the workload maps to more than one active catalog service, failing
// closed instead of attributing evidence to an arbitrary one.
func (r ServiceCatalogIDResolver) ResolveCatalogServiceID(ctx context.Context, workloadID string) (string, error) {
	if r.queryer == nil {
		return "", fmt.Errorf("catalog service id queryer is required")
	}
	workloadID = strings.TrimSpace(workloadID)
	if workloadID == "" {
		return "", nil
	}

	rows, err := r.queryer.QueryContext(ctx, serviceCatalogIDForWorkloadQuery, workloadID)
	if err != nil {
		return "", fmt.Errorf("resolve catalog service id: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Dedup in Go as well as in SQL: the query already SELECTs DISTINCT, but the
	// ambiguity decision must not silently depend on that contract. A workload is
	// ambiguous only when it maps to more than one DISTINCT catalog service id, so
	// repeated rows for the same id collapse to one rather than tripping the
	// fail-closed gate.
	seen := make(map[string]struct{}, 2)
	var resolved string
	for rows.Next() {
		var serviceID string
		if scanErr := rows.Scan(&serviceID); scanErr != nil {
			return "", fmt.Errorf("scan catalog service id: %w", scanErr)
		}
		if serviceID = strings.TrimSpace(serviceID); serviceID == "" {
			continue
		}
		if _, ok := seen[serviceID]; ok {
			continue
		}
		seen[serviceID] = struct{}{}
		resolved = serviceID
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("resolve catalog service id: %w", err)
	}
	if len(seen) > 1 {
		return "", ErrAmbiguousCatalogService
	}
	return resolved, nil
}
