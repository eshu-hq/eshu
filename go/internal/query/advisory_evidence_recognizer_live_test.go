// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// recognizerLiveKinds mirrors the six advisory fact kinds the shipped query
// probes.
var recognizerLiveKinds = []string{
	"vulnerability.cve",
	"vulnerability.affected_package",
	"vulnerability.affected_product",
	"vulnerability.epss_score",
	"vulnerability.known_exploited",
	"vulnerability.reference",
}

func openRecognizerLiveDB(t *testing.T) *sql.DB {
	t.Helper()

	const schema = "eshu_10410_advisory_evidence_recognizer_live"
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the #10410 recognizer live proof")
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

func seedRecognizerLiveFixtures(t *testing.T, ctx context.Context, db *sql.DB) {
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
    'rec:s1', 'advisory', 'nvd', 'rec:s1', 'vulnerability',
    'rec:s1', clock_timestamp(), clock_timestamp(), 'active', 'rec:g1', '{}'::jsonb
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at, payload
) VALUES (
    'rec:g1', 'rec:s1', 'test', clock_timestamp(), clock_timestamp(),
    'active', clock_timestamp(), '{}'::jsonb
);
`); err != nil {
		t.Fatalf("create recognizer proof tables: %v", err)
	}

	insertFact := func(factID, kind, payload string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, source_confidence,
    observed_at, ingested_at, is_tombstone, payload
) VALUES (
    $1, 'rec:s1', 'rec:g1', $2, $1,
    'test', $1, 'reported',
    TIMESTAMPTZ '2026-05-24 12:00:00+00', TIMESTAMPTZ '2026-05-24 12:00:00+00', FALSE, $3::jsonb
)`, factID, kind, payload); err != nil {
			t.Fatalf("seed fact %s: %v", factID, err)
		}
	}

	// CVE seed whose aliases name a PYSEC record: the 3.5 defect drops the
	// hop on origin/main.
	insertFact("rec-nvd-cve", "vulnerability.cve", `{
		"source": "nvd", "cve_id": "CVE-2026-5101", "advisory_id": "CVE-2026-5101",
		"aliases": ["PYSEC-2026-5101"]
	}`)
	insertFact("rec-pysec-cve", "vulnerability.cve", `{
		"source": "osv", "advisory_id": "PYSEC-2026-5101",
		"aliases": ["CVE-2026-5101"]
	}`)
	insertFact("rec-pysec-ap", "vulnerability.affected_package", `{
		"source": "osv", "advisory_id": "PYSEC-2026-5101",
		"package_id": "pkg:pypi/django", "purl": "pkg:pypi/django@4.2"
	}`)
	insertFact("rec-pysec-ref", "vulnerability.reference", `{
		"source": "osv", "advisory_id": "PYSEC-2026-5101",
		"reference_type": "ADVISORY", "url": "https://example.test/pysec-2026-5101"
	}`)
	// GHSA seed with the same PYSEC-alias shape.
	insertFact("rec-ghsa-cve", "vulnerability.cve", `{
		"source": "ghsa", "ghsa_id": "GHSA-aaaa-bbbb-cccc", "advisory_id": "GHSA-aaaa-bbbb-cccc",
		"aliases": ["PYSEC-2026-5102"]
	}`)
	insertFact("rec-pysec2-cve", "vulnerability.cve", `{
		"source": "osv", "advisory_id": "PYSEC-2026-5102",
		"aliases": ["GHSA-aaaa-bbbb-cccc"]
	}`)
	insertFact("rec-pysec2-ap", "vulnerability.affected_package", `{
		"source": "osv", "advisory_id": "PYSEC-2026-5102",
		"package_id": "pkg:pypi/flask"
	}`)
	// Distro id reached only via a CVE seed alias.
	insertFact("rec-nvd-distro", "vulnerability.cve", `{
		"source": "nvd", "cve_id": "CVE-2026-5103", "advisory_id": "CVE-2026-5103",
		"aliases": ["DEBIAN-CVE-2026-5103"]
	}`)
	insertFact("rec-debian-ap", "vulnerability.affected_package", `{
		"source": "debian", "advisory_id": "DEBIAN-CVE-2026-5103",
		"package_id": "pkg:deb/debian/glibc"
	}`)
	// Colon errata id reached only via a CVE seed alias.
	insertFact("rec-nvd-errata", "vulnerability.cve", `{
		"source": "nvd", "cve_id": "CVE-2026-5104", "advisory_id": "CVE-2026-5104",
		"aliases": ["ALSA-2023:5104"]
	}`)
	insertFact("rec-alsa-ap", "vulnerability.affected_package", `{
		"source": "almalinux", "advisory_id": "ALSA-2023:5104",
		"package_id": "pkg:rpm/almalinux/bash"
	}`)
	// Upstream-only id: the seed names it in upstream, never in aliases.
	insertFact("rec-nvd-upstream", "vulnerability.cve", `{
		"source": "nvd", "cve_id": "CVE-2026-5106", "advisory_id": "CVE-2026-5106",
		"upstream": ["MAL-2026-5106"]
	}`)
	insertFact("rec-mal-up", "vulnerability.affected_package", `{
		"source": "osv", "advisory_id": "MAL-2026-5106",
		"package_id": "pkg:npm/evil"
	}`)
	// Related-only id: the seed names it in related, never in aliases.
	insertFact("rec-nvd-related", "vulnerability.cve", `{
		"source": "nvd", "cve_id": "CVE-2026-5107", "advisory_id": "CVE-2026-5107",
		"related": ["MAL-2026-5107"]
	}`)
	insertFact("rec-mal-rel", "vulnerability.affected_package", `{
		"source": "osv", "advisory_id": "MAL-2026-5107",
		"package_id": "pkg:npm/evil-two"
	}`)
	// Two-hop alias chain: the seed aliases B, B aliases C. Only B's facts
	// may return.
	insertFact("rec-nvd-chain", "vulnerability.cve", `{
		"source": "nvd", "cve_id": "CVE-2026-5108", "advisory_id": "CVE-2026-5108",
		"aliases": ["PYSEC-2026-5108"]
	}`)
	insertFact("rec-pysec-chain-b", "vulnerability.affected_package", `{
		"source": "osv", "advisory_id": "PYSEC-2026-5108",
		"aliases": ["PYSEC-2026-5109"],
		"package_id": "pkg:pypi/requests"
	}`)
	insertFact("rec-pysec-chain-c", "vulnerability.affected_package", `{
		"source": "osv", "advisory_id": "PYSEC-2026-5109",
		"package_id": "pkg:pypi/urllib3"
	}`)

	// Noise so a fact_records sequence scan is never the cheapest plan.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT 'rec-noise-' || lpad(i::text, 6, '0'),
       'rec:s1', 'rec:g1', 'content_entity',
       'content_entity:rec-noise:' || i,
       'git', 'rec-noise-' || i,
       TIMESTAMPTZ '2026-05-24 12:00:00+00', TIMESTAMPTZ '2026-05-24 12:00:00+00',
       '{}'::jsonb
