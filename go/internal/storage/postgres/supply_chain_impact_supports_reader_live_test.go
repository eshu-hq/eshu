// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11198 and #11199 reducer input moved off finding facts, on real PostgreSQL: on a multi-scope seed written through the production impact writer (active and superseded generations, a retracted finding, a CVE-less advisory finding, one finding per repository and package shape) the security-alert reconciliation loader and the service-catalog vulnerability loader return exactly the rows and payloads the frozen finding-fact reads return, and both refuse with a retryable not-ready error before the supports backfill is ready
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The frozen fact-read oracles below are the statements the two loaders ran
// before #11198 and #11199 (origin/main bccdb88982), kept here so the equality
// proof compares the new supports reads against the real finding-fact reads.
// The alert-loader oracle is derived from the shipped statement by restoring
// the impact kind to its IN list, so only that one edit is frozen.
const legacyAdvisoriesForReposOracle = `
SELECT fact.fact_id, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'repository_id' = ANY($1::text[])
ORDER BY fact.fact_id ASC
`

func legacyReconciliationOracleQuery(t *testing.T) string {
	t.Helper()
	const without = "    'reducer_package_consumption_correlation'\n)"
	const with = "    'reducer_package_consumption_correlation',\n    'reducer_supply_chain_impact_finding'\n)"
	if !strings.Contains(listActiveSecurityAlertReconciliationFactsQuery, without) {
		t.Fatal("the shipped reconciliation fact arm no longer ends its kind list as the oracle derivation expects")
	}
	return strings.Replace(listActiveSecurityAlertReconciliationFactsQuery, without, with, 1)
}

const (
	b2cScopeA     = "scope:11198:a"
	b2cGenA       = "generation:11198:a"
	b2cScopeB     = "scope:11198:b"
	b2cGenB       = "generation:11198:b"
	b2cGenBOld    = "generation:11198:b:old"
	b2cScopeC     = "scope:11198:c"
	b2cGenC       = "generation:11198:c"
	b2cNow        = "2026-11-23T12:00:00Z"
	b2cAlertScope = b2cScopeA
)

// b2cSeedScopes inserts the scopes and generations: B holds a superseded
// generation whose findings must never be read, C is the scope whose finding
// is retracted.
func b2cSeedScopes(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	now, _ := time.Parse(time.RFC3339, b2cNow)
	for _, scope := range []struct{ id, active string }{{b2cScopeA, b2cGenA}, {b2cScopeB, b2cGenB}, {b2cScopeC, b2cGenC}} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb)`,
			scope.id, now); err != nil {
			t.Fatalf("insert scope %s: %v", scope.id, err)
		}
	}
	for _, generation := range []struct{ id, scope, status string }{
		{b2cGenA, b2cScopeA, "active"},
		{b2cGenBOld, b2cScopeB, "superseded"},
		{b2cGenB, b2cScopeB, "active"},
		{b2cGenC, b2cScopeC, "active"},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, $4, $3, '{}'::jsonb)`,
			generation.id, generation.scope, now, generation.status); err != nil {
			t.Fatalf("insert generation %s: %v", generation.id, err)
		}
	}
	for _, scope := range []struct{ id, active string }{{b2cScopeA, b2cGenA}, {b2cScopeB, b2cGenB}, {b2cScopeC, b2cGenC}} {
		if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
			scope.active, scope.id); err != nil {
			t.Fatalf("activate %s: %v", scope.id, err)
		}
	}
}

func b2cFinding(repo, pkg, cve, advisory, version string) supplychaincore.SupplyChainImpactFinding {
	return supplychaincore.SupplyChainImpactFinding{
		CVEID: cve, AdvisoryID: advisory, PackageID: pkg, Ecosystem: "npm", PackageName: pkg,
		ObservedVersion: version, Status: supplychaincore.SupplyChainImpactAffectedExact, RepositoryID: repo,
	}
}

var b2cToken int64

func b2cWrite(scope, generation, intent string, findings ...supplychaincore.SupplyChainImpactFinding) supplychaincore.SupplyChainImpactWrite {
	b2cToken++
	return supplychaincore.SupplyChainImpactWrite{
		FencingToken: b2cToken, IntentID: intent, ScopeID: scope, GenerationID: generation,
		SourceSystem: "vulnerability_intelligence", Cause: "synthetic #11198 equality seed", Findings: findings,
	}
}

