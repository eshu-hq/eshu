// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// impactRepoAdvisoryKinds mirrors the six advisory fact kinds the shipped
// query probes (same convention as the package-kind live proof, which
// hardcodes the slice at the call site).
var impactRepoAdvisoryKinds = []string{
	"vulnerability.cve",
	"vulnerability.affected_package",
	"vulnerability.affected_product",
	"vulnerability.epss_score",
	"vulnerability.known_exploited",
	"vulnerability.reference",
}

// impactRepoFixture seeds one scope per shape the repository-first probe
// must distinguish: active, stale generation, tombstone, other repository,
// and workload-anchored findings, plus the advisory facts the seed legs
// resolve through the impact-derived lookup ids.
func seedAdvisoryImpactRepoFixtures(t *testing.T, ctx context.Context, db *sql.DB) {
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
) VALUES
    ('repo:s1', 'sbom', 'test', 'repo:s1', 'sbom', 'repo:s1', clock_timestamp(), clock_timestamp(), 'active', 'repo:g1', '{}'::jsonb),
    ('repo:s-old', 'sbom', 'test', 'repo:s-old', 'sbom', 'repo:s-old', clock_timestamp(), clock_timestamp(), 'active', 'repo:g2', '{}'::jsonb),
    ('repo:s-other', 'sbom', 'test', 'repo:s-other', 'sbom', 'repo:s-other', clock_timestamp(), clock_timestamp(), 'active', 'repo:g9', '{}'::jsonb);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at, payload
) VALUES
    ('repo:g1', 'repo:s1', 'test', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp(), '{}'::jsonb),
    ('repo:g1-old', 'repo:s-old', 'test', clock_timestamp(), clock_timestamp(), 'superseded', clock_timestamp(), '{}'::jsonb),
    ('repo:g2', 'repo:s-old', 'test', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp(), '{}'::jsonb),
    ('repo:g9', 'repo:s-other', 'test', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp(), '{}'::jsonb);
`); err != nil {
		t.Fatalf("create impact repo proof tables: %v", err)
	}

	insertFact := func(factID, scopeID, generationID, kind, payload string, tombstone bool) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, source_confidence,
    observed_at, ingested_at, is_tombstone, payload
) VALUES (
    $1, $2, $3, $4, $1,
    'test', $1, 'reported',
    TIMESTAMPTZ '2026-05-24 12:00:00+00', TIMESTAMPTZ '2026-05-24 12:00:00+00', $6, $5::jsonb
)`, factID, scopeID, generationID, kind, payload, tombstone); err != nil {
			t.Fatalf("seed fact %s: %v", factID, err)
		}
	}

	// Live impact finding in the active generation: the only row the
	// repository-A scope must resolve.
	insertFact("repo-f1", "repo:s1", "repo:g1", "reducer_supply_chain_impact_finding", `{
		"cve_id": "CVE-2026-9101",
		"repository_id": "repository:repo-A",
		"service_ids": ["service:svc-X"]
	}`, false)
	// Stale-generation sibling with a distinct CVE: a wrong scope join
	// (e.g. ON TRUE) leaks it into the repository-A scope.
	insertFact("repo-f3-stale", "repo:s-old", "repo:g1-old", "reducer_supply_chain_impact_finding", `{
		"cve_id": "CVE-2026-9103",
		"repository_id": "repository:repo-A"
	}`, false)
	// Tombstoned sibling: excluded by the tombstone predicate on every shape.
	insertFact("repo-f4-tomb", "repo:s1", "repo:g1", "reducer_supply_chain_impact_finding", `{
		"cve_id": "CVE-2026-9104",
		"repository_id": "repository:repo-A"
	}`, true)
	// Other-repository control: never in the repository-A scope.
	insertFact("repo-f5-other", "repo:s-other", "repo:g9", "reducer_supply_chain_impact_finding", `{
		"cve_id": "CVE-2026-9105",
		"repository_id": "repository:repo-B"
	}`, false)
	// Workload-anchored finding for the workload-only shape.
	insertFact("repo-f6-workload", "repo:s1", "repo:g1", "reducer_supply_chain_impact_finding", `{
		"cve_id": "CVE-2026-9106",
		"repository_id": "repository:repo-A",
		"workload_ids": ["workload:repo-W"]
	}`, false)

	// Advisory facts the seed legs resolve through impact-derived ids.
	for _, seed := range []struct{ id, cve string }{
		{"repo-adv-9101", "CVE-2026-9101"},
		{"repo-adv-9103", "CVE-2026-9103"},
		{"repo-adv-9104", "CVE-2026-9104"},
		{"repo-adv-9106", "CVE-2026-9106"},
	} {
		insertFact(seed.id, "repo:s1", "repo:g1", "vulnerability.cve", `{
			"source": "test",
			"advisory_id": "TEST-`+seed.cve+`",
			"cve_id": "`+seed.cve+`"
		}`, false)
	}

	// The shipped repository-key index, built from migration DDL (never a
	// hand copy) so the probe path matches deployments.
	bootstrapSQL := storagepostgres.MigrationSQL("fact_records")
	for _, stmt := range strings.Split(bootstrapSQL, ";") {
		if !strings.Contains(stmt, "CREATE INDEX") {
			continue
		}
		if strings.Contains(stmt, "fact_records_supply_chain_impact_repository_lookup_idx") {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("apply shipped impact repository index: %v", err)
			}
		}
	}
	if _, err := db.ExecContext(ctx, `ANALYZE fact_records;`); err != nil {
		t.Fatalf("analyze impact repo facts: %v", err)
	}
}