FROM generate_series(1, 20000) AS i;
`); err != nil {
		t.Fatalf("seed recognizer noise: %v", err)
	}

	// Shipped advisory-evidence lookup indexes from their migration DDL
	// (never a hand copy), matching the package-kind live proof.
	bootstrapSQL := storagepostgres.MigrationSQL("fact_records")
	for _, stmt := range strings.Split(bootstrapSQL, ";") {
		if !strings.Contains(stmt, "CREATE INDEX") {
			continue
		}
		if strings.Contains(stmt, "fact_records_vulnerability_active_") ||
			strings.Contains(stmt, "fact_records_vulnerability_affected_package_lookup_idx") ||
			strings.Contains(stmt, "fact_records_vulnerability_package_purl_lookup_idx") {
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
		t.Fatalf("analyze recognizer facts: %v", err)
	}
}

// queryRecognizerLiveIDs runs the shipped evidence SQL with explicit lookup
// ids and returns the advisory ids of the returned facts.
func queryRecognizerLiveIDs(t *testing.T, ctx context.Context, db *sql.DB, lookupIDs []string) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, listAdvisoryEvidenceQuery,
		array.Of(recognizerLiveKinds),
		array.Of(lookupIDs),
		array.Of([]string{}),
		"",
		advisory.EvidenceMaxFactRows,
		"", "", "",
		array.Of([]string{}),
	)
	if err != nil {
		t.Fatalf("list advisory evidence for %v: %v", lookupIDs, err)
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
		var decoded map[string]any
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("decode advisory evidence payload: %v", err)
		}
		advisoryID, _ := decoded["advisory_id"].(string)
		got[factID] = advisoryID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate advisory evidence rows: %v", err)
	}
	return got
}

// TestAdvisoryEvidenceRecognizerAliasHopLive is the RED-first regression for
// eshu-hq/eshu#10410: seeding by CVE or GHSA must return the facts of a
// record reachable only via aliases, for PYSEC, distro, and colon-errata
// ids alike.
func TestAdvisoryEvidenceRecognizerAliasHopLive(t *testing.T) {
	db := openRecognizerLiveDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	seedRecognizerLiveFixtures(t, ctx, db)

	t.Run("CVE seed pulls PYSEC package and reference facts", func(t *testing.T) {
		got := queryRecognizerLiveIDs(t, ctx, db, []string{"CVE-2026-5101"})
		for _, want := range []string{"rec-pysec-ap", "rec-pysec-ref"} {
			advisoryID, ok := got[want]
			if !ok {
				t.Fatalf("CVE seed missing PYSEC fact %s, got %v", want, got)
			}
			if advisoryID != "PYSEC-2026-5101" {
				t.Fatalf("fact %s advisory_id = %q, want PYSEC-2026-5101", want, advisoryID)
			}
		}
	})

	t.Run("GHSA seed pulls PYSEC package facts", func(t *testing.T) {
		got := queryRecognizerLiveIDs(t, ctx, db, []string{"GHSA-aaaa-bbbb-cccc"})
		if advisoryID, ok := got["rec-pysec2-ap"]; !ok {
			t.Fatalf("GHSA seed missing PYSEC fact rec-pysec2-ap, got %v", got)
		} else if advisoryID != "PYSEC-2026-5102" {
			t.Fatalf("rec-pysec2-ap advisory_id = %q, want PYSEC-2026-5102", advisoryID)
		}
	})

	t.Run("CVE seed pulls distro package facts", func(t *testing.T) {
		got := queryRecognizerLiveIDs(t, ctx, db, []string{"CVE-2026-5103"})
		if advisoryID, ok := got["rec-debian-ap"]; !ok {
			t.Fatalf("CVE seed missing distro fact rec-debian-ap, got %v", got)
		} else if advisoryID != "DEBIAN-CVE-2026-5103" {
			t.Fatalf("rec-debian-ap advisory_id = %q, want DEBIAN-CVE-2026-5103", advisoryID)
		}
	})

	t.Run("CVE seed pulls colon errata package facts", func(t *testing.T) {
		got := queryRecognizerLiveIDs(t, ctx, db, []string{"CVE-2026-5104"})
		if advisoryID, ok := got["rec-alsa-ap"]; !ok {
			t.Fatalf("CVE seed missing errata fact rec-alsa-ap, got %v", got)
		} else if advisoryID != "ALSA-2023:5104" {
			t.Fatalf("rec-alsa-ap advisory_id = %q, want ALSA-2023:5104", advisoryID)
		}
	})

	t.Run("direct PYSEC lookup still resolves", func(t *testing.T) {
		got := queryRecognizerLiveIDs(t, ctx, db, []string{"PYSEC-2026-5101"})
		if _, ok := got["rec-pysec-ap"]; !ok {
			t.Fatalf("direct PYSEC lookup missing rec-pysec-ap, got %v", got)
		}
	})
}

// TestAdvisoryEvidenceRecognizerNoSemanticWideningLive pins the ruling's
// negative space: upstream-only, related-only, and two-hop ids stay out.
func TestAdvisoryEvidenceRecognizerNoSemanticWideningLive(t *testing.T) {
	db := openRecognizerLiveDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	seedRecognizerLiveFixtures(t, ctx, db)

	t.Run("upstream-only id is not pulled", func(t *testing.T) {
		got := queryRecognizerLiveIDs(t, ctx, db, []string{"CVE-2026-5106"})
		if _, ok := got["rec-mal-up"]; ok {
			t.Fatalf("upstream-only fact rec-mal-up pulled, got %v", got)
		}
		if _, ok := got["rec-nvd-upstream"]; !ok {
			t.Fatalf("seed fact rec-nvd-upstream missing, got %v", got)
		}
	})

	t.Run("related-only id is not pulled", func(t *testing.T) {
		got := queryRecognizerLiveIDs(t, ctx, db, []string{"CVE-2026-5107"})
		if _, ok := got["rec-mal-rel"]; ok {
			t.Fatalf("related-only fact rec-mal-rel pulled, got %v", got)
		}
	})

	t.Run("two-hop alias is not pulled", func(t *testing.T) {
		got := queryRecognizerLiveIDs(t, ctx, db, []string{"CVE-2026-5108"})
		if _, ok := got["rec-pysec-chain-b"]; !ok {
			t.Fatalf("one-hop fact rec-pysec-chain-b missing, got %v", got)
		}
		if _, ok := got["rec-pysec-chain-c"]; ok {
			t.Fatalf("two-hop fact rec-pysec-chain-c pulled, got %v", got)
		}
	})
}
