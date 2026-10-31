// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9685 disposable PostgreSQL 18 rescanned-scope proof: a rescan for a new digest reuses the operator-configured scope, so scope-level sibling attribution is ambiguous there
//eshu:readiness:env ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DISPOSABLE ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestSupplyChainImpactReadinessScanTierRescannedScopeLive is the #9685
// regression: one operator-configured image scope rescanned for a NEW
// digest. Scope IDs come from operator target configuration (the planner
// requires a scope_id, never a digest), so nothing at mint binds a scope to
// one digest — a rescan lands a new generation under the SAME scope. The
// scope then holds two committed generations: gen-old (superseded) with the
// old digest's analysis + os_package rows, and gen-new (active) with the new
// digest's analysis + os_package rows under the same image_reference.
// os_package rows carry no digest of their own, so scope-level sibling
// attribution is ambiguous here: a request for the OLD digest must NOT count
// the new image's active packages, while a request for the NEW digest counts
// exactly its active generation's rows through the same-generation
// (pair-keyed) sibling. scanner_worker.analysis counts its own
// digest-matching fact in both cases (direct digest match, unaffected).
func TestSupplyChainImpactReadinessScanTierRescannedScopeLive(t *testing.T) {
	dsn := os.Getenv("ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN")
	optIn := os.Getenv("ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 3*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	oldDigest := "sha256:" + strings.Repeat("d1", 32)
	newDigest := "sha256:" + strings.Repeat("d2", 32)
	// 3 os_package rows seed the superseded generation as historical evidence;
	// only the active generation's rows may count as current evidence.
	const newOSPackageCount = 5 // active generation: the new image's current evidence

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ('rescan-scope', 'container_image', 'scanner_worker', 'rescan-target', 'scanner_worker', 'rescan-target', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb);
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('rescan-scope', 'rescan-gen-old', 'sync', clock_timestamp(), clock_timestamp(), 'superseded'),
       ('rescan-scope', 'rescan-gen-new', 'sync', clock_timestamp(), clock_timestamp(), 'active');
UPDATE ingestion_scopes SET active_generation_id = 'rescan-gen-new' WHERE scope_id = 'rescan-scope';
`); err != nil {
		t.Fatalf("seed rescanned scope: %v", err)
	}
	// Both generations' analyses name the SAME image_reference (the rescan
	// target did not move) but different image_digests — the ambiguous
	// scope-level key. Parameterized INSERTs are issued separately: pgx's
	// extended protocol rejects multi-statement query text.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('rescan-analysis-old', 'rescan-scope', 'rescan-gen-old', 'scanner_worker.analysis', 'rescan-analysis-old-key', 'scanner_worker', 'rescan-analysis-old', clock_timestamp(), clock_timestamp(), FALSE,
  jsonb_build_object('analyzer', 'image_unpacking', 'target_kind', 'image', 'target_locator_hash', md5('rescan-old'), 'analysis_status', 'completed', 'coverage_status', 'supported',
    'result_count', 3, 'fact_count', 3, 'image_reference', 'registry.example.com/team/rescanned:prod',
    'image_digest', $1::text, 'evidence_source', 'scanner_worker', 'extraction_reason', 'scheduled_scan'))
`, oldDigest); err != nil {
		t.Fatalf("seed old analysis fact: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('rescan-analysis-new', 'rescan-scope', 'rescan-gen-new', 'scanner_worker.analysis', 'rescan-analysis-new-key', 'scanner_worker', 'rescan-analysis-new', clock_timestamp(), clock_timestamp(), FALSE,
  jsonb_build_object('analyzer', 'image_unpacking', 'target_kind', 'image', 'target_locator_hash', md5('rescan-new'), 'analysis_status', 'completed', 'coverage_status', 'supported',
    'result_count', 5, 'fact_count', 5, 'image_reference', 'registry.example.com/team/rescanned:prod',
    'image_digest', $1::text, 'evidence_source', 'scanner_worker', 'extraction_reason', 'scheduled_scan'))
`, newDigest); err != nil {
		t.Fatalf("seed new analysis fact: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'rescan-ospkg-old-' || p, 'rescan-scope', 'rescan-gen-old', 'vulnerability.os_package', 'rescan-ospkg-old-key-' || p, 'scanner_worker', 'rescan-ospkg-old-' || p, clock_timestamp(), clock_timestamp(), FALSE,
  jsonb_build_object('distro', 'debian', 'distro_version', '12', 'package_manager', 'dpkg', 'name', 'pkg-old-' || p, 'arch', 'amd64',
    'installed_version_raw', '1.0.' || p, 'repository_class', 'vendor', 'vendor_advisory_source', 'debian')
FROM generate_series(1, 3) AS p;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'rescan-ospkg-new-' || p, 'rescan-scope', 'rescan-gen-new', 'vulnerability.os_package', 'rescan-ospkg-new-key-' || p, 'scanner_worker', 'rescan-ospkg-new-' || p, clock_timestamp(), clock_timestamp(), FALSE,
  jsonb_build_object('distro', 'debian', 'distro_version', '12', 'package_manager', 'dpkg', 'name', 'pkg-new-' || p, 'arch', 'amd64',
    'installed_version_raw', '2.0.' || p, 'repository_class', 'vendor', 'vendor_advisory_source', 'debian')
FROM generate_series(1, 5) AS p;
ANALYZE fact_records;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;
`); err != nil {
		t.Fatalf("seed os_package rows: %v", err)
	}

	queryFamilies := func(digest string) map[string]int {
		t.Helper()
		args := readinessArgsForQuery(ReadinessQuery{SubjectDigest: digest})
		rows, err := db.QueryContext(ctx, ListReadinessQuery, args...)
		if err != nil {
			t.Fatalf("query production shape for %s: %v", digest, err)
		}
		defer func() { _ = rows.Close() }()
		families := map[string]int{}
		for rows.Next() {
			var family string
			var factCount int
			var latest sql.NullTime
			var incompleteFlag sql.NullBool
			var reasons array.StringArray
			var a, b, c sql.NullString
			if err := rows.Scan(&family, &factCount, &latest, &incompleteFlag, &reasons, &a, &b, &c); err != nil {
				t.Fatalf("scan: %v", err)
			}
			families[family] = factCount
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		return families
	}

	oldFamilies := queryFamilies(oldDigest)
	if got := oldFamilies["scanner_worker.analysis"]; got != 1 {
		t.Fatalf("old digest families[scanner_worker.analysis] = %d, want 1 (its own digest-matching scan proof)", got)
	}
	if got := oldFamilies["vulnerability.os_package"]; got != 0 {
		t.Fatalf("old digest families[vulnerability.os_package] = %d, want 0 (scope holds two digests, so scope-level attribution is withheld; got the new image's %d active packages)", got, newOSPackageCount)
	}
	newFamilies := queryFamilies(newDigest)
	if got := newFamilies["scanner_worker.analysis"]; got != 1 {
		t.Fatalf("new digest families[scanner_worker.analysis] = %d, want 1 (its own digest-matching scan proof)", got)
	}
	if got := newFamilies["vulnerability.os_package"]; got != newOSPackageCount {
		t.Fatalf("new digest families[vulnerability.os_package] = %d, want %d (active rows pair-matched to the same generation's digest-matching analysis)", got, newOSPackageCount)
	}
}
