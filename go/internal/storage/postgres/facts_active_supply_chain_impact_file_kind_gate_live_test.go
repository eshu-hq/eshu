// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/lib/pq"
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

// TestSupplyChainImpactFileKindGateLive executes the statement the reducer
// actually runs -- listActiveSupplyChainImpactFactPagesQuery, bound exactly as
// loadActiveSupplyChainImpactFactPagePair binds it -- against real Postgres,
// and proves the two things the #5237 gate is accepted on:
//
//   - Exactness. For an empty $10 (a Maven intent), a NULL $10, and a
//     non-empty $10 (an npm intent), the shipped statement returns byte-identical
//     result sets to the same statement with the gate neutralised. The gate is
//     derived out of the shipped constant rather than hand-copied, so the
//     comparison cannot drift into comparing two stale strings.
//   - Mechanism. With $10 empty, the shipped statement's buffer accesses are a
//     small fraction of the ungated statement's. That is the detoast traffic the
//     evidence note reports, measured here on a seeded corpus rather than only
//     in a throwaway harness.
//
// Wall-clock ratios are deliberately not asserted: this runs on whatever
// machine and container the operator has. Buffer counts are a property of the
// plan and the data, so they hold across machines; the headline seconds stay in
// docs/internal/evidence/5237-supply-chain-impact-file-fact-scan.md.
func TestSupplyChainImpactFileKindGateLive(t *testing.T) {
	ctx, db := openFileKindGateProofDB(t)
	applyFileKindGateProofDefinitions(t, ctx, db)
	seedFileKindGateProofFacts(t, ctx, db)

	shipped := listActiveSupplyChainImpactFactPagesQuery
	ungated := fileKindGateProofUngatedQuery(t, shipped)

	repoScopes := []string{fileKindGateProofRepoScopeA, fileKindGateProofRepoScopeB}
	for _, tc := range []struct {
		name              string
		fileRepositoryIDs any
		wantFileRows      bool
	}{
		{name: "maven intent binds an empty $10", fileRepositoryIDs: []string{}},
		{name: "driver binds $10 as NULL", fileRepositoryIDs: nil},
		{name: "npm intent binds a populated $10", fileRepositoryIDs: repoScopes, wantFileRows: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shippedRows := fileKindGateProofRows(t, ctx, db, shipped, tc.fileRepositoryIDs)
			ungatedRows := fileKindGateProofRows(t, ctx, db, ungated, tc.fileRepositoryIDs)

			if shippedRows != ungatedRows {
				t.Fatalf(
					"the #5237 gate changed the result set.\nshipped:\n%s\nungated:\n%s",
					shippedRows, ungatedRows,
				)
			}
			if !strings.Contains(shippedRows, "fact:osv:purl") ||
				!strings.Contains(shippedRows, "fact:osv:cve") {
				t.Fatalf("both variants lost the seeded OSV evidence:\n%s", shippedRows)
			}
			if gotFileRows := strings.Contains(shippedRows, "|file"); gotFileRows != tc.wantFileRows {
				t.Fatalf("file rows present = %v, want %v:\n%s", gotFileRows, tc.wantFileRows, shippedRows)
			}
		})
	}

	shippedBuffers := fileKindGateProofBuffers(t, ctx, db, shipped, []string{})
	ungatedBuffers := fileKindGateProofBuffers(t, ctx, db, ungated, []string{})
	t.Logf(
		"%d file facts across 2 repository scopes, Maven intent (empty $10): "+
			"shipped buffers=%d, ungated buffers=%d (%.0fx)",
		2*fileKindGateProofRepoFileFacts,
		shippedBuffers, ungatedBuffers,
		float64(ungatedBuffers)/float64(shippedBuffers),
	)
	if shippedBuffers*10 > ungatedBuffers {
		t.Fatalf(
			"gate saved less than 10x the buffer traffic: shipped=%d ungated=%d. "+
				"#5237 rests on the gate removing the file rows before their TOASTed "+
				"payloads are extracted once per ungated identity predicate",
			shippedBuffers, ungatedBuffers,
		)
	}
}

