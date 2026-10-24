// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// fileKindGateProofRepoFileFacts is how many `file` facts each seeded
// repository scope carries. Their payloads are large enough, and compress
// poorly enough, that Postgres stores them out of line -- which is the whole
// mechanism behind #5237: without the gate each ungated identity predicate
// detoasts the payload again.
const (
	fileKindGateProofRepoFileFacts = 400
	fileKindGateProofRepoScopeA    = "repository:5237:gate-proof-a"
	fileKindGateProofRepoScopeB    = "repository:5237:gate-proof-b"
	fileKindGateProofOSVScope      = "vulnerability:5237:gate-proof"
	fileKindGateProofPURL          = "pkg:maven/org.example/example-artifact@1.2.3"
	fileKindGateProofCVE           = "CVE-2026-52370"
)

// TestSupplyChainImpactFileKindGateLive proves the #5237 gate on the #8880
// loader against real Postgres:
//
//   - Exactness. For an empty FileRepositoryIDs (a Maven intent), a NULL one,
//     and a populated one (an npm intent), the production loader returns the
//     rows of the frozen pre-#8880 statement walked to exhaustion.
//   - Mechanism. With FileRepositoryIDs empty the loader issues no file
//     statement at all, and its statements touch a small fraction of the
//     buffers the frozen statement does with the gate neutralised (the
//     per-predicate detoast traffic the evidence note reports).
//
// Wall-clock ratios are deliberately not asserted; buffer counts are a
// property of the plan and the data.
func TestSupplyChainImpactFileKindGateLive(t *testing.T) {
	ctx, db := openFileKindGateProofDB(t)
	applyFileKindGateProofDefinitions(t, ctx, db)
	seedFileKindGateProofFacts(t, ctx, db)
	recorder := &supplyChainImpactRecordingDB{SQLDB: SQLDB{DB: db}}

	repoScopes := []string{fileKindGateProofRepoScopeA, fileKindGateProofRepoScopeB}
	for _, tc := range []struct {
		name              string
		fileRepositoryIDs []string
		wantFileRows      bool
	}{
		{name: "maven intent binds an empty FileRepositoryIDs", fileRepositoryIDs: []string{}},
		{name: "nil FileRepositoryIDs", fileRepositoryIDs: nil},
		{name: "npm intent binds populated FileRepositoryIDs", fileRepositoryIDs: repoScopes, wantFileRows: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := fileKindGateProofFilter(tc.fileRepositoryIDs)
			want, _ := walkSupplyChainImpactRetiredOracle(t, ctx, db, filter)
			got, statements := supplyChainImpactProductionRows(t, ctx, recorder, filter)
			if !reflect.DeepEqual(supplyChainImpactRowLines(t, got), supplyChainImpactRowLines(t, want)) {
				t.Fatalf("the #8880 loader changed the #5237 result set\n%s",
					supplyChainImpactLineDiff(supplyChainImpactRowLines(t, want), supplyChainImpactRowLines(t, got)))
			}
			gotFileRows := false
			for _, row := range got.Rows {
				gotFileRows = gotFileRows || row.FactKind == "file"
			}
			if gotFileRows != tc.wantFileRows {
				t.Fatalf("file rows present = %v, want %v", gotFileRows, tc.wantFileRows)
			}
			if !tc.wantFileRows {
				for _, statement := range statements {
					if strings.Contains(statement.Query, factimpact.JSTSLanguagePredicate+"\n      AND fact.payload->>'repo_id'") ||
						strings.Contains(statement.Query, "fact.fact_kind = 'file'\n      AND fact.is_tombstone = FALSE") {
						t.Fatalf("an intent without FileRepositoryIDs issued a file statement:\n%s", statement.Query)
					}
				}
			}
		})
	}

	filter := fileKindGateProofFilter([]string{})
	_, statements := supplyChainImpactProductionRows(t, ctx, recorder, filter)
	shipped, _ := supplyChainImpactStatementBuffers(t, ctx, db, statements, "force_generic_plan")
	ungatedOracle := fileKindGateProofUngatedQuery(t, supplyChainImpactRetiredOracleQuery)
	ungated, _ := supplyChainImpactStatementBuffers(t, ctx, db, []supplyChainImpactRecordedStatement{{
		Query: ungatedOracle, Args: supplyChainImpactRetiredOracleArgs(normalizedSupplyChainImpactFilter(filter), "", false),
	}}, "force_generic_plan")
	t.Logf("%d file facts across 2 repository scopes, Maven intent: loader buffers=%d over %d statements, ungated retired statement buffers=%d (%.0fx)",
		2*fileKindGateProofRepoFileFacts, shipped, len(statements), ungated, float64(ungated)/float64(max(shipped, 1)))
	if shipped*10 > ungated {
		t.Fatalf("loader saved less than 10x the buffer traffic: loader=%d ungated=%d", shipped, ungated)
	}
}

