// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// dsnForSecurityAlertKeyedProof returns the live Postgres DSN for the
// security-alert keyed-reader differential proof, or "" to skip it.
func dsnForSecurityAlertKeyedProof(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("ESHU_SECURITY_ALERT_KEYED_PROOF_DSN"); dsn != "" {
		return dsn
	}
	return ""
}

// TestListActiveSecurityAlertReconciliationFactsKeyedEquivalentToLegacy is
// the live bidirectional differential proof for the keyed security-alert
// reader (issue #10875): with version-1-style sidecar rows written by the
// real writer and the backfill marker present, the keyed EXISTS query must
// return exactly the legacy six-arm query's set — no dropped facts, no
// paging-corrupting duplicates.
func TestListActiveSecurityAlertReconciliationFactsKeyedEquivalentToLegacy(t *testing.T) {
	dsn := dsnForSecurityAlertKeyedProof(t)
	if dsn == "" {
		t.Skip("set ESHU_SECURITY_ALERT_KEYED_PROOF_DSN to run the security-alert keyed-reader differential proof")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	schema := fmt.Sprintf("security_alert_keyed_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	execAll := func(statements ...string) {
		t.Helper()
		for _, statement := range statements {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				t.Fatalf("proof ddl: %v\n%s", err, statement)
			}
		}
	}
	execAll(
		`CREATE TABLE ingestion_scopes (scope_id TEXT PRIMARY KEY, active_generation_id TEXT)`,
		`CREATE TABLE scope_generations (scope_id TEXT NOT NULL, generation_id TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active', PRIMARY KEY (scope_id, generation_id))`,
		`CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY, scope_id TEXT NOT NULL, generation_id TEXT NOT NULL,
    fact_kind TEXT NOT NULL, stable_fact_key TEXT NOT NULL DEFAULT '',
    schema_version TEXT NOT NULL DEFAULT '0', collector_kind TEXT NOT NULL DEFAULT 'u',
    fencing_token BIGINT NOT NULL DEFAULT 0, source_confidence TEXT NOT NULL DEFAULT 'u',
    source_system TEXT NOT NULL DEFAULT 'git', source_fact_key TEXT NOT NULL DEFAULT '',
    source_uri TEXT NULL, source_record_id TEXT NULL, observed_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE, payload JSONB NOT NULL DEFAULT '{}'::jsonb
)`,
		`CREATE TABLE security_alert_reconciliation_keys (
    fact_id TEXT NOT NULL, scope_id TEXT NOT NULL, generation_id TEXT NOT NULL,
    key_kind TEXT NOT NULL, key_value TEXT NOT NULL,
    PRIMARY KEY (fact_id, key_kind, key_value)
)`,
		`CREATE TABLE security_alert_reconciliation_key_backfill_markers (marker_name TEXT PRIMARY KEY, completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ('scope-1', 'gen-1')`,
		`INSERT INTO scope_generations (scope_id, generation_id, status) VALUES ('scope-1', 'gen-1', 'active')`,
	)

	seed := func(factID, kind string, tombstone bool, payload string) {
		t.Helper()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, observed_at, is_tombstone, payload)
VALUES ($1, 'scope-1', 'gen-1', $2, NOW(), $3, $4::jsonb)`,
			factID, kind, tombstone, payload); err != nil {
			t.Fatalf("seed fact %q: %v", factID, err)
		}
	}
	// Matched by repository arm (verbatim, padded value stored but matched on exact).
	seed("fact-repo", "security_alert.repository_alert", false, `{"repository_id": "acme/app"}`)
	// Matched by package arm + scalar cve arm.
	seed("fact-pkg", "reducer_package_consumption_correlation", false, `{"package_id": "pkg:npm/left-pad", "cve_id": "CVE-2024-0001"}`)
	// Matched by cve_ids array arm + advisory arm (scalar + array union).
	seed("fact-vuln", "reducer_supply_chain_impact_finding", false, `{"cve_ids": ["CVE-2024-0002"], "ghsa_ids": ["GHSA-1"], "advisory_id": "GHSA-2"}`)
	// Object-shaped cve_ids: ?| matches key names; must survive the conversion.
	seed("fact-obj", "reducer_supply_chain_impact_finding", false, `{"cve_ids": {"CVE-2024-0003": {}}}`)
	// Non-string array element: ?| never matches it under either form.
	seed("fact-num-arr", "reducer_supply_chain_impact_finding", false, `{"cve_ids": [20240002]}`)
	// Noise: eligible kind but no matching identifier.
	seed("fact-noise", "security_alert.repository_alert", false, `{"repository_id": "someone/else"}`)
	// Wrong kind: identifiers present but the load never reads it.
	seed("fact-kind", "content", false, `{"repository_id": "acme/app"}`)
	// Tombstone with identifiers: excluded by both forms.
	seed("fact-tomb", "security_alert.repository_alert", true, `{"repository_id": "acme/app"}`)
	// Numeric scalar id: the legacy arm reads payload->>'repository_id',
	// which renders the JSON number as text, so the "12345" filter matches.
	seed("fact-num-repo", "security_alert.repository_alert", false, `{"repository_id": 12345}`)
	// Boolean scalar id: ->> renders true as text, so the "true" filter matches.
	seed("fact-bool-pkg", "reducer_package_consumption_correlation", false, `{"package_id": true}`)
	// Numeric scalar cve id: ->> renders the JSON number as text.
	seed("fact-num-cve", "reducer_supply_chain_impact_finding", false, `{"cve_id": 20240001}`)

	envelopes := []facts.Envelope{
		{FactID: "fact-repo", ScopeID: "scope-1", GenerationID: "gen-1", FactKind: "security_alert.repository_alert", Payload: map[string]any{"repository_id": "acme/app"}},
		{FactID: "fact-pkg", ScopeID: "scope-1", GenerationID: "gen-1", FactKind: "reducer_package_consumption_correlation", Payload: map[string]any{"package_id": "pkg:npm/left-pad", "cve_id": "CVE-2024-0001"}},
		{FactID: "fact-vuln", ScopeID: "scope-1", GenerationID: "gen-1", FactKind: "reducer_supply_chain_impact_finding", Payload: map[string]any{"cve_ids": []any{"CVE-2024-0002"}, "ghsa_ids": []any{"GHSA-1"}, "advisory_id": "GHSA-2"}},
		{FactID: "fact-obj", ScopeID: "scope-1", GenerationID: "gen-1", FactKind: "reducer_supply_chain_impact_finding", Payload: map[string]any{"cve_ids": map[string]any{"CVE-2024-0003": map[string]any{}}}},
		{FactID: "fact-noise", ScopeID: "scope-1", GenerationID: "gen-1", FactKind: "security_alert.repository_alert", Payload: map[string]any{"repository_id": "someone/else"}},
		{FactID: "fact-kind", ScopeID: "scope-1", GenerationID: "gen-1", FactKind: "content", Payload: map[string]any{"repository_id": "acme/app"}},
		{FactID: "fact-tomb", ScopeID: "scope-1", GenerationID: "gen-1", FactKind: "security_alert.repository_alert", IsTombstone: true, Payload: map[string]any{"repository_id": "acme/app"}},
		{FactID: "fact-num-repo", ScopeID: "scope-1", GenerationID: "gen-1", FactKind: "security_alert.repository_alert", Payload: map[string]any{"repository_id": float64(12345)}},
		{FactID: "fact-bool-pkg", ScopeID: "scope-1", GenerationID: "gen-1", FactKind: "reducer_package_consumption_correlation", Payload: map[string]any{"package_id": true}},
		{FactID: "fact-num-cve", ScopeID: "scope-1", GenerationID: "gen-1", FactKind: "reducer_supply_chain_impact_finding", Payload: map[string]any{"cve_id": float64(20240001)}},
		{FactID: "fact-num-arr", ScopeID: "scope-1", GenerationID: "gen-1", FactKind: "reducer_supply_chain_impact_finding", Payload: map[string]any{"cve_ids": []any{float64(20240002)}}},
	}
	if err := refreshSecurityAlertReconciliationKeys(ctx, SQLDB{DB: db}, envelopes); err != nil {
		t.Fatalf("seed security alert keys with the production writer: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO security_alert_reconciliation_key_backfill_markers (marker_name) VALUES ($1)", SecurityAlertReconciliationKeyBackfillMarker); err != nil {
		t.Fatalf("insert ready marker: %v", err)
	}

	filter := securityalert.SecurityAlertReconciliationFactFilter{
		RepositoryIDs: []string{"acme/app", "12345"},
		PackageIDs:    []string{"pkg:npm/left-pad", "true"},
		CVEIDs:        []string{"CVE-2024-0001", "CVE-2024-0002", "CVE-2024-0003", "20240001"},
		GHSAIDs:       []string{"GHSA-1", "GHSA-2"},
	}
	load := func(query string) []string {
		t.Helper()
		rows, err := db.QueryContext(ctx, query,
			array.StringArray(filter.RepositoryIDs), array.StringArray(filter.PackageIDs),
			array.StringArray(filter.CVEIDs), array.StringArray(filter.GHSAIDs),
			"", listFactsByKindPageSize)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var ids []string
		for rows.Next() {
			var id string
			cols := make([]any, 16)
			cols[0] = &id
			for i := 1; i < 16; i++ {
				var v any
				cols[i] = &v
			}
			if err := rows.Scan(cols...); err != nil {
				t.Fatalf("scan: %v", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		sort.Strings(ids)
		return ids
	}
	legacy := load(listActiveSecurityAlertReconciliationFactsQuery)
	keyed := load(listActiveSecurityAlertReconciliationFactsKeyedQuery)
	want := []string{"fact-bool-pkg", "fact-num-cve", "fact-num-repo", "fact-obj", "fact-pkg", "fact-repo", "fact-vuln"}
	if fmt.Sprint(legacy) != fmt.Sprint(want) {
		t.Fatalf("legacy set = %v, want %v", legacy, want)
	}
	if fmt.Sprint(keyed) != fmt.Sprint(legacy) {
		t.Fatalf("keyed set = %v diverges from legacy set = %v", keyed, legacy)
	}
}
