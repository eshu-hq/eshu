// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestSupplyChainImpactReadinessScanTierQueryPlanLive is the ongoing
// query-plan/correctness regression proof for the OS-package scan-tier CTEs
// added by issue #5467 (vulnerability.os_package, scanner_worker.analysis).
// It is gated behind ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN because it
// needs a real Postgres instance; see the entity-name and content-index live
// tests in this package for the same pattern.
//
// Scope of the latency claim this test enforces: a single query on a fully
// warm buffer cache (freshly ANALYZEd, same session, no concurrent load)
// against the ~7k-row corpus seedScanTierReadinessExplainCorpus builds below
// stays under the 500ms local-performance-envelope budget. That is not a
// general or cold-cache latency guarantee — it says nothing about disk-read
// latency, lock contention, or production-scale row counts.
//
// The #5467 Prove-The-Theory-First evidence (proving the SQL change itself
// before it was written, not an ongoing bound) was a ONE-TIME, NOT
// reproducible-from-this-file measurement: a throwaway harness derived the
// pre-#5467 9-branch union's query text from origin/main and ran both shapes
// EXPLAIN ANALYZE against a separate, larger (~50k-row), also fully-cached
// corpus. Pre-change: 131.3ms execution, 2,665,455 shared hit blocks,
// 0 shared read (disk) blocks. Post-change (the 11-branch union with the two
// new CTEs): 147.7ms execution, 2,993,340 shared hit blocks, 0 shared read
// blocks — a ~12.5% execution increase and ~12.3% buffer increase, both
// proportional to the 2-of-11 added branches. Like this test's own bound,
// that comparison was cached and single-corpus, not a cold-cache or
// production-scale measurement; it was captured once to justify shipping the
// SQL change and then discarded (per CLAUDE.md's "throwaway shim" framing for
// a theory proof), which is why it cannot be rerun from this file. This test
// is what carries the change's latency contract forward.
func TestSupplyChainImpactReadinessScanTierQueryPlanLive(t *testing.T) {
	dsn := os.Getenv("ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN")
	optIn := os.Getenv("ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 3*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	targetDigest := seedScanTierReadinessExplainCorpus(t, ctx, db)

	args := []any{
		pq.Array(vulnerabilityAdvisoryFactKinds),
		pq.Array(vulnerabilityExploitabilityFactKinds),
		pq.Array(packageConsumptionCorrelationFactKinds),
		pq.Array(packageRegistryFactKinds),
		pq.Array(sbomComponentFactKinds),
		pq.Array(sbomAttestationFactKinds),
		pq.Array(containerImageIdentityFactKinds),
		pq.Array(vulnerabilitySourceSnapshotFactKinds),
		"", "", "", targetDigest, "", "",
		pq.Array(vulnerabilityOSPackageFactKinds),
		pq.Array(scannerWorkerAnalysisFactKinds),
	}

	var raw []byte
	if err := db.QueryRowContext(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+listSupplyChainImpactReadinessQuery,
		args...,
	).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN production scan-tier readiness query: %v", err)
	}
	var plan []struct {
		ExecutionTime float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil || len(plan) != 1 {
		t.Fatalf("decode scan-tier readiness plan: count=%d err=%v raw=%s", len(plan), err, raw)
	}
	t.Logf("scan-tier readiness plan: execution_ms=%.3f", plan[0].ExecutionTime)
	if plan[0].ExecutionTime > 500 {
		t.Fatalf("execution time = %.3fms, want <=500ms (local-performance-envelope budget)", plan[0].ExecutionTime)
	}

	// Correctness: the scanned target image's evidence must actually surface.
	rows, err := db.QueryContext(ctx, listSupplyChainImpactReadinessQuery, args...)
	if err != nil {
		t.Fatalf("query production shape: %v", err)
	}
	defer func() { _ = rows.Close() }()
	families := map[string]int{}
	for rows.Next() {
		var family string
		var factCount int
		var latest sql.NullTime
		var incompleteFlag sql.NullBool
		var reasons pq.StringArray
		var a, b, c sql.NullString
		if err := rows.Scan(&family, &factCount, &latest, &incompleteFlag, &reasons, &a, &b, &c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		families[family] = factCount
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if got := families["vulnerability.os_package"]; got != 500 {
		t.Fatalf("families[vulnerability.os_package] = %d, want 500", got)
	}
	if got := families["scanner_worker.analysis"]; got != 1 {
		t.Fatalf("families[scanner_worker.analysis] = %d, want 1", got)
	}
}

// TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive locks
// the semi-join invariant a review finding raised: with TWO
// scanner_worker.analysis facts matching the same scope+generation (a second
// analyzer, or any other source of more than one matching analysis row),
// vulnerability.os_package's fact_count must still equal the os_package row
// count exactly — never doubled by a JOIN-style fan-out. This seeds its own
// minimal scope rather than reusing seedScanTierReadinessExplainCorpus (which
// seeds exactly one analysis fact per scope, so it could never catch this
// regression) and runs the identical shipped query.
func TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive(t *testing.T) {
	dsn := os.Getenv("ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DSN")
	optIn := os.Getenv("ESHU_SCAN_TIER_READINESS_EXPLAIN_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 3*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	targetDigest := "sha256:" + strings.Repeat("b6", 32)
	const osPackageCount = 7

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ('fanout-scope', 'container_image', 'scanner_worker', 'fanout', 'scanner_worker', 'fanout', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb);
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('fanout-scope', 'fanout-gen', 'sync', clock_timestamp(), clock_timestamp(), 'active');
UPDATE ingestion_scopes SET active_generation_id = 'fanout-gen' WHERE scope_id = 'fanout-scope';
`); err != nil {
		t.Fatalf("seed fan-out scope: %v", err)
	}
	// Two scanner_worker.analysis facts in the SAME scope_id+generation_id,
	// both matching the target digest — the exact shape that would fan out
	// vulnerability.os_package's COUNT(*) under a JOIN.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES
  ('fanout-analysis-1', 'fanout-scope', 'fanout-gen', 'scanner_worker.analysis', 'fanout-analysis-key-1', 'scanner_worker', 'fanout-analysis-1', clock_timestamp(), clock_timestamp(), FALSE,
    jsonb_build_object('analyzer', 'ospkg', 'target_kind', 'image', 'target_locator_hash', md5('fanout-1'), 'analysis_status', 'completed', 'coverage_status', 'supported',
      'result_count', 7, 'fact_count', 7, 'image_reference', 'registry.example.com/team/fanout:prod',
      'image_digest', $1::text, 'evidence_source', 'scanner_worker', 'extraction_reason', 'scheduled_scan')),
  ('fanout-analysis-2', 'fanout-scope', 'fanout-gen', 'scanner_worker.analysis', 'fanout-analysis-key-2', 'scanner_worker', 'fanout-analysis-2', clock_timestamp(), clock_timestamp(), FALSE,
    jsonb_build_object('analyzer', 'ospkg-second', 'target_kind', 'image', 'target_locator_hash', md5('fanout-2'), 'analysis_status', 'completed', 'coverage_status', 'supported',
      'result_count', 7, 'fact_count', 7, 'image_reference', 'registry.example.com/team/fanout:prod',
      'image_digest', $1::text, 'evidence_source', 'scanner_worker', 'extraction_reason', 'scheduled_scan'))
`, targetDigest); err != nil {
		t.Fatalf("seed two matching analysis facts: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'fanout-ospkg-' || p, 'fanout-scope', 'fanout-gen', 'vulnerability.os_package', 'fanout-ospkg-key-' || p, 'scanner_worker', 'fanout-ospkg-' || p, clock_timestamp(), clock_timestamp(), FALSE,
  jsonb_build_object('distro', 'debian', 'distro_version', '12', 'package_manager', 'dpkg', 'name', 'pkg-fanout-' || p, 'arch', 'amd64',
    'installed_version_raw', '1.0.' || p, 'repository_class', 'vendor', 'vendor_advisory_source', 'debian')
FROM generate_series(1, 7) AS p;
ANALYZE fact_records;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;
`); err != nil {
		t.Fatalf("seed os_package rows: %v", err)
	}

	args := []any{
		pq.Array(vulnerabilityAdvisoryFactKinds),
		pq.Array(vulnerabilityExploitabilityFactKinds),
		pq.Array(packageConsumptionCorrelationFactKinds),
		pq.Array(packageRegistryFactKinds),
		pq.Array(sbomComponentFactKinds),
		pq.Array(sbomAttestationFactKinds),
		pq.Array(containerImageIdentityFactKinds),
		pq.Array(vulnerabilitySourceSnapshotFactKinds),
		"", "", "", targetDigest, "", "",
		pq.Array(vulnerabilityOSPackageFactKinds),
		pq.Array(scannerWorkerAnalysisFactKinds),
	}
	rows, err := db.QueryContext(ctx, listSupplyChainImpactReadinessQuery, args...)
	if err != nil {
		t.Fatalf("query production shape: %v", err)
	}
	defer func() { _ = rows.Close() }()
	families := map[string]int{}
	for rows.Next() {
		var family string
		var factCount int
		var latest sql.NullTime
		var incompleteFlag sql.NullBool
		var reasons pq.StringArray
		var a, b, c sql.NullString
		if err := rows.Scan(&family, &factCount, &latest, &incompleteFlag, &reasons, &a, &b, &c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		families[family] = factCount
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if got := families["scanner_worker.analysis"]; got != 2 {
		t.Fatalf("families[scanner_worker.analysis] = %d, want 2 (both matching analysis facts counted directly)", got)
	}
	if got := families["vulnerability.os_package"]; got != osPackageCount {
		t.Fatalf("families[vulnerability.os_package] = %d, want %d (must NOT fan out to %d under two matching analysis facts)", got, osPackageCount, osPackageCount*2)
	}
}

// seedScanTierReadinessExplainCorpus seeds a representative fact_records
// corpus — noise across the pre-existing readiness families plus the
// worst-case scan-tier shape (500 installed packages, the
// maxSupplyChainImpactOSPackageAdvisoryTargets bound) for one target
// image — and returns the target image's digest.
func seedScanTierReadinessExplainCorpus(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	targetDigest := "sha256:" + strings.Repeat("a5", 32)

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
SELECT 'noise-scope-' || n, 'package', 'vulnerability_intelligence', 'noise-' || n, 'vulnerability_intelligence', 'noise-' || n, clock_timestamp(), clock_timestamp(), 'active',
  jsonb_build_object('repo_id', 'repo-noise-' || (n % 200))
FROM generate_series(1, 5000) AS n;
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'noise-scope-' || n, 'gen-noise-' || n, 'sync', clock_timestamp(), clock_timestamp(), 'active'
FROM generate_series(1, 5000) AS n;
UPDATE ingestion_scopes SET active_generation_id = 'gen-noise-' || substring(scope_id from 13)
WHERE scope_id LIKE 'noise-scope-%';
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'noise-advisory-' || n, 'noise-scope-' || n, 'gen-noise-' || n, 'vulnerability.cve', 'noise-advisory-key-' || n, 'vulnerability_intelligence', 'noise-advisory-' || n, clock_timestamp(), clock_timestamp(), FALSE,
  jsonb_build_object('cve_id', 'CVE-2026-' || lpad((10000 + n)::text, 5, '0'))
FROM generate_series(1, 5000) AS n;
`); err != nil {
		t.Fatalf("seed noise families: %v", err)
	}

	// 50 realistic scanned-image scan scopes, each carrying 1
	// scanner_worker.analysis + ~30 os_package facts, unrelated to the
	// target digest — proves the join stays anchored to the requested image.
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
SELECT 'scan-scope-' || n, 'container_image', 'scanner_worker', 'scan-' || n, 'scanner_worker', 'scan-' || n, clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
FROM generate_series(1, 50) AS n;
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'scan-scope-' || n, 'scan-gen-' || n, 'sync', clock_timestamp(), clock_timestamp(), 'active'
FROM generate_series(1, 50) AS n;
UPDATE ingestion_scopes SET active_generation_id = 'scan-gen-' || substring(scope_id from 12)
WHERE scope_id LIKE 'scan-scope-%';
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'scan-analysis-' || n, 'scan-scope-' || n, 'scan-gen-' || n, 'scanner_worker.analysis', 'scan-analysis-key-' || n, 'scanner_worker', 'scan-analysis-' || n, clock_timestamp(), clock_timestamp(), FALSE,
  jsonb_build_object('analyzer', 'ospkg', 'target_kind', 'image', 'target_locator_hash', md5('scan-' || n), 'analysis_status', 'completed', 'coverage_status', 'supported',
    'result_count', 30, 'fact_count', 30, 'image_reference', 'registry.example.com/team/noise:' || n,
    'image_digest', 'sha256:' || md5('scan-image-' || n), 'evidence_source', 'scanner_worker', 'extraction_reason', 'scheduled_scan')
FROM generate_series(1, 50) AS n;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'scan-ospkg-' || n || '-' || p, 'scan-scope-' || n, 'scan-gen-' || n, 'vulnerability.os_package', 'scan-ospkg-key-' || n || '-' || p, 'scanner_worker', 'scan-ospkg-' || n || '-' || p, clock_timestamp(), clock_timestamp(), FALSE,
  jsonb_build_object('distro', 'debian', 'distro_version', '12', 'package_manager', 'dpkg', 'name', 'pkg-' || p, 'arch', 'amd64',
    'installed_version_raw', '1.0.' || p, 'repository_class', 'vendor', 'vendor_advisory_source', 'debian')
FROM generate_series(1, 50) AS n, generate_series(1, 30) AS p;
`); err != nil {
		t.Fatalf("seed scan-tier noise scopes: %v", err)
	}

	// The worst-case target scope: the exact digest this test queries by,
	// with 500 installed packages (maxSupplyChainImpactOSPackageAdvisoryTargets).
	// The parameterized INSERT (needs $1 for the target digest) is issued as
	// its own ExecContext call: pgx's extended protocol (used whenever
	// arguments are bound) rejects a multi-statement query text, unlike the
	// simple protocol used for the parameter-free statements above.
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ('scan-scope-target', 'container_image', 'scanner_worker', 'scan-target', 'scanner_worker', 'scan-target', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb);
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('scan-scope-target', 'scan-gen-target', 'sync', clock_timestamp(), clock_timestamp(), 'active');
UPDATE ingestion_scopes SET active_generation_id = 'scan-gen-target' WHERE scope_id = 'scan-scope-target';
`); err != nil {
		t.Fatalf("seed target scan scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('scan-analysis-target', 'scan-scope-target', 'scan-gen-target', 'scanner_worker.analysis', 'scan-analysis-target-key', 'scanner_worker', 'scan-analysis-target', clock_timestamp(), clock_timestamp(), FALSE,
  jsonb_build_object('analyzer', 'ospkg', 'target_kind', 'image', 'target_locator_hash', md5('scan-target'), 'analysis_status', 'completed', 'coverage_status', 'supported',
    'result_count', 500, 'fact_count', 500, 'image_reference', 'registry.example.com/team/api:prod',
    'image_digest', $1::text, 'evidence_source', 'scanner_worker', 'extraction_reason', 'scheduled_scan'))
`, targetDigest); err != nil {
		t.Fatalf("seed target scanner_worker.analysis: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'scan-ospkg-target-' || p, 'scan-scope-target', 'scan-gen-target', 'vulnerability.os_package', 'scan-ospkg-target-key-' || p, 'scanner_worker', 'scan-ospkg-target-' || p, clock_timestamp(), clock_timestamp(), FALSE,
  jsonb_build_object('distro', 'debian', 'distro_version', '12', 'package_manager', 'dpkg', 'name', 'pkg-target-' || p, 'arch', 'amd64',
    'installed_version_raw', '1.0.' || p, 'repository_class', 'vendor', 'vendor_advisory_source', 'debian')
FROM generate_series(1, 500) AS p;
ANALYZE fact_records;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;
`); err != nil {
		t.Fatalf("seed target os_package rows: %v", err)
	}

	return targetDigest
}