// fileKindGateProofFilter is the single-package Maven filter with the given
// FileRepositoryIDs.
func fileKindGateProofFilter(fileRepositoryIDs []string) supplychaincore.SupplyChainImpactFactFilter {
	return supplychaincore.SupplyChainImpactFactFilter{
		PURLs:             []string{fileKindGateProofPURL},
		CVEIDs:            []string{fileKindGateProofCVE},
		FileRepositoryIDs: fileRepositoryIDs,
	}
}

// normalizedSupplyChainImpactFilter returns the filter the loader binds.
func normalizedSupplyChainImpactFilter(filter supplychaincore.SupplyChainImpactFactFilter) supplychaincore.SupplyChainImpactFactFilter {
	normalizeSupplyChainImpactFactFilter(&filter)
	return filter
}

// fileKindGateProofUngatedQuery returns the frozen retired statement with the
// #5237 conjunct neutralised, cut out of the frozen text so the "before" side
// of the mechanism proof cannot drift into a stale hand-copied query.
//
// It anchors on the conjunct's opening rather than its full text and replaces
// the whole line. That is deliberate: an anchor on the exact guard expression
// would turn every rewrite of that guard into a "gate not found" failure, and
// the differential below would never get to run against the rewrite. Anchored
// this way, a guard that no longer means what it should — dropping the $10 test,
// or widening it to something always true — still reaches the comparison and
// fails there, on behaviour.
func fileKindGateProofUngatedQuery(t *testing.T, shipped string) string {
	t.Helper()

	const anchor = "AND (fact.fact_kind <> 'file'"
	if occurrences := strings.Count(shipped, anchor); occurrences != 1 {
		t.Fatalf(
			"the #5237 file-kind conjunct %q appears %d times in the shipped statement, want exactly 1",
			anchor, occurrences,
		)
	}
	at := strings.Index(shipped, anchor)
	end := strings.Index(shipped[at:], "\n")
	if end < 0 {
		t.Fatalf("the #5237 file-kind conjunct at offset %d does not end in a newline", at)
	}
	return shipped[:at] + "AND TRUE" + shipped[at+end:]
}

func openFileKindGateProofDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5237 file-kind gate proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres admin connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })

	schemaName := fmt.Sprintf("supply_chain_5237_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+array.QuoteIdentifier(schemaName)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = adminDB.ExecContext(
			cleanupCtx,
			"DROP SCHEMA "+array.QuoteIdentifier(schemaName)+" CASCADE",
		)
	})

	targetURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse ESHU_POSTGRES_TEST_DSN: %v", err)
	}
	params := targetURL.Query()
	params.Set("search_path", schemaName)
	targetURL.RawQuery = params.Encode()
	db, err := sql.Open("pgx", targetURL.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping isolated Postgres schema: %v", err)
	}
	return ctx, db
}

// applyFileKindGateProofDefinitions installs the real table shapes the page
// query reads, including the container-image identity stream it unions in, so
// the statement under test is the production statement and not a trimmed
// stand-in.
func applyFileKindGateProofDefinitions(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	required := map[string]bool{
		"ingestion_scopes":                                        true,
		"scope_generations":                                       true,
		"fact_records":                                            true,
		"fact_work_items":                                         true,
		"container_image_identity_support_store":                  true,
		"container_image_identity_support_current_view":           true,
		"container_image_identity_current_facts_function":         true,
		"container_image_identity_current_support_facts_function": true,
	}
	definitions := supplyChainImpactDefinitionsWithAnchors(t, required)
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
		t.Fatalf("apply isolated Postgres schema: %v", err)
	}
}