// b2cSeed writes the multi-scope seed through the production impact writer and
// adds the alert and consumption facts the reconciliation loader also reads.
func b2cSeed(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	b2cSeedScopes(t, ctx, db)
	now, _ := time.Parse(time.RFC3339, b2cNow)
	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  SupplyChainImpactBeginner{Beginner: SQLDB{DB: db}},
		Now: func() time.Time { return now },
	}
	write := func(w supplychaincore.SupplyChainImpactWrite) {
		t.Helper()
		if _, err := writer.WriteSupplyChainImpactFindings(ctx, w); err != nil {
			t.Fatalf("write %s: %v", w.IntentID, err)
		}
	}
	// Scope A: two repositories, one package shared by both, a CVE-less
	// advisory finding, and a repository-less finding.
	write(b2cWrite(b2cScopeA, b2cGenA, "intent:a",
		b2cFinding("repo:r1", "pkg:npm/p1", "CVE-2026-0001", "GHSA-1111", "1.0.0"),
		b2cFinding("repo:r2", "pkg:npm/p1", "CVE-2026-0001", "GHSA-1111", "1.0.1"),
		b2cFinding("repo:r1", "pkg:npm/p2", "", "GHSA-2222", "2.0.0"),
		b2cFinding("", "pkg:npm/p3", "CVE-2026-0003", "", "3.0.0")))
	// Scope B: a finding in the superseded generation, then the active one.
	write(b2cWrite(b2cScopeB, b2cGenBOld, "intent:b-old", b2cFinding("repo:r3", "pkg:npm/p4", "CVE-2026-0004", "", "4.0.0")))
	write(b2cWrite(b2cScopeB, b2cGenB, "intent:b",
		b2cFinding("repo:r3", "pkg:npm/p1", "CVE-2026-0001", "GHSA-1111", "1.0.0"),
		b2cFinding("repo:r1", "pkg:npm/p5", "CVE-2026-0005", "", "5.0.0")))
	// Scope C: a finding written then retracted by a complete second pass.
	write(b2cWrite(b2cScopeC, b2cGenC, "intent:c-1",
		b2cFinding("repo:r4", "pkg:npm/p6", "CVE-2026-0006", "", "6.0.0"),
		b2cFinding("repo:r5", "pkg:npm/p7", "CVE-2026-0007", "", "7.0.0")))
	write(b2cWrite(b2cScopeC, b2cGenC, "intent:c-2", b2cFinding("repo:r5", "pkg:npm/p7", "CVE-2026-0007", "", "7.0.0")))

	// Alert and consumption facts get ids that interleave with the impact fact
	// ids, so the merged statement's order is tested, not a concatenation.
	impactIDs := b2cImpactFactIDs(t, ctx, db)
	if len(impactIDs) < 4 {
		t.Fatalf("seeded %d impact facts, want at least 4", len(impactIDs))
	}
	store := NewFactStore(SQLDB{DB: db})
	if err := store.UpsertFacts(ctx, []facts.Envelope{
		{
			FactID: impactIDs[0] + "-alert", ScopeID: b2cAlertScope, GenerationID: b2cGenA, FactKind: facts.SecurityAlertRepositoryAlertFactKind,
			StableFactKey: "alert:1", SchemaVersion: "1.0.0", CollectorKind: "synthetic", SourceConfidence: "reported", ObservedAt: now,
			Payload: map[string]any{"repository_id": "repo:r1", "package_id": "pkg:npm/p1", "cve_ids": []any{"CVE-2026-0001"}},
		},
		{
			FactID: impactIDs[len(impactIDs)/2] + "-consumption", ScopeID: b2cAlertScope, GenerationID: b2cGenA, FactKind: "reducer_package_consumption_correlation",
			StableFactKey: "consumption:1", SchemaVersion: "1.0.0", CollectorKind: "synthetic", SourceConfidence: "inferred", ObservedAt: now,
			Payload: map[string]any{"repository_id": "repo:r2", "package_id": "pkg:npm/p1"},
		},
		{
			FactID: impactIDs[len(impactIDs)-1] + "-alert2", ScopeID: b2cAlertScope, GenerationID: b2cGenA, FactKind: facts.SecurityAlertRepositoryAlertFactKind,
			StableFactKey: "alert:2", SchemaVersion: "1.0.0", CollectorKind: "synthetic", SourceConfidence: "reported", ObservedAt: now,
			Payload: map[string]any{"repository_id": "repo:r1", "package_id": "pkg:npm/p2", "ghsa_ids": []any{"GHSA-2222"}},
		},
	}); err != nil {
		t.Fatalf("seed alert facts: %v", err)
	}
}

