// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10411 disposable PostgreSQL proof that the advisory-group winners reader returns members plus purl from the winner fact for qualifying rows and excludes every non-qualifying row (status, effective suppression, expiry, repository, derived scope, scan association, other group); seeded rows carry a unique prefix with cleanup on both paths; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// advisoryGroupWinnersLivePrefix namespaces every seeded row this proof owns,
// so cleanup deletes exactly what the seed inserted on the shared test
// database. It mirrors the finding-CVE live proof: direct DSN read with a
// skip, no build tag, no shared-suite mutation.
const advisoryGroupWinnersLivePrefix = "test-agw-10411-"

const (
	advisoryGroupWinnersLiveGroupA = "CVE-10411-AAAA"
	advisoryGroupWinnersLiveGroupB = "GHSA-10411-bbbb"
	advisoryGroupWinnersLiveOther  = "CVE-10411-OTHER"
)

// TestAdvisoryGroupWinnersQualifyingLive proves the winners-reader lane over
// PostgreSQL: a qualifying row returns members plus purl from its winner
// fact, and every exclusion (status, suppression, expiry, repository, derived
// scope, scan association, group) holds.
func TestAdvisoryGroupWinnersQualifyingLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the advisory-group winners proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	seedAdvisoryGroupWinnersLive(t, ctx, db)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupAdvisoryGroupWinnersLive(t, cleanupCtx, db)
	})

	store := NewFactStore(SQLDB{DB: db})
	got, err := store.QualifyingAdvisoryGroupWinners(ctx, advisoryGroupWinnersLiveGroupA)
	if err != nil {
		t.Fatalf("QualifyingAdvisoryGroupWinners() error = %v", err)
	}
	byRepo := map[string]advisorygroup.WinnerRow{}
	for _, row := range got {
		byRepo[row.RepositoryID] = row
	}
	// Qualifying CVE-anchored row with members and purl from its winner fact.
	qual, ok := byRepo["repo-agw-a"]
	if !ok {
		t.Fatalf("QualifyingAdvisoryGroupWinners() missing repo-agw-a in %#v", got)
	}
	if qual.PackageID != "pkg-agw-a" || qual.Ecosystem != "npm" {
		t.Fatalf("repo-agw-a row = %+v, want package pkg-agw-a ecosystem npm", qual)
	}
	if qual.PURL != "pkg:npm/pkg-agw-a@1.2.3" {
		t.Fatalf("repo-agw-a purl = %q, want the winner fact purl", qual.PURL)
	}
	if len(qual.Members) != 2 || qual.Members[0] != "GHSA-10411-m1" || qual.Members[1] != "GHSA-10411-m2" {
		t.Fatalf("repo-agw-a members = %q, want the winner fact advisory_group_members", qual.Members)
	}
	if qual.CVEID != advisoryGroupWinnersLiveGroupA {
		t.Fatalf("repo-agw-a cve = %q, want %q", qual.CVEID, advisoryGroupWinnersLiveGroupA)
	}
	// Expired suppression stays visible.
	if _, ok := byRepo["repo-agw-expired"]; !ok {
		t.Fatalf("QualifyingAdvisoryGroupWinners() must include the expired-suppression row in %#v", got)
	}
	// Visible triage states stay visible.
	if _, ok := byRepo["repo-agw-dismissed"]; !ok {
		t.Fatalf("QualifyingAdvisoryGroupWinners() must include the provider_dismissed row in %#v", got)
	}
	if _, ok := byRepo["repo-agw-mismatch"]; !ok {
		t.Fatalf("QualifyingAdvisoryGroupWinners() must include the scope_mismatch row in %#v", got)
	}
	for _, excluded := range []string{
		"repo-agw-fixed", "repo-agw-unknown", "repo-agw-hidden",
		"repo-agw-retracted", "repo-agw-future", "repo-agw-derived",
		"repo-agw-scan", "repo-agw-other",
	} {
		if row, ok := byRepo[excluded]; ok {
			t.Fatalf("QualifyingAdvisoryGroupWinners() must exclude %+v", row)
		}
	}
	if _, ok := byRepo[""]; ok {
		t.Fatalf("QualifyingAdvisoryGroupWinners() must exclude blank repository rows in %#v", got)
	}
	// Deterministic order: repository_id, package_id.
	for i := 1; i < len(got); i++ {
		prev, cur := got[i-1], got[i]
		if prev.RepositoryID > cur.RepositoryID ||
			(prev.RepositoryID == cur.RepositoryID && prev.PackageID > cur.PackageID) {
			t.Fatalf("QualifyingAdvisoryGroupWinners() out of order: %+v before %+v", prev, cur)
		}
	}

	// CVE-less group matches on advisory_id.
	cveless, err := store.QualifyingAdvisoryGroupWinners(ctx, advisoryGroupWinnersLiveGroupB)
	if err != nil {
		t.Fatalf("QualifyingAdvisoryGroupWinners(CVE-less) error = %v", err)
	}
	if len(cveless) != 1 || cveless[0].RepositoryID != "repo-agw-b" {
		t.Fatalf("CVE-less group rows = %+v, want exactly repo-agw-b", cveless)
	}
	if cveless[0].Members != nil {
		t.Fatalf("CVE-less group members = %q, want nil when no winner fact exists", cveless[0].Members)
	}
	if cveless[0].PURL != "" {
		t.Fatalf("CVE-less group purl = %q, want empty when no winner fact exists", cveless[0].PURL)
	}

	// Unknown group matches nothing.
	none, err := store.QualifyingAdvisoryGroupWinners(ctx, "CVE-10411-NOMATCH")
	if err != nil {
		t.Fatalf("QualifyingAdvisoryGroupWinners(unknown) error = %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("unknown group rows = %+v, want none", none)
	}

	// The watermark reads the maintainer singleton without mutating it.
	watermark, err := store.AdvisoryGroupWinnersWatermark(ctx)
	if err != nil {
		t.Fatalf("AdvisoryGroupWinnersWatermark() error = %v", err)
	}
	var direct time.Time
	if err := db.QueryRowContext(ctx,
		`SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1`,
	).Scan(&direct); err != nil && err != sql.ErrNoRows {
		t.Fatalf("read watermark singleton: %v", err)
	}
	if err == sql.ErrNoRows {
		if !watermark.IsZero() {
			t.Fatalf("AdvisoryGroupWinnersWatermark() = %v, want zero with no watermark row", watermark)
		}
	} else if !watermark.Equal(direct) {
		t.Fatalf("AdvisoryGroupWinnersWatermark() = %v, want singleton %v", watermark, direct)
	}
}

