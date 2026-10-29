// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// TestAdvisoryEvidencePackageLegsCoverNonAffectedPackageKindsLive is the
// regression proof for eshu-hq/eshu#9474 P1: the package legs of
// ListEvidenceQuery were pinned to
// fact_kind = 'vulnerability.affected_package', silently dropping
// package-matching rows of other kinds. GitLab (GLAD) CVE envelopes carry
// package_id (gitlab_gemnasium_envelope.go newGitLabCVEEnvelope), so a
// package-scoped read must return those CVE rows even when no
// affected_package sibling exists.
//
// The test runs the shipped SQL text against a hermetic seeded schema, so it
// fails on the statement the P1 names, not on a copy of it.
func TestAdvisoryEvidencePackageLegsCoverNonAffectedPackageKindsLive(t *testing.T) {
	db := openAdvisoryPackageKindLiveDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()

	seedAdvisoryPackageKindFixtures(t, ctx, db)

	packageKinds := []string{
		"vulnerability.cve",
		"vulnerability.affected_package",
		"vulnerability.affected_product",
		"vulnerability.epss_score",
		"vulnerability.known_exploited",
		"vulnerability.reference",
	}

	queryFactIDs := func(pkg string) map[string]string {
		t.Helper()
		rows, err := db.QueryContext(ctx, listAdvisoryEvidenceQuery,
			array.Of(packageKinds),
			array.Of([]string{}),
			array.Of([]string{pkg}),
			"",
			advisory.EvidenceMaxFactRows,
			"", "", "",
			array.Of([]string{}),
		)
		if err != nil {
			t.Fatalf("list advisory evidence for package %q: %v", pkg, err)
		}
		defer func() { _ = rows.Close() }()
		got := map[string]string{}
		for rows.Next() {
			var factID, factKind, sourceConfidence, schemaVersion string
			var observedAt sql.NullTime
			var payload []byte
			if err := rows.Scan(&factID, &factKind, &sourceConfidence, &observedAt, &schemaVersion, &payload); err != nil {
				t.Fatalf("scan advisory evidence row: %v", err)
			}
			got[factID] = factKind
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate advisory evidence rows: %v", err)
		}
		return got
	}

	t.Run("cve with package_id and no affected_package sibling", func(t *testing.T) {
		got := queryFactIDs("pkg:npm/leftpad")
		if kind, ok := got["pkgkind-cve-only"]; !ok {
			t.Fatalf("package query missing cve-with-package_id row, got %v", got)
		} else if kind != "vulnerability.cve" {
			t.Fatalf("pkgkind-cve-only kind = %q, want vulnerability.cve", kind)
		}
	})

	t.Run("tombstoned affected_package sibling does not suppress the cve", func(t *testing.T) {
		got := queryFactIDs("pkg:npm/tomb")
		if kind, ok := got["pkgkind-cve-tomb-sib"]; !ok {
			t.Fatalf("package query missing cve row with tombstoned sibling, got %v", got)
		} else if kind != "vulnerability.cve" {
			t.Fatalf("pkgkind-cve-tomb-sib kind = %q, want vulnerability.cve", kind)
		}
		if kind, ok := got["pkgkind-ap-tombstone"]; ok {
			t.Fatalf("package query returned tombstoned affected_package row (kind %q)", kind)
		}
	})

	t.Run("non-affected_package row carrying purl", func(t *testing.T) {
		got := queryFactIDs("pkg:npm/leftpad@1.0.0")
		if kind, ok := got["pkgkind-ref-purl"]; !ok {
			t.Fatalf("package query missing reference row carrying purl, got %v", got)
		} else if kind != "vulnerability.reference" {
			t.Fatalf("pkgkind-ref-purl kind = %q, want vulnerability.reference", kind)
		}
	})
}

// cteHeader matches an EXPLAIN CTE section header ("CTE seed_candidates")
// without matching "CTE Scan on ..." probe lines.
var cteHeader = regexp.MustCompile(`^\s*CTE ([a-z_]+)\s*$`)