// fileKindGateProofUngatedQuery returns the shipped statement with the #5237
// conjunct neutralised, cut out of the shipped constant so the "before" side of
// the differential can never drift into a stale hand-copied query.
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

// fileKindGateProofRows returns one line per returned row, in the order the
// statement produced it, so a differential failure prints what actually
// differed instead of a bare count.
func fileKindGateProofRows(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	fileRepositoryIDs any,
) string {
	t.Helper()

	rows, err := db.QueryContext(
		ctx,
		"SELECT stream_rank, stream_ordinal, fact_id, fact_kind FROM ("+query+") AS selected",
		fileKindGateProofArgs(fileRepositoryIDs)...,
	)
	if err != nil {
		t.Fatalf("run supply-chain impact page query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var lines []string
	for rows.Next() {
		var (
			rank     int
			ordinal  int64
			factID   string
			factKind string
		)
		if err := rows.Scan(&rank, &ordinal, &factID, &factKind); err != nil {
			t.Fatalf("scan supply-chain impact row: %v", err)
		}
		lines = append(lines, fmt.Sprintf("%d|%d|%s|%s", rank, ordinal, factID, factKind))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate supply-chain impact rows: %v", err)
	}
	return strings.Join(lines, "\n")
}

// fileKindGateProofBuffers reports total shared buffer accesses for one warm
// execution, the counter the #5237 evidence note reports as the mechanism.
func fileKindGateProofBuffers(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	fileRepositoryIDs any,
) int64 {
	t.Helper()

	var buffers int64
	// Run twice and keep the second: the first execution pays the cold reads
	// that would otherwise land in whichever variant ran first.
	for run := 0; run < 2; run++ {
		var raw []byte
		if err := db.QueryRowContext(
			ctx,
			"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query,
			fileKindGateProofArgs(fileRepositoryIDs)...,
		).Scan(&raw); err != nil {
			t.Fatalf("EXPLAIN supply-chain impact page query: %v", err)
		}
		var decoded []struct {
			Plan struct {
				SharedHitBlocks  int64 `json:"Shared Hit Blocks"`
				SharedReadBlocks int64 `json:"Shared Read Blocks"`
			} `json:"Plan"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode EXPLAIN output: %v", err)
		}
		if len(decoded) != 1 {
			t.Fatalf("EXPLAIN plans = %d, want 1", len(decoded))
		}
		buffers = decoded[0].Plan.SharedHitBlocks + decoded[0].Plan.SharedReadBlocks
	}
	return buffers
}

// fileKindGateProofArgs binds the 21 placeholders exactly as
// loadActiveSupplyChainImpactFactPagePair binds them for a single-package
// intent: the Maven PURL and its CVE in the identity slots, everything else
// empty, and $10 (FileRepositoryIDs) supplied by the caller.
func fileKindGateProofArgs(fileRepositoryIDs any) []any {
	empty := []string{}
	purls := []string{fileKindGateProofPURL}
	cves := []string{fileKindGateProofCVE}
	return []any{
		empty, purls, cves, empty, empty, empty,
		empty, empty, empty, fileRepositoryIDs, "", 1_000,
		empty,
		lowerCleanedStringFilterValues(purls),
		lowerCleanedStringFilterValues(cves),
		empty, empty, empty,
		false, "", 1_000,
	}
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
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schemaName)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = adminDB.ExecContext(
			cleanupCtx,
			"DROP SCHEMA "+pq.QuoteIdentifier(schemaName)+" CASCADE",
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
	var definitions []Definition
	for _, definition := range BootstrapDefinitions() {
		if required[definition.Name] {
			definitions = append(definitions, definition)
		}
	}
	if len(definitions) != len(required) {
		t.Fatalf("isolated schema definitions = %d, want %d", len(definitions), len(required))
	}
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
