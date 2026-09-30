// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// Issue #7154: a scope whose installed OS-package targets exceed one reader
// page (500) used to be marked partial on every pass, so its superseded
// findings were never retracted. These proofs run the real handler over a real
// FactStore, so the reducer's runtime type assertion against the storage
// reader is exercised too: a signature drift would silently load no OS-package
// evidence and fail here rather than in production.
const (
	cappedScanScope      = "scan-target:7154:capped"
	cappedScanGeneration = "generation:7154:scan"
	cappedCVE            = "CVE-2026-71540"
)

// openCappedScopeLiveDB opens the isolated replace-set schema and applies the
// full bootstrap schema on top, because the reducer's active-evidence loader
// reads SQL functions and support tables the four-table replace-set schema
// does not carry.
func openCappedScopeLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	// The sibling helper pins pg_trgm to public and puts public on the
	// search_path, so the full bootstrap finds gin_trgm_ops whichever live
	// test installed the extension first.
	db := openCappedScopeSchema(ctx, t)
	if err := postgres.ApplyBootstrapWithoutContentSearchIndexes(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	replaceSetSeedScope(t, ctx, db)
	return ctx, db
}

// openCappedScopeSchema opens a pool whose search_path is a fresh isolated
// schema then public, dropped when the test ends. pg_trgm is pinned to public
// first: the bootstrap's CREATE EXTENSION IF NOT EXISTS would otherwise install
// it into the private schema and drop it with the schema, and a bootstrap that
// runs while pg_trgm lives only in public needs public on the search_path to
// resolve gin_trgm_ops.
func openCappedScopeSchema(ctx context.Context, t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #7154 capped-scope proof")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres admin connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if _, err := adminDB.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public"); err != nil {
		t.Fatalf("install pg_trgm in public: %v", err)
	}
	schema := fmt.Sprintf("capped_7154_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+array.QuoteIdentifier(schema)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = adminDB.ExecContext(cleanupCtx, "DROP SCHEMA "+array.QuoteIdentifier(schema)+" CASCADE")
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse ESHU_POSTGRES_TEST_DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping isolated schema: %v", err)
	}
	return db
}

// seedCappedScanScope registers an active scan-target scope holding count
// vulnerability.os_package facts (pkg-0000 ... pkg-<count-1>) that the
// advisory-target reader returns.
func seedCappedScanScope(t *testing.T, ctx context.Context, db *sql.DB, count int) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'container_image', 'synthetic', $1, 'synthetic',
          $1, $2, $2, 'active', '{}'::jsonb)`,
		cappedScanScope, replaceSetLiveNow,
	); err != nil {
		t.Fatalf("insert scan scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`,
		cappedScanGeneration, cappedScanScope, replaceSetLiveNow,
	); err != nil {
		t.Fatalf("insert scan generation: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		cappedScanGeneration, cappedScanScope,
	); err != nil {
		t.Fatalf("activate scan generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone,
  payload, fencing_token
)
SELECT
  'os-package:7154:' || lpad(n::text, 6, '0'), $1, $2, 'vulnerability.os_package',
  'os-package:7154:' || lpad(n::text, 6, '0'), 'synthetic',
  'os-package:7154:' || lpad(n::text, 6, '0'), $3::timestamptz, $3::timestamptz, FALSE,
  jsonb_build_object(
    'distro', 'debian', 'distro_version', '12', 'package_manager', 'dpkg',
    'name', 'pkg-' || lpad(n::text, 4, '0'), 'arch', 'amd64',
    'repository_class', 'vendor', 'vendor_advisory_source', 'debian',
    'installed_version_raw', '1.0-1',
    'purl', 'pkg:deb/debian/pkg-' || lpad(n::text, 4, '0') || '@1.0-1?arch=amd64'),
  0
FROM generate_series(0, $4::integer - 1) AS n`,
		cappedScanScope, cappedScanGeneration, replaceSetLiveNow, count,
	); err != nil {
		t.Fatalf("seed os_package facts: %v", err)
	}
}

// cappedSeedIntelFact inserts one active fact into the intent's
// vulnerability-intelligence scope.
func cappedSeedIntelFact(t *testing.T, ctx context.Context, db *sql.DB, factID, kind, payloadJSON string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone,
  payload, fencing_token
) VALUES ($1, $2, $3, $4, $1, 'synthetic', $1, $5, $5, FALSE, $6::jsonb, 0)`,
		factID, replaceSetLiveScope, replaceSetLiveGeneration, kind, replaceSetLiveNow, payloadJSON,
	); err != nil {
		t.Fatalf("seed intel fact %s: %v", factID, err)
	}
}