// TestAdvisoryEvidencePackageLegsProbeFiveKindIndexesLive pins the planner
// side of the #9474 verdict: every package leg must probe its five-kind
// partial index, never fall back to a fact_records sequence scan.
func TestAdvisoryEvidencePackageLegsProbeFiveKindIndexesLive(t *testing.T) {
	db := openAdvisoryPackageKindLiveDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()

	seedAdvisoryPackageKindFixtures(t, ctx, db)

	kinds := []string{
		"vulnerability.cve",
		"vulnerability.affected_package",
		"vulnerability.affected_product",
		"vulnerability.epss_score",
		"vulnerability.known_exploited",
		"vulnerability.reference",
	}

	for _, pkg := range []string{"pkg:npm/leftpad", "pkg:npm/leftpad@1.0.0"} {
		rows, err := db.QueryContext(ctx, "EXPLAIN "+listAdvisoryEvidenceQuery,
			array.Of(kinds),
			array.Of([]string{}),
			array.Of([]string{pkg}),
			"",
			advisory.EvidenceMaxFactRows,
			"", "", "",
			array.Of([]string{}),
		)
		if err != nil {
			t.Fatalf("explain advisory evidence for package %q: %v", pkg, err)
		}
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				_ = rows.Close()
				t.Fatalf("scan explain row: %v", err)
			}
			lines = append(lines, line)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate explain rows: %v", err)
		}
		plan := strings.Join(lines, "\n")
		// Scope the scan assertion to the seed/matched CTEs: the
		// impact_candidates CTE legitimately walks fact_records in a
		// schema without the impact read's own indexes, and it is
		// unrelated to the #9474 package legs.
		sections := map[string][]string{}
		current := ""
		for _, line := range lines {
			if m := cteHeader.FindStringSubmatch(line); m != nil {
				current = m[1]
				continue
			}
			if current != "" {
				sections[current] = append(sections[current], line)
			}
		}
		for _, section := range []string{"seed_candidates", "matched_candidates"} {
			body := strings.Join(sections[section], "\n")
			if body == "" {
				t.Fatalf("package query plan for %q is missing CTE %s:\n%s", pkg, section, plan)
			}
			if strings.Contains(body, "Seq Scan on fact_records") {
				t.Fatalf("package query for %q seq-scans fact_records in CTE %s:\n%s", pkg, section, plan)
			}
		}
		for _, want := range []string{
			"fact_records_vulnerability_active_package_lookup_v2_idx",
			"fact_records_vulnerability_active_purl_lookup_v2_idx",
		} {
			if !strings.Contains(plan, want) {
				t.Fatalf("package query for %q does not probe %s:\n%s", pkg, want, plan)
			}
		}
	}
}