// queryImpactRepoEvidence runs the shipped ListEvidenceQuery with the given
// impact selector and returns the sorted evidence fact ids.
func queryImpactRepoEvidence(t *testing.T, ctx context.Context, db *sql.DB, repository, service, workload string) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, listAdvisoryEvidenceQuery,
		array.Of(impactRepoAdvisoryKinds),
		array.Of([]string{}),
		array.Of([]string{}),
		"",
		advisory.EvidenceMaxFactRows,
		repository, service, workload,
		array.Of([]string{}),
	)
	if err != nil {
		t.Fatalf("query impact repo evidence: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var factID, kind, confidence, schemaVersion string
		var observedAt time.Time
		var payload []byte
		if err := rows.Scan(&factID, &kind, &confidence, &observedAt, &schemaVersion, &payload); err != nil {
			t.Fatalf("scan evidence row: %v", err)
		}
		ids = append(ids, factID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate evidence rows: %v", err)
	}
	return ids
}

// TestAdvisoryEvidenceImpactRepoProbeLive executes the shipped query text
// against repository-anchored selectors: the RED-first executing proof for
// the #9487 repository-first branch that string pins cannot provide.
func TestAdvisoryEvidenceImpactRepoProbeLive(t *testing.T) {
	db := openAdvisoryPackageKindLiveDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()

	seedAdvisoryImpactRepoFixtures(t, ctx, db)

	contains := func(ids []string, want string) bool {
		for _, id := range ids {
			if id == want {
				return true
			}
		}
		return false
	}

	repoA := queryImpactRepoEvidence(t, ctx, db, "repository:repo-A", "", "")
	if !contains(repoA, "repo-adv-9101") {
		t.Fatalf("repository scope must resolve live finding CVE-2026-9101, got %v", repoA)
	}
	for _, excluded := range []string{"repo-adv-9103", "repo-adv-9104"} {
		if contains(repoA, excluded) {
			t.Fatalf("repository scope must exclude %s (stale generation / tombstone), got %v", excluded, repoA)
		}
	}

	repoSvc := queryImpactRepoEvidence(t, ctx, db, "repository:repo-A", "service:svc-X", "")
	if !contains(repoSvc, "repo-adv-9101") {
		t.Fatalf("repository+service scope must resolve CVE-2026-9101, got %v", repoSvc)
	}

	repoSvcMiss := queryImpactRepoEvidence(t, ctx, db, "repository:repo-A", "service:svc-missing", "")
	if contains(repoSvcMiss, "repo-adv-9101") {
		t.Fatalf("repository+missing-service scope must exclude CVE-2026-9101, got %v", repoSvcMiss)
	}

	workload := queryImpactRepoEvidence(t, ctx, db, "", "", "workload:repo-W")
	if !contains(workload, "repo-adv-9106") {
		t.Fatalf("workload scope must resolve CVE-2026-9106, got %v", workload)
	}
}