type capturingImpactWriter struct {
	inner reducer.PostgresSupplyChainImpactWriter
	write reducer.SupplyChainImpactWrite
}

func (c *capturingImpactWriter) WriteSupplyChainImpactFindings(
	ctx context.Context,
	write reducer.SupplyChainImpactWrite,
) (reducer.SupplyChainImpactWriteResult, error) {
	c.write = write
	return c.inner.WriteSupplyChainImpactFindings(ctx, write)
}

// TestSupplyChainImpactCappedScopeConvergesLive is the #7154 headline proof.
// The scope holds 1203 installed OS-package targets (more than two reader
// pages) and a stale finding row an earlier pass wrote. On main the target
// count trips the 500-target cap, the pass is marked partial, and the stale row
// stays active forever. Paged to completion the pass is a complete view: it
// retracts the stale row and keeps the finding it derives.
func TestSupplyChainImpactCappedScopeConvergesLive(t *testing.T) {
	ctx, db := openCappedScopeLiveDB(t)
	const targets = 1203
	seedCappedScanScope(t, ctx, db, targets)
	cappedSeedIntelFact(t, ctx, db, "vuln-cve:7154", "vulnerability.cve", fmt.Sprintf(
		`{"cve_id":%q,"advisory_id":"DSA-2026-7154","source":"debian","cvss_score":7.5,
		  "cvss_vector":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N","severity_label":"HIGH",
		  "modified_at":"2026-06-05T12:00:00Z","aliases":[%q,"DSA-2026-7154"]}`, cappedCVE, cappedCVE))
	cappedSeedIntelFact(t, ctx, db, "vuln-affected:7154", "vulnerability.affected_package", fmt.Sprintf(
		`{"cve_id":%q,"advisory_id":"DSA-2026-7154","source":"debian",
		  "package_id":"pkg:deb/debian/pkg-0000","ecosystem":"deb","package_name":"pkg-0000",
		  "affected_versions":["1.0-1"],"fixed_versions":["1.0-2"]}`, cappedCVE))
	// An OS-package finding derived from a target past the first reader page
	// (fact id order puts pkg-1100 in the third page): the narrowed read must
	// reach it and derive its finding, not only load counts.
	cappedSeedIntelFact(t, ctx, db, "vuln-cve:7154-deb-late", "vulnerability.cve",
		`{"cve_id":"CVE-2026-71542","advisory_id":"DSA-2026-71542","source":"debian","cvss_score":7.5,
		  "severity_label":"HIGH","modified_at":"2026-06-05T12:00:00Z","aliases":["CVE-2026-71542","DSA-2026-71542"]}`)
	cappedSeedIntelFact(t, ctx, db, "vuln-affected:7154-deb-late", "vulnerability.affected_package",
		`{"cve_id":"CVE-2026-71542","advisory_id":"DSA-2026-71542","source":"debian",
		  "package_id":"pkg:deb/debian/pkg-1100","ecosystem":"deb","package_name":"pkg-1100",
		  "affected_versions":["1.0-1"],"fixed_versions":["1.0-2"]}`)
	// A finding the pass does derive, so the proof also shows retraction keeps
	// what the evidence still supports: an npm package a repository consumes at
	// an affected version.
	cappedSeedIntelFact(t, ctx, db, "vuln-cve:7154-npm", "vulnerability.cve",
		`{"cve_id":"CVE-2026-71541","advisory_id":"CVE-2026-71541","cvss_score":8.1,"aliases":["CVE-2026-71541"]}`)
	cappedSeedIntelFact(t, ctx, db, "vuln-affected:7154-npm", "vulnerability.affected_package",
		`{"cve_id":"CVE-2026-71541","advisory_id":"CVE-2026-71541","package_id":"pkg:npm/example",
		  "ecosystem":"npm","package_name":"example","affected_versions":["1.2.3"],"fixed_versions":["1.3.0"]}`)
	cappedSeedIntelFact(t, ctx, db, "package-consumption:7154", "reducer_package_consumption_correlation",
		`{"package_id":"pkg:npm/example","relationship_kind":"consumption","repository_id":"repo://example/api",
		  "dependency_range":"1.2.3","canonical_writes":1,"evidence_fact_ids":["manifest-lock-1"]}`)
	// The stale row: an earlier pass's finding the current evidence no longer
	// derives. Same (scope, generation, kind) as the pass under test.
	replaceSetPlantRow(t, ctx, db, "stale-finding:7154", replaceSetLiveScope, replaceSetLiveGeneration,
		facts.ReducerSupplyChainImpactFindingFactKind, 0)

	capture := &capturingImpactWriter{inner: replaceSetWriter(db)}
	handler := reducer.SupplyChainImpactHandler{
		FactLoader: postgres.NewFactStore(postgres.SQLDB{DB: db}),
		Writer:     capture,
	}
	result, err := handler.Handle(ctx, reducercontract.Intent{
		IntentID:     "intent:7154:capped",
		ScopeID:      replaceSetLiveScope,
		GenerationID: replaceSetLiveGeneration,
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "synthetic #7154 capped-scope proof",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	// The read is narrowed to the intent's affected packages (two of the 1203
	// installed targets), so the load count is those two, not every target.
	if got, want := result.SubSignals["os_package_advisory_facts"], float64(2); got != want {
		t.Fatalf("os_package_advisory_facts = %v, want 2 (the matching packages of %d installed targets)", got, targets)
	}
	if capture.write.PartialEvidence {
		t.Fatalf("PartialEvidence = true for a scope that pages to completion (%d targets): the stale finding would stay forever", targets)
	}
	var tombstoned bool
	if err := db.QueryRowContext(ctx,
		`SELECT is_tombstone FROM fact_records WHERE fact_id = 'stale-finding:7154'`,
	).Scan(&tombstoned); err != nil {
		t.Fatalf("read stale row: %v", err)
	}
	if !tombstoned {
		t.Fatal("stale finding is still active after a pass over a scope larger than one reader page; a capped scope must converge (#7154)")
	}
	var lateOSFindings int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_records
WHERE fact_kind = $1 AND scope_id = $2 AND generation_id = $3 AND is_tombstone = FALSE
  AND payload->>'cve_id' = 'CVE-2026-71542'`,
		facts.ReducerSupplyChainImpactFindingFactKind, replaceSetLiveScope, replaceSetLiveGeneration,
	).Scan(&lateOSFindings); err != nil {
		t.Fatalf("count late OS findings: %v", err)
	}
	if lateOSFindings == 0 {
		t.Fatal("no finding derived for the OS package past the first reader page; the read must reach every matching target")
	}
	var active int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_records
WHERE fact_kind = $1 AND scope_id = $2 AND generation_id = $3 AND is_tombstone = FALSE`,
		facts.ReducerSupplyChainImpactFindingFactKind, replaceSetLiveScope, replaceSetLiveGeneration,
	).Scan(&active); err != nil {
		t.Fatalf("count active findings: %v", err)
	}
	if active != len(capture.write.Findings) || active == 0 {
		t.Fatalf("active findings = %d, want the %d the pass derived (non-zero)", active, len(capture.write.Findings))
	}
}