func seedAdvisoryGroupWinnersLive(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	p := advisoryGroupWinnersLivePrefix

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic',
          $1, NOW(), NOW(), 'active', '{}'::jsonb)`,
		p+"scope",
	); err != nil {
		t.Fatalf("insert source scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES ($1, $2, 'synthetic', NOW(), NOW(), 'active', NOW(), '{}'::jsonb)`,
		p+"gen", p+"scope",
	); err != nil {
		t.Fatalf("insert source generation: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		p+"gen", p+"scope",
	); err != nil {
		t.Fatalf("activate source generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES ($1, $2, $3, 'reducer_supply_chain_impact_finding', $1,
          'synthetic', $1, NOW(), NOW(), $4::jsonb)`,
		p+"fact-members", p+"scope", p+"gen",
		`{"purl": "pkg:npm/pkg-agw-a@1.2.3", "advisory_group_members": ["GHSA-10411-m1", "GHSA-10411-m2"]}`,
	); err != nil {
		t.Fatalf("insert winner fact: %v", err)
	}

	type winner struct {
		key, fact, scope, status, repo, pkg, eco, cve, advisory, suppression string
		expires                                                              *time.Time
	}
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)
	seed := []winner{
		{"a", p + "fact-members", "static-scope", "affected_exact", "repo-agw-a", "pkg-agw-a", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "active", nil},
		{"b", p + "missing-fact", "static-scope", "affected_derived", "repo-agw-b", "pkg-agw-b", "npm", "", advisoryGroupWinnersLiveGroupB, "active", nil},
		{"fixed", p + "missing-fact", "static-scope", "not_affected_known_fixed", "repo-agw-fixed", "pkg-agw-fixed", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "active", nil},
		{"unknown", p + "missing-fact", "static-scope", "unknown_impact", "repo-agw-unknown", "pkg-agw-unknown", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "active", nil},
		{"hidden", p + "missing-fact", "static-scope", "possibly_affected", "repo-agw-hidden", "pkg-agw-hidden", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "not_affected", nil},
		{"retracted", p + "missing-fact", "static-scope", "possibly_affected", "repo-agw-retracted", "pkg-agw-retracted", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "advisory_retracted", nil},
		{"expired", p + "missing-fact", "static-scope", "affected_exact", "repo-agw-expired", "pkg-agw-expired", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "ignored", &past},
		{"future", p + "missing-fact", "static-scope", "affected_exact", "repo-agw-future", "pkg-agw-future", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "ignored", &future},
		{"blank", p + "missing-fact", "static-scope", "affected_exact", "", "pkg-agw-blank", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "active", nil},
		{"derived", p + "missing-fact", "image://derived/abc", "affected_exact", "repo-agw-derived", "pkg-agw-derived", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "active", nil},
		{"scan", p + "fact-scan", "static-scope", "affected_exact", "repo-agw-scan", "pkg-agw-scan", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "active", nil},
		{"other", p + "missing-fact", "static-scope", "affected_exact", "repo-agw-other", "pkg-agw-other", "npm", advisoryGroupWinnersLiveOther, "GHSA-10411-O", "active", nil},
		{"dismissed", p + "missing-fact", "static-scope", "affected_exact", "repo-agw-dismissed", "pkg-agw-dismissed", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "provider_dismissed", nil},
		{"mismatch", p + "missing-fact", "static-scope", "affected_exact", "repo-agw-mismatch", "pkg-agw-mismatch", "npm", advisoryGroupWinnersLiveGroupA, "GHSA-10411-A", "scope_mismatch", nil},
	}
	for _, row := range seed {
		if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_canonical_winners
    (canonical_key, winner_fact_id, winner_scope_id, finding_id, impact_status,
     repository_id, cve_id, advisory_id, package_id, ecosystem,
     suppression_state, suppression_expires_at, materialized_at)
    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())`,
			p+"winner-"+row.key, row.fact, row.scope, p+"finding-"+row.key,
			row.status, row.repo, row.cve, row.advisory, row.pkg, row.eco,
			row.suppression, row.expires,
		); err != nil {
			t.Fatalf("seed winner %q: %v", row.key, err)
		}
	}
	seedAdvisoryGroupWinnerScanAssociationLive(t, ctx, db)
}