func openAdvisoryPackageKindLiveDB(t *testing.T) *sql.DB {
	t.Helper()

	const schema = "eshu_9474_advisory_evidence_package_kind_live"
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the #9474 package-kind live proof")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	if _, err := adminDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE; CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("drop isolated proof schema: %v", err)
		}
	})

	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	params := parsedDSN.Query()
	params.Set("search_path", schema)
	parsedDSN.RawQuery = params.Encode()
	db, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedAdvisoryPackageKindFixtures(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    scope_kind TEXT NOT NULL,
    source_system TEXT NOT NULL,
    source_key TEXT NOT NULL,
    collector_kind TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    active_generation_id TEXT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL,
    trigger_kind TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fact_kind TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    schema_version TEXT NOT NULL DEFAULT '0.0.0',
    collector_kind TEXT NOT NULL DEFAULT 'unknown',
    fencing_token BIGINT NOT NULL DEFAULT 0,
    source_confidence TEXT NOT NULL DEFAULT 'unknown',
    source_system TEXT NOT NULL,
    source_fact_key TEXT NOT NULL,
    source_uri TEXT NULL,
    source_record_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id, payload
) VALUES (
    'pkgkind:s1', 'advisory', 'glad', 'pkgkind:s1', 'vulnerability',
    'pkgkind:s1', clock_timestamp(), clock_timestamp(), 'active', 'pkgkind:g1', '{}'::jsonb
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at, payload
) VALUES (
    'pkgkind:g1', 'pkgkind:s1', 'test', clock_timestamp(), clock_timestamp(),
    'active', clock_timestamp(), '{}'::jsonb
);
`); err != nil {
		t.Fatalf("create package-kind proof tables: %v", err)
	}

	insertFact := func(factID, kind, payload string, tombstone bool) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, source_confidence,
    observed_at, ingested_at, is_tombstone, payload
) VALUES (
    $1, 'pkgkind:s1', 'pkgkind:g1', $2, $1,
    'glad', $1, 'reported',
    TIMESTAMPTZ '2026-05-24 12:00:00+00', TIMESTAMPTZ '2026-05-24 12:00:00+00', $4, $3::jsonb
)`, factID, kind, payload, tombstone); err != nil {
			t.Fatalf("seed fact %s: %v", factID, err)
		}
	}

	// GLAD-shaped CVE row carrying package_id, with no affected_package
	// sibling: the exact shape #9474 P1 says the pinned legs drop.
	insertFact("pkgkind-cve-only", "vulnerability.cve", `{
		"source": "glad",
		"advisory_id": "GLAD-PKG-1",
		"cve_id": "CVE-2026-9001",
		"package_id": "pkg:npm/leftpad",
		"title": "leftpad rejoices"
	}`, false)

	// CVE with package_id plus a tombstoned affected_package sibling: the
	// live row must survive while the tombstone stays excluded.
	insertFact("pkgkind-cve-tomb-sib", "vulnerability.cve", `{
		"source": "glad",
		"advisory_id": "GLAD-PKG-2",
		"cve_id": "CVE-2026-9002",
		"package_id": "pkg:npm/tomb",
		"title": "tomb rejoices"
	}`, false)
	insertFact("pkgkind-ap-tombstone", "vulnerability.affected_package", `{
		"source": "glad",
		"advisory_id": "GLAD-PKG-2",
		"cve_id": "CVE-2026-9002",
		"package_id": "pkg:npm/tomb",
		"purl": "pkg:npm/tomb@1.0.0"
	}`, true)

	// Synthetic non-affected_package row carrying purl: proves the purl
	// five-kind slice reaches beyond affected_package.
	insertFact("pkgkind-ref-purl", "vulnerability.reference", `{
		"source": "glad",
		"advisory_id": "GLAD-PKG-3",
		"cve_id": "CVE-2026-9003",
		"purl": "pkg:npm/leftpad@1.0.0",
		"reference_type": "ADVISORY",
		"url": "https://example.test/advisories/GLAD-PKG-3"
	}`, false)

	// Noise so a fact_records sequence scan is never the cheapest plan.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT 'pkgkind-noise-' || lpad(i::text, 6, '0'),
       'pkgkind:s1', 'pkgkind:g1', 'content_entity',
       'content_entity:pkgkind-noise:' || i,
       'git', 'pkgkind-noise-' || i,
       TIMESTAMPTZ '2026-05-24 12:00:00+00', TIMESTAMPTZ '2026-05-24 12:00:00+00',
       '{}'::jsonb
FROM generate_series(1, 20000) AS i;
`); err != nil {
		t.Fatalf("seed package-kind noise: %v", err)
	}

	// Build the shipped advisory-evidence lookup indexes from their
	// migration DDL (never a hand copy), so the plan proof below runs
	// against the exact statements deployments apply: the three identity
	// v2 indexes ship in the fact_records base migration, and the two
	// five-kind indexes ship in #9474 migrations 240-241.
	bootstrapSQL := storagepostgres.MigrationSQL("fact_records")
	for _, stmt := range strings.Split(bootstrapSQL, ";") {
		if strings.Contains(stmt, "CREATE INDEX") && strings.Contains(stmt, "fact_records_vulnerability_active_") {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("apply shipped advisory lookup index: %v", err)
			}
		}
	}
	for _, name := range []string{
		"fact_records_vulnerability_active_package_lookup_v2_idx",
		"fact_records_vulnerability_active_purl_lookup_v2_idx",
	} {
		if _, err := db.ExecContext(ctx, storagepostgres.MigrationSQL(name)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
	if _, err := db.ExecContext(ctx, `ANALYZE fact_records;`); err != nil {
		t.Fatalf("analyze package-kind facts: %v", err)
	}
}