// seedFileKindGateProofFacts builds the shape #5237 reported: one OSV scope
// holding the evidence the intent actually wants, and repository scopes holding
// orders of magnitude more `file` facts with TOASTed payloads.
func seedFileKindGateProofFacts(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	for scopeID, scopeKind := range map[string]string{
		fileKindGateProofOSVScope:   "vulnerability_intelligence",
		fileKindGateProofRepoScopeA: "repository",
		fileKindGateProofRepoScopeB: "repository",
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload
) VALUES (
  $1, $3, 'synthetic', $1, 'synthetic', $1,
  '2026-08-12T12:00:00Z', '2026-08-12T12:00:00Z', 'active', $2, '{}'::jsonb
)`, scopeID, scopeID+":generation", scopeKind); err != nil {
			t.Fatalf("seed scope %s: %v", scopeID, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES (
  $2, $1, 'synthetic', '2026-08-12T12:00:00Z', '2026-08-12T12:00:00Z',
  'active', '2026-08-12T12:00:00Z', '{}'::jsonb
)`, scopeID, scopeID+":generation"); err != nil {
			t.Fatalf("seed generation for %s: %v", scopeID, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES
(
  'fact:osv:purl', $1, $2, 'vulnerability.affected_package', 'stable:osv:purl',
  '1.0.0', 'synthetic', 1, 'reported', 'synthetic', 'source:osv:purl',
  '2026-08-12T12:00:00Z', '2026-08-12T12:00:00Z',
  jsonb_build_object('purl', $3::text)
),
(
  'fact:osv:cve', $1, $2, 'vulnerability.cve', 'stable:osv:cve',
  '1.0.0', 'synthetic', 1, 'reported', 'synthetic', 'source:osv:cve',
  '2026-08-12T12:00:00Z', '2026-08-12T12:00:00Z',
  jsonb_build_object('cve_id', $4::text)
)`, fileKindGateProofOSVScope, fileKindGateProofOSVScope+":generation",
		fileKindGateProofPURL, fileKindGateProofCVE); err != nil {
		t.Fatalf("seed OSV evidence: %v", err)
	}

	for _, scopeID := range []string{fileKindGateProofRepoScopeA, fileKindGateProofRepoScopeB} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  format('fact:file:%s:%s', $1::text, lpad(n::text, 6, '0')), $1, $2, 'file',
  format('stable:%s:%s', $1::text, lpad(n::text, 6, '0')), '1.0.0', 'synthetic', 1, 'reported',
  'synthetic', format('source:%s:%s', $1::text, lpad(n::text, 6, '0')),
  '2026-08-12T12:00:00Z', '2026-08-12T12:00:00Z',
  jsonb_build_object(
    'repo_id', $1::text,
    'relative_path', format('src/generated/file_%s.ts', lpad(n::text, 6, '0')),
    'is_dependency', false,
    'parsed_file_data', jsonb_build_object(
      'language', CASE WHEN mod(n, 2) = 0 THEN 'typescript' ELSE 'go' END,
      -- ~25 KB of poorly compressible text per payload. Postgres compresses
      -- it and still has to push it out of line, so every payload->>'key'
      -- extraction pays a detoast -- the #5237 mechanism. Distinct md5 blocks
      -- rather than one repeated block: a repeated block compresses down to
      -- an inline payload and the mechanism disappears.
      'content', (
        SELECT string_agg(md5(n::text || g::text), '')
        FROM generate_series(1, 800) AS g
      )
    )
  )
FROM generate_series(1, $3::integer) AS n`,
			scopeID, scopeID+":generation", fileKindGateProofRepoFileFacts); err != nil {
			t.Fatalf("seed file facts for %s: %v", scopeID, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
ANALYZE fact_records;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;`); err != nil {
		t.Fatalf("analyze seeded corpus: %v", err)
	}
}