// b2cImpactFactIDs lists the live impact fact ids in the database's order.
// Since PR-C2 the production writer stores findings as supports only, so the
// ids read the supports table: one row per live finding.
func b2cImpactFactIDs(t *testing.T, ctx context.Context, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT fact_id FROM supply_chain_impact_finding_supports ORDER BY fact_id`)
	if err != nil {
		t.Fatalf("list impact fact ids: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// b2cOldWriterInsert writes impact finding facts the way a writer of the
// previous release does: straight into fact_records, without the supports
// maintainer's setting, so the fence marks their scope dirty. It plants the
// shapes the writer cannot produce: an operator-scope row, a numeric
// repository id, a blank repository id, and a tombstoned row.
func b2cOldWriterInsert(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	now, _ := time.Parse(time.RFC3339, b2cNow)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ('operator:vulnerability_suppressions', 'operator', 'operator', 'operator:vulnerability_suppressions', 'operator', 'operator:vulnerability_suppressions', $1, $1, 'active', '{}'::jsonb)`, now); err != nil {
		t.Fatalf("insert operator scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('generation:operator', 'operator:vulnerability_suppressions', 'synthetic', $1, $1, 'active', $1, '{}'::jsonb)`, now); err != nil {
		t.Fatalf("insert operator generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = 'generation:operator' WHERE scope_id = 'operator:vulnerability_suppressions'`); err != nil {
		t.Fatalf("activate operator generation: %v", err)
	}
	insert := func(factID, scope, generation string, tombstone bool, payload string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'reducer', 1, 'exact', 'vulnerability_intelligence', $1, $5, $5, $6, $7::jsonb)`,
			factID, scope, generation, facts.ReducerSupplyChainImpactFindingFactKind, now, tombstone, payload); err != nil {
			t.Fatalf("old-writer insert %s: %v", factID, err)
		}
	}
	insert("ow:operator", "operator:vulnerability_suppressions", "generation:operator", false,
		`{"repository_id":"repo:r1","package_id":"pkg:npm/p1","cve_id":"CVE-2026-0001","impact_status":"affected_exact","suppression_state":"suppressed"}`)
	insert("ow:numeric-repo", b2cScopeA, b2cGenA, false,
		`{"repository_id":424242,"package_id":"pkg:npm/p8","cve_id":"CVE-2026-0008","impact_status":"affected_exact"}`)
	insert("ow:blank-repo", b2cScopeA, b2cGenA, false,
		`{"repository_id":"","package_id":"pkg:npm/p9","cve_id":"CVE-2026-0009","advisory_id":"GHSA-9999","impact_status":"affected_exact"}`)
	insert("ow:tombstone", b2cScopeA, b2cGenA, true,
		`{"repository_id":"repo:r1","package_id":"pkg:npm/p1","cve_id":"CVE-2026-0001","impact_status":"affected_exact"}`)
}

// b2cMakeReady drives the reducer-owned supports backfill (RunStep, the
// production loop's own step) until the readiness predicate reads true: the
// writer kept the supports current in its own transactions, so a fresh seed only
// needs the marker, and a seed an old writer touched needs the fence drained too.
// It never writes the marker or the dirty table directly.
func b2cMakeReady(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	backfill := SupplyChainImpactSupportsBackfill{DB: SQLDB{DB: db}}
	for step := 0; step < 300; step++ {
		if _, err := backfill.RunStep(ctx); err != nil {
			t.Fatalf("backfill step: %v", err)
		}
		ready, err := SupplyChainImpactSupportsReady(ctx, SQLDB{DB: db})
		if err != nil {
			t.Fatalf("readiness probe: %v", err)
		}
		if ready {
			return
		}
	}
	t.Fatal("supports backfill did not reach ready")
}

type b2cRow struct {
	FactID  string
	Kind    string
	Payload string
}

func b2cOracleRows(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) []b2cRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("oracle query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []b2cRow
	cols, _ := rows.Columns()
	for rows.Next() {
		var row b2cRow
		switch len(cols) {
		case 2:
			if err := rows.Scan(&row.FactID, &row.Payload); err != nil {
				t.Fatalf("scan: %v", err)
			}
			row.Kind = facts.ReducerSupplyChainImpactFindingFactKind
		default:
			dest := make([]any, len(cols))
			scratch := make([]sql.RawBytes, len(cols))
			for i := range dest {
				dest[i] = &scratch[i]
			}
			if err := rows.Scan(dest...); err != nil {
				t.Fatalf("scan: %v", err)
			}
			row = b2cRow{FactID: string(scratch[0]), Kind: string(scratch[3]), Payload: string(scratch[15])}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	return out
}

// b2cNormalize re-marshals a payload so the comparison is on JSON content, not
// the textual form each read happened to carry.
func b2cNormalize(t *testing.T, payload map[string]any) string {
	t.Helper()
	return fmt.Sprint(payload)
}

func b2cPayloadText(t *testing.T, raw string) string {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("decode oracle payload: %v", err)
	}
	return b2cNormalize(t, decoded)
}

// TestSecurityAlertReconciliationAndAdvisoryLoadersEqualFactReadsLive is the
// #11198 and #11199 equality proof: the new supports reads return the same fact
// ids and payloads as the finding-fact reads on the multi-scope seed, for every
// filter shape.
func TestSecurityAlertReconciliationAndAdvisoryLoadersEqualFactReadsLive(t *testing.T) {
	db, ctx := openPreC2IsolatedLiveDB(t, "b2c_reader_equality",
		workflowControlIntegrationDSNEnv+" is not set; skipping Postgres integration test")
	b2cSeed(t, ctx, db)
	store := NewFactStore(SQLDB{DB: db})

	// Not ready first: both loaders refuse with the retryable error and read
	// nothing, before the backfill marker exists.
	if _, err := store.GetSupplyChainAdvisoriesForRepos(ctx, []string{"repo:r1"}); !errors.As(err, new(*supplychaincore.SupplyChainImpactSupportsNotReadyError)) {
		t.Fatalf("GetSupplyChainAdvisoriesForRepos() before ready error = %v, want *supplychaincore.SupplyChainImpactSupportsNotReadyError", err)
	}
	if _, err := store.ListActiveSecurityAlertReconciliationFacts(ctx,
		securityalert.SecurityAlertReconciliationFactFilter{RepositoryIDs: []string{"repo:r1"}}); !errors.As(err, new(*supplychaincore.SupplyChainImpactSupportsNotReadyError)) {
		t.Fatalf("ListActiveSecurityAlertReconciliationFacts() before ready error = %v, want *supplychaincore.SupplyChainImpactSupportsNotReadyError", err)
	}
	b2cMakeReady(t, ctx, db)

	check := func(t *testing.T) {
		t.Run("advisories for repositories", func(t *testing.T) {
			for name, repos := range map[string][]string{
				"one repository":     {"repo:r1"},
				"two repositories":   {"repo:r1", "repo:r2"},
				"unknown repository": {"repo:nope"},
				"retracted finding":  {"repo:r4"},
				"superseded only":    {"repo:r3"},
				"repo-less excluded": {""},
				"every seeded repo":  {"repo:r1", "repo:r2", "repo:r3", "repo:r4", "repo:r5", "424242"},
				"numeric repo id":    {"424242"},
			} {
				t.Run(name, func(t *testing.T) {
					cleaned := cleanStringFilterValues(repos)
					var want []b2cRow
					if len(cleaned) > 0 {
						want = b2cOracleRows(t, ctx, db, legacyAdvisoriesForReposOracle, cleaned)
					}
					got, err := store.GetSupplyChainAdvisoriesForRepos(ctx, repos)
					if err != nil {
						t.Fatalf("GetSupplyChainAdvisoriesForRepos() error = %v, want nil", err)
					}
					// Compare per repository: the record set the fact read implies.
					wantByRepo := map[string][]servicecatalog.ServiceVulnerabilityRecord{}
					for _, row := range want {
						var payload map[string]any
						if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
							t.Fatalf("decode: %v", err)
						}
						repo := payloadString(payload, "repository_id")
						if repo == "" {
							// The loader skips a blank or non-string repository id.
							continue
						}
						wantByRepo[repo] = append(wantByRepo[repo], supplyChainAdvisoryRecord(facts.Envelope{Payload: payload}))
					}
					if len(cleaned) == 0 {
						wantByRepo = nil
					}
					if !reflect.DeepEqual(normalizeRecords(got), normalizeRecords(wantByRepo)) {
						t.Fatalf("records = %#v, want %#v (the finding-fact read)", got, wantByRepo)
					}
				})
			}
			// Non-vacuity: the oracle returns rows for the populated shapes.
			if rows := b2cOracleRows(t, ctx, db, legacyAdvisoriesForReposOracle, []string{"repo:r1", "repo:r2"}); len(rows) < 4 {
				t.Fatalf("oracle rows for r1,r2 = %d, want at least 4: the seed would prove nothing", len(rows))
			}
		})

		t.Run("reconciliation evidence", func(t *testing.T) {
			oracle := legacyReconciliationOracleQuery(t)
			for name, filter := range map[string]securityalert.SecurityAlertReconciliationFactFilter{
				"repository":      {RepositoryIDs: []string{"repo:r1"}},
				"package":         {PackageIDs: []string{"pkg:npm/p1"}},
				"cve":             {CVEIDs: []string{"CVE-2026-0001"}},
				"ghsa advisory":   {GHSAIDs: []string{"GHSA-2222"}},
				"combined":        {RepositoryIDs: []string{"repo:r2"}, PackageIDs: []string{"pkg:npm/p2"}, CVEIDs: []string{"CVE-2026-0005"}, GHSAIDs: []string{"GHSA-1111"}},
				"unmatched":       {RepositoryIDs: []string{"repo:nope"}},
				"retracted":       {RepositoryIDs: []string{"repo:r4"}},
				"superseded only": {RepositoryIDs: []string{"repo:r3"}, CVEIDs: []string{"CVE-2026-0004"}},
				"numeric repo id": {RepositoryIDs: []string{"424242"}},
				"blank repo id":   {GHSAIDs: []string{"GHSA-9999"}, CVEIDs: []string{"CVE-2026-0009"}},
			} {
				t.Run(name, func(t *testing.T) {
					want := b2cOracleRows(t, ctx, db, oracle,
						filter.RepositoryIDs, filter.PackageIDs, filter.CVEIDs, filter.GHSAIDs, "", 100000)
					got, err := store.ListActiveSecurityAlertReconciliationFacts(ctx, filter)
					if err != nil {
						t.Fatalf("ListActiveSecurityAlertReconciliationFacts() error = %v, want nil", err)
					}
					// The loader drains the fact stream and the supports stream
					// separately, so the equality is per kind, in fact_id order
					// within each kind (the order the handler reads); the fact
					// rows come before the impact rows.
					gotRows, wantRows := map[string][]string{}, map[string][]string{}
					seenImpact := false
					for _, envelope := range got {
						if envelope.FactKind == facts.ReducerSupplyChainImpactFindingFactKind {
							seenImpact = true
						} else if seenImpact {
							t.Fatalf("fact-stream row %s after an impact row: the loader must return the fact stream first", envelope.FactID)
						}
						gotRows[envelope.FactKind] = append(gotRows[envelope.FactKind], envelope.FactID+"|"+b2cNormalize(t, envelope.Payload))
					}
					for _, row := range want {
						wantRows[row.Kind] = append(wantRows[row.Kind], row.FactID+"|"+b2cPayloadText(t, row.Payload))
					}
					if !reflect.DeepEqual(gotRows, wantRows) {
						t.Fatalf("rows differ from the finding-fact read (per kind, fact_id order):\n got: %v\nwant: %v", gotRows, wantRows)
					}
				})
			}
			if rows := b2cOracleRows(t, ctx, db, oracle, []string{"repo:r1"}, []string(nil), []string(nil), []string(nil), "", 100000); len(rows) < 3 {
				t.Fatalf("oracle rows for r1 = %d, want at least 3 (alert plus impact findings): the seed would prove nothing", len(rows))
			}
		})
	}
	t.Run("ready after the backfill", check)

	// A writer of the previous release inserts finding facts after the table was
	// ready: the fence marks their scopes dirty and both loaders refuse again;
	// the backfill drains the marks and the loaders answer, equal to the fact read.
	b2cOldWriterInsert(t, ctx, db)
	if _, err := store.GetSupplyChainAdvisoriesForRepos(ctx, []string{"repo:r1"}); !errors.As(err, new(*supplychaincore.SupplyChainImpactSupportsNotReadyError)) {
		t.Fatalf("GetSupplyChainAdvisoriesForRepos() after an old-writer insert error = %v, want the not-ready refusal", err)
	}
	b2cMakeReady(t, ctx, db)
	t.Run("after an old-writer insert and the drain", check)
}

// normalizeRecords makes a record map comparable: record order within a
// repository is the read's fact id order and is part of the contract, so it is
// kept; an absent key and an empty slice compare equal.
func normalizeRecords(in map[string][]servicecatalog.ServiceVulnerabilityRecord) map[string][]servicecatalog.ServiceVulnerabilityRecord {
	out := map[string][]servicecatalog.ServiceVulnerabilityRecord{}
	for repo, records := range in {
		if len(records) > 0 {
			out[repo] = records
		}
	}
	return out
}
