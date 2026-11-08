// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// listScopedSupplyChainImpactFindingPackagesQuery returns the distinct CVE,
// advisory, and package identities of one generation's active finding rows
// for stamped CVEs (#9779). The fact kind is a literal so the planner proves
// the predicate of the partial finding indexes under a generic plan, the same
// shape the holder read and the writer's stored read use. The handler seeds
// its scoped evidence filters from these rows instead of from everything the
// scope holds.
const listScopedSupplyChainImpactFindingPackagesQuery = `
SELECT DISTINCT payload->>'cve_id',
       COALESCE(payload->>'advisory_id', ''),
       COALESCE(payload->>'package_id', ''),
       COALESCE(payload->>'purl', ''),
       COALESCE(payload->>'package_name', ''),
       COALESCE(payload->>'ecosystem', '')
FROM fact_records
WHERE fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND scope_id = $1
  AND generation_id = $2
  AND is_tombstone = FALSE
  AND payload->>'cve_id' = ANY($3::text[])
`

// ListScopedSupplyChainImpactFindingPackages loads the finding-row package
// and advisory seeds of one (scope, generation) for stamped CVEs. An empty
// stamp reads nothing; tombstoned rows are excluded so a retracted copy
// never seeds a load.
func (s FactStore) ListScopedSupplyChainImpactFindingPackages(
	ctx context.Context,
	scopeID string,
	generationID string,
	cveIDs []string,
) ([]supplychaincore.ScopedFindingPackage, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	if len(cveIDs) == 0 {
		return nil, nil
	}
	var out []supplychaincore.ScopedFindingPackage
	err := withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		rows, err := queryer.QueryContext(ctx, listScopedSupplyChainImpactFindingPackagesQuery, scopeID, generationID, cveIDs)
		if err != nil {
			return fmt.Errorf("read scoped supply chain impact finding packages: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var row supplychaincore.ScopedFindingPackage
			if err := rows.Scan(&row.CVEID, &row.AdvisoryID, &row.PackageID, &row.PURL, &row.PackageName, &row.Ecosystem); err != nil {
				return fmt.Errorf("scan scoped supply chain impact finding package: %w", err)
			}
			if row.CVEID == "" {
				continue
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
