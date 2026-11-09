// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10464 disposable PostgreSQL proof that the advisory catalog serves the newest GHSA copy: two window copies of one advisory, older live plus newer live, read as the newer copy's severity and packages only
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
)

// seedGHSACatalogCopy stores one GHSA advisory copy (CVE plus
// affected_package facts) in its own write-once window scope.
func seedGHSACatalogCopy(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, scope, generation, factPrefix, cveID, advisoryID, modifiedAt string, score float64, packageID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes
        (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
        VALUES ($1, 'vulnerability_intelligence', 'vulnerability_intelligence', $1,
                'vulnerability_intelligence', $1, $2, $2, 'active', $3)`,
		scope, now, generation); err != nil {
		t.Fatalf("seed GHSA catalog scope %s: %v", scope, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations
        (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
        VALUES ($1, $2, 'schedule', $3, $3, 'active')`, scope, generation, now); err != nil {
		t.Fatalf("seed GHSA catalog generation %s: %v", scope, err)
	}
	cvePayload, err := json.Marshal(map[string]any{
		"cve_id": cveID, "advisory_id": advisoryID, "ghsa_id": advisoryID,
		"source": "ghsa", "modified_at": modifiedAt, "severity_label": "HIGH", "cvss_score": score,
	})
	if err != nil {
		t.Fatalf("marshal GHSA catalog cve payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_records
        (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
         source_fact_key, observed_at, ingested_at, is_tombstone, payload)
        VALUES ($1, $2, $3, 'vulnerability.cve', $1, 'vulnerability_intelligence',
                $1, $4, $4, FALSE, $5)`,
		factPrefix+"-cve", scope, generation, now, cvePayload); err != nil {
		t.Fatalf("seed GHSA catalog cve fact: %v", err)
	}
	packagePayload, err := json.Marshal(map[string]any{
		"cve_id": cveID, "advisory_id": advisoryID, "source": "ghsa",
		"package_id": packageID, "ecosystem": "npm", "affected_range": ">=1.0.0",
	})
	if err != nil {
		t.Fatalf("marshal GHSA catalog package payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_records
        (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
         source_fact_key, observed_at, ingested_at, is_tombstone, payload)
        VALUES ($1, $2, $3, 'vulnerability.affected_package', $1, 'vulnerability_intelligence',
                $1, $4, $4, FALSE, $5)`,
		factPrefix+"-pkg", scope, generation, now, packagePayload); err != nil {
		t.Fatalf("seed GHSA catalog package fact: %v", err)
	}
}

// seedGHSACatalogCopyCVEOnly stores one GHSA advisory copy's CVE fact with
// no affected_package fact in its own write-once window scope.
func seedGHSACatalogCopyCVEOnly(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, scope, generation, factPrefix, cveID, advisoryID, modifiedAt string, score float64) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes
        (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
        VALUES ($1, 'vulnerability_intelligence', 'vulnerability_intelligence', $1,
                'vulnerability_intelligence', $1, $2, $2, 'active', $3)`,
		scope, now, generation); err != nil {
		t.Fatalf("seed GHSA catalog scope %s: %v", scope, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations
        (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
        VALUES ($1, $2, 'schedule', $3, $3, 'active')`, scope, generation, now); err != nil {
		t.Fatalf("seed GHSA catalog generation %s: %v", scope, err)
	}
	cvePayload, err := json.Marshal(map[string]any{
		"cve_id": cveID, "advisory_id": advisoryID, "ghsa_id": advisoryID,
		"source": "ghsa", "modified_at": modifiedAt, "severity_label": "MEDIUM", "cvss_score": score,
	})
	if err != nil {
		t.Fatalf("marshal GHSA catalog cve payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_records
        (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
         source_fact_key, observed_at, ingested_at, is_tombstone, payload)
        VALUES ($1, $2, $3, 'vulnerability.cve', $1, 'vulnerability_intelligence',
                $1, $4, $4, FALSE, $5)`,
		factPrefix+"-cve", scope, generation, now, cvePayload); err != nil {
		t.Fatalf("seed GHSA catalog cve fact: %v", err)
	}
}

// TestSupplyChainAdvisoryCatalogServesNewestGHSACopyLive is the #10464
// read-model cell: the catalog serves the newest GHSA copy per window, so an
// older window copy's severity and packages stop shadowing the record its
// newer copy already replaced.
func TestSupplyChainAdvisoryCatalogServesNewestGHSACopyLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	const cveID = "CVE-2026-9401"
	const advisoryID = "GHSA-mmmm-nnnn-oooo"
	seedGHSACatalogCopy(t, ctx, db, now,
		"vuln-intel://ghsa/reviewed/_window/1767225600000000-1767312000000000", "generation:ghsa-w1",
		"older", cveID, advisoryID, "2026-10-01T00:00:00Z", 9.8, "pkg:old/lib")
	seedGHSACatalogCopy(t, ctx, db, now,
		"vuln-intel://ghsa/reviewed/_window/1767312000000000-1767398400000000", "generation:ghsa-w2",
		"newer", cveID, advisoryID, "2026-11-01T00:00:00Z", 5.0, "pkg:new/lib")

	page, err := advisory.NewPostgresCatalogStore(db).ListAdvisoryCatalog(ctx, advisory.CatalogFilter{Query: advisoryID, Limit: 10})
	if err != nil {
		t.Fatalf("ListAdvisoryCatalog(): %v", err)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("catalog rows = %d, want 1 for one advisory in two windows", len(page.Rows))
	}
	row := page.Rows[0]
	if row.CVSSScore != 5.0 {
		t.Fatalf("CVSSScore = %v, want the newest copy's 5.0, not the older copy's 9.8", row.CVSSScore)
	}
	if !slices.Equal(row.PackageIDs, []string{"pkg:new/lib"}) {
		t.Fatalf("PackageIDs = %v, want only the newest copy's package", row.PackageIDs)
	}
	if !slices.Equal(row.Sources, []string{"ghsa"}) {
		t.Fatalf("Sources = %v, want [ghsa]", row.Sources)
	}
}

// TestSupplyChainAdvisoryCatalogKeepsSupersededPackagesForPackagelessWinnerLive
// is the #10464 packageless-winner cell: when the winning copy names no
// package of its own, the record's package evidence lives only in the
// superseded copy's scope, so the catalog still reads it instead of dropping
// the advisory's packages.
func TestSupplyChainAdvisoryCatalogKeepsSupersededPackagesForPackagelessWinnerLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	const cveID = "CVE-2026-9402"
	const advisoryID = "GHSA-pppp-qqqq-rrrr"
	seedGHSACatalogCopy(t, ctx, db, now,
		"vuln-intel://ghsa/reviewed/_window/1767225600000000-1767312000000000", "generation:ghsa-w1",
		"older", cveID, advisoryID, "2026-10-01T00:00:00Z", 9.8, "pkg:old/lib")
	seedGHSACatalogCopyCVEOnly(t, ctx, db, now,
		"vuln-intel://ghsa/reviewed/_window/1767312000000000-1767398400000000", "generation:ghsa-w2",
		"newer", cveID, advisoryID, "2026-11-01T00:00:00Z", 5.0)

	page, err := advisory.NewPostgresCatalogStore(db).ListAdvisoryCatalog(ctx, advisory.CatalogFilter{Query: advisoryID, Limit: 10})
	if err != nil {
		t.Fatalf("ListAdvisoryCatalog(): %v", err)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("catalog rows = %d, want 1 for one advisory in two windows", len(page.Rows))
	}
	row := page.Rows[0]
	if row.CVSSScore != 5.0 {
		t.Fatalf("CVSSScore = %v, want the packageless winner's 5.0, not the older copy's 9.8", row.CVSSScore)
	}
	if !slices.Equal(row.PackageIDs, []string{"pkg:old/lib"}) {
		t.Fatalf("PackageIDs = %v, want the superseded copy's package: the winner names none", row.PackageIDs)
	}
}

// seedGHSACatalogPackageOnly stores one GHSA affected_package fact whose own
// write-once window scope holds no LIVE CVE fact: the scope's CVE copy is a
// tombstone (the retracted-copy shape the reducer keeps via !present[key][own]).
// The package payload still names its CVE id, as production GHSA packages do:
// retraction tombstones the CVE fact, it does not strip the package payload.
func seedGHSACatalogPackageOnly(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, scope, generation, factPrefix, cveID, advisoryID, packageID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes
        (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
        VALUES ($1, 'vulnerability_intelligence', 'vulnerability_intelligence', $1,
                'vulnerability_intelligence', $1, $2, $2, 'active', $3)`,
		scope, now, generation); err != nil {
		t.Fatalf("seed GHSA package-only scope %s: %v", scope, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations
        (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
        VALUES ($1, $2, 'schedule', $3, $3, 'active')`, scope, generation, now); err != nil {
		t.Fatalf("seed GHSA package-only generation %s: %v", scope, err)
	}
	tombstonedCVE, err := json.Marshal(map[string]any{
		"cve_id": cveID, "advisory_id": advisoryID, "ghsa_id": advisoryID,
		"source": "ghsa", "modified_at": "2026-10-01T00:00:00Z", "severity_label": "HIGH", "cvss_score": 9.8,
	})
	if err != nil {
		t.Fatalf("marshal GHSA package-only tombstone payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_records
        (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
         source_fact_key, observed_at, ingested_at, is_tombstone, payload)
        VALUES ($1, $2, $3, 'vulnerability.cve', $1,
                'vulnerability_intelligence', $1, $4, $4, TRUE, $5)`,
		factPrefix+"-cve-tombstone", scope, generation, now, tombstonedCVE); err != nil {
		t.Fatalf("seed GHSA package-only tombstone fact: %v", err)
	}
	packagePayload, err := json.Marshal(map[string]any{
		"cve_id": cveID, "advisory_id": advisoryID, "ghsa_id": advisoryID, "source": "ghsa",
		"ecosystem": "npm", "package_id": packageID,
	})
	if err != nil {
		t.Fatalf("marshal GHSA package-only payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_records
        (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
         source_fact_key, observed_at, ingested_at, is_tombstone, payload)
        VALUES ($1, $2, $3, 'vulnerability.affected_package', $1,
                'vulnerability_intelligence', $1, $4, $4, FALSE, $5)`,
		factPrefix+"-package", scope, generation, now, packagePayload); err != nil {
		t.Fatalf("seed GHSA package-only fact: %v", err)
	}
}

// TestSupplyChainAdvisoryCatalogKeepsPackageOnlyScopePackagesLive is the
// #10464 package-only-scope cell: a scope whose CVE copy is tombstoned holds
// an affected_package fact with no same-key live CVE fact of its own, so it
// keeps its package even when the winning copy elsewhere names a package
// (the reducer's !present[key][own] keep). Without the own-scope arm the
// catalog hides a live source it cannot attribute.
func TestSupplyChainAdvisoryCatalogKeepsPackageOnlyScopePackagesLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	const cveID = "CVE-2026-9403"
	const advisoryID = "GHSA-ssss-tttt-uuuu"
	seedGHSACatalogPackageOnly(t, ctx, db, now,
		"vuln-intel://ghsa/reviewed/_window/1767225600000000-1767312000000000", "generation:ghsa-p1",
		"pkgonly", cveID, advisoryID, "pkg:orphan/lib")
	seedGHSACatalogCopy(t, ctx, db, now,
		"vuln-intel://ghsa/reviewed/_window/1767312000000000-1767398400000000", "generation:ghsa-p2",
		"winner", cveID, advisoryID, "2026-11-01T00:00:00Z", 5.0, "pkg:winner/lib")

	page, err := advisory.NewPostgresCatalogStore(db).ListAdvisoryCatalog(ctx, advisory.CatalogFilter{Query: advisoryID, Limit: 10})
	if err != nil {
		t.Fatalf("ListAdvisoryCatalog(): %v", err)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("catalog rows = %d, want 1 for one advisory in two windows", len(page.Rows))
	}
	row := page.Rows[0]
	if !slices.Equal(row.PackageIDs, []string{"pkg:orphan/lib", "pkg:winner/lib"}) {
		t.Fatalf("PackageIDs = %v, want both the package-only scope's and the winner's package", row.PackageIDs)
	}
}