// seedAdvisoryGroupWinnerScanAssociationLive attaches the scan winner to a
// minimal scanner publication chain so the static-winner NOT EXISTS exclusion
// has a real association to act on.
func seedAdvisoryGroupWinnerScanAssociationLive(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	p := advisoryGroupWinnersLivePrefix

	if _, err := db.ExecContext(ctx, `
INSERT INTO scanner_image_targets (
  scan_scope_id, authorization_namespace, authorization_domain, manifest_digest
) VALUES ($1, 'local', 'example.test', $1)`, p+"scan-scope"); err != nil {
		t.Fatalf("insert scan target: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scanner_image_target_sources (
  binding_id, scan_scope_id, source_fact_id, source_collector_instance_id,
  source_scope_id, source_generation_id, source_payload_sha256,
  scanner_collector_instance_id, registry, repository, runtime_digest,
  runtime_digest_kind, runtime_image_reference, registry_domain_config_sha256,
  validated_at
) VALUES ($1, $2, $3, 'collector', 'scope', 'gen', 'sha256', 'scanner',
          'example.test', 'repo', 'digest', 'sha256', 'ref', 'sha256', NOW())`,
		p+"binding", p+"scan-scope", p+"fact-scan"); err != nil {
		t.Fatalf("insert scan source: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scanner_image_target_attempts (
  work_item_id, scan_scope_id, binding_id, status, created_at
) VALUES ($1, $2, $3, 'completed', NOW())`,
		p+"work-item", p+"scan-scope", p+"binding"); err != nil {
		t.Fatalf("insert scan attempt: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scanner_image_result_publications (
  publication_id, scan_scope_id, scan_generation_id, binding_id, work_item_id,
  impact_scope_id, impact_generation_id, impact_fencing_token,
  result_set_sha256, advisory_set_sha256, analysis_version,
  role_policy_revision, database_identity, status, published_at
) VALUES ($1, $2, 'gen', $3, $4, 'impact-scope', 'impact-gen', 1,
          'results', 'advisories', 'v1', 1, gen_random_uuid(), 'current', NOW())`,
		p+"publication", p+"scan-scope", p+"binding", p+"work-item"); err != nil {
		t.Fatalf("insert scan publication: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scanner_image_result_fact_associations (
  finding_fact_id, publication_id, scan_scope_id, scan_generation_id,
  tenant_id, workspace_id
) VALUES ($1, $2, $3, 'gen', '', '')`,
		p+"fact-scan", p+"publication", p+"scan-scope"); err != nil {
		t.Fatalf("insert scan association: %v", err)
	}
}

func cleanupAdvisoryGroupWinnersLive(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	p := advisoryGroupWinnersLivePrefix

	queries := []string{
		`DELETE FROM scanner_image_result_fact_associations WHERE finding_fact_id LIKE '` + p + `%'`,
		`DELETE FROM scanner_image_result_publications WHERE publication_id LIKE '` + p + `%'`,
		`DELETE FROM scanner_image_target_attempts WHERE work_item_id LIKE '` + p + `%'`,
		`DELETE FROM scanner_image_target_sources WHERE binding_id LIKE '` + p + `%'`,
		`DELETE FROM scanner_image_targets WHERE scan_scope_id LIKE '` + p + `%'`,
		`DELETE FROM supply_chain_impact_canonical_winners WHERE canonical_key LIKE '` + p + `%'`,
		`DELETE FROM fact_records WHERE fact_id LIKE '` + p + `%'`,
		`DELETE FROM scope_generations WHERE generation_id LIKE '` + p + `%'`,
		`DELETE FROM ingestion_scopes WHERE scope_id LIKE '` + p + `%'`,
	}
	for _, query := range queries {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("cleanup advisory-group winners (%s): %v", query, err)
		}
	}
}
