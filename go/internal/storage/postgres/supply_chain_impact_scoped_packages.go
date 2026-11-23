// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// listScopedSupplyChainImpactFindingPackagesQuery returns the distinct CVE,
// advisory, and package identities of one generation's active finding supports
// for stamped CVEs (#9779, #11197). It reads the per-support table through the
// (scope_id, generation_id) index, the same access path the writer's own
// support reads use. The handler seeds its scoped evidence filters from these
// rows instead of from everything the scope holds. The package name and PURL
// come from the support's verbatim payload, and so does the ecosystem: the
// ecosystem column is lowercased for filtering, and the seed must return the
// fact's own spelling. The empty cve id a support stores for a finding without
// one is excluded, as the fact read's equality never matched it.
const listScopedSupplyChainImpactFindingPackagesQuery = `
SELECT DISTINCT support.cve_id,
       support.advisory_id,
       support.package_id,
       COALESCE(support.payload->>'purl', ''),
       COALESCE(support.payload->>'package_name', ''),
       COALESCE(support.payload->>'ecosystem', '')
FROM supply_chain_impact_finding_supports AS support
WHERE support.scope_id = $1
  AND support.generation_id = $2
  AND support.cve_id = ANY($3::text[])
  AND support.cve_id <> ''
`

// ListScopedSupplyChainImpactFindingPackages loads the finding-row package
// and advisory seeds of one (scope, generation) for stamped CVEs. An empty
// stamp reads nothing; a retracted copy has no support, so it never seeds a
// load. The read refuses with supplychaincore.ErrSupportsNotReady while the
// supports table is not ready (#11197).
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
		if err := requireSupplyChainImpactSupportsReady(ctx, queryer); err != nil {
			return err
		}
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
