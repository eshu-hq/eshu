// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestSecurityAlertReconciliationHandlerRefusedBaseStoreRead is the #10193
// store-level proof the reducer-root stub tests cannot give: the manifest
// dependency read runs against a real FactStore on a disposable database
// whose delta-active scope has no full generation at its union-base commit.
// A direct read on that scope takes the union_base_missing refusal path
// (resolveDeltaBases, the refusal counter, the WARN) and returns delta-only
// rows; the same store then feeds the real security-alert reconciliation
// handler, whose narrower name set has no live delta rows, so its own read
// short-circuits before base resolution (reached-scope rule) and returns
// empty, and the handler commits provider_only.
//
// The alert and impact envelopes stay in memory: the thread this answers
// contests only the manifest read half, and seeding those two envelopes
// through SQL would duplicate the reducer-root fixture without testing any
// new path. The writer stays a recording execer for the same reason; its
// upsert-only shape is pinned by the assertions below, not by a live write.
//
// The read half of this chain is additionally pinned by the existing
// manifest overlay proofs (TestListActivePackageManifestDependencyFactsDeltaBaseUnavailableLive
// and the delta-union cases live suite); this test proves the composition,
// that a real refusal reaching the handler still reconciles provider_only.
//
// Live gate: this file is not a *_live_test.go file, so the live-tests
// ledger does not track it. It self-skips without the disposable-database
// DSN pair it shares with the manifest scope-proof live suite:
// ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN (administrative postgres DSN)
// with ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE=1.
func TestSecurityAlertReconciliationHandlerRefusedBaseStoreRead(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN"))
	optIn := strings.TrimSpace(os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE"))
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	logs := &bytes.Buffer{}
	store := NewFactStore(SQLDB{DB: database}).WithInstruments(instruments).
		WithLogger(slog.New(slog.NewTextHandler(logs, nil)))

	repoID := "repo://github/acme/api"
	packageID := "npm://registry.npmjs.org/base-only-lib"
	seedRefusedBaseManifestScope(t, ctx, database, repoID)

	// The refusal engages on a direct read whose name set intersects the
	// delta's live rows: the scope is reached, the base lookup finds no
	// full generation at c0, the read counts union_base_missing, logs the
	// WARN, and returns the delta's rows only.
	direct, err := store.ListActivePackageManifestDependencyFacts(ctx, []string{"npm"}, []string{"delta-lib"})
	if err != nil {
		t.Fatalf("direct manifest read error = %v, want nil (a refused base keeps delta-only rows, it does not fail)", err)
	}
	if len(direct) != 1 || direct[0].FactID != "delta-delta-lib" {
		t.Fatalf("direct manifest read rows = %v, want exactly the delta pin (delta-only, no union leak)", direct)
	}
	// The alert's name has no live delta rows, so the handler's narrower
	// read below short-circuits before base resolution on that call
	// (reached-scope rule) and returns empty. The refusal it rests on is
	// the one proven here.
	narrow, err := store.ListActivePackageManifestDependencyFacts(ctx, []string{"npm"}, []string{"base-only-lib"})
	if err != nil {
		t.Fatalf("narrow manifest read error = %v, want nil", err)
	}
	if len(narrow) != 0 {
		t.Fatalf("narrow manifest read rows = %d, want 0 (the pin lives only in the refused base)", len(narrow))
	}

	loader := &refusedBaseStoreLoader{
		alert:  refusedBaseStoreAlertEnvelope(repoID, packageID),
		impact: refusedBaseStoreImpactEnvelope(repoID, packageID),
		store:  store,
	}
	execer := &refusedBaseStoreExecer{}
	handler := securityalert.SecurityAlertReconciliationHandler{
		FactLoader:                  loader,
		Writer:                      securityalert.PostgresSecurityAlertReconciliationWriter{DB: execer},
		ExtractManifestConsumptions: correlation.ExtractSecurityAlertManifestConsumptions,
	}

	result, err := handler.Handle(ctx, reducercontract.Intent{
		IntentID:     "intent-refused-base-store-read",
		ScopeID:      repoID,
		GenerationID: "security-alert-generation-1",
		SourceSystem: "security_alert",
		Domain:       reducercontract.DomainSecurityAlertReconciliation,
		Cause:        "provider security alert evidence observed",
		AttemptCount: 1,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil: provider_only neither waits nor fails on a refused base (#10193 leave-as-is)", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle() status = %q, want succeeded", result.Status)
	}
	if got := result.CanonicalWrites; got != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 (upsert-only writer, no graph write)", got)
	}
	if !strings.Contains(result.EvidenceSummary, "provider_only=1") {
		t.Fatalf("EvidenceSummary = %q, want provider_only=1", result.EvidenceSummary)
	}

	// Exactly one delta-union decision across both reads above and the
	// handler's own read inside Handle: the missing-base refusal for the
	// union-base commit the delta generation points at. The handler's
	// narrow name set contributes no second decision (reached-scope rule),
	// so a count of 1 also pins that the handler's read short-circuits
	// instead of re-resolving.
	if got, want := refusedBaseStoreUnionOutcomes(t, reader), map[string]int64{"union_base_missing": 1}; !refusedBaseStoreOutcomeEqual(got, want) {
		t.Fatalf("delta-union outcomes = %v, want %v: the refused scope must resolve once, on the read that reaches it", got, want)
	}
	warnLines := refusedBaseStoreWarnings(logs.String(), "manifest dependency read delta base missing")
	if len(warnLines) != 1 || !strings.Contains(warnLines[0], "delta_union_base_commit_sha=c0") {
		t.Fatalf("base-missing WARN lines = %q, want one carrying the refused union-base commit", warnLines)
	}

	// Writer assertion on the resulting rows: exactly one upserted
	// reconciliation fact, no DELETE/retract statement, provider_only with
	// the missing-owned-dependency reason. The recording execer (not a live
	// write) keeps this assertion on the writer's row shape.
	if got, want := len(execer.calls), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d (one batched upsert)", got, want)
	}
	call := execer.calls[0]
	if call.query != factwrite.BatchInsertQuery {
		t.Fatalf("writer query = %q, want the batched fact insert", call.query)
	}
	if strings.Contains(strings.ToUpper(call.query), "DELETE") {
		t.Fatalf("writer query contains DELETE, want upsert-only: %s", call.query)
	}
	rows := refusedBaseStoreDecodedRows(t, call.args)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("decoded rows = %d, want %d (single-alert blast radius)", got, want)
	}
	if got, want := rows[0].factKind, "reducer_security_alert_reconciliation"; got != want {
		t.Fatalf("row FactKind = %q, want %q", got, want)
	}
	var payload map[string]any
	if err := json.Unmarshal(rows[0].payload, &payload); err != nil {
		t.Fatalf("decode row payload: %v", err)
	}
	if got, want := payload["reconciliation_status"], string(securityalert.SecurityAlertReconciliationProviderOnly); got != want {
		t.Fatalf("reconciliation_status = %v, want %q on a refused base (#10193 leave-as-is)", got, want)
	}
	if got, _ := payload["reason"].(string); !strings.Contains(got, "no matching owned dependency evidence") {
		t.Fatalf("reason = %q, want the no-matching-owned-dependency-evidence reason", got)
	}
	if got, ok := payload["impact_wait_outcome"]; ok {
		t.Fatalf("provider_only row carries impact_wait_outcome %v; it never waited on impact evidence", got)
	}
	if got, _ := payload["dependency_evidence_id"].(string); got != "" {
		t.Fatalf("dependency_evidence_id = %q, want empty on a refused base", got)
	}
}

// refusedBaseStoreLoader serves the alert and impact reads from in-memory
// envelopes and delegates only the manifest dependency read to a real
// FactStore. That delegation is the production read the reducer-root stub
// cannot exercise: the delta's rows (and only those rows) reach the
// handler, because the refused base contributes nothing.
type refusedBaseStoreLoader struct {
	alert  facts.Envelope
	impact facts.Envelope
	store  *FactStore
}

func (l *refusedBaseStoreLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return []facts.Envelope{l.alert}, nil
}

func (l *refusedBaseStoreLoader) ListFactsByKind(context.Context, string, string, []string) ([]facts.Envelope, error) {
	return []facts.Envelope{l.alert}, nil
}

func (l *refusedBaseStoreLoader) ListActiveSecurityAlertReconciliationFacts(
	context.Context,
	securityalert.SecurityAlertReconciliationFactFilter,
) ([]facts.Envelope, error) {
	return []facts.Envelope{l.impact}, nil
}

func (l *refusedBaseStoreLoader) ListActiveRepositoryFacts(context.Context) ([]facts.Envelope, error) {
	return nil, nil
}

func (l *refusedBaseStoreLoader) ListActivePackageManifestDependencyFacts(
	ctx context.Context,
	ecosystems []string,
	packageNames []string,
) ([]facts.Envelope, error) {
	return l.store.ListActivePackageManifestDependencyFacts(ctx, ecosystems, packageNames)
}

// refusedBaseStoreAlertEnvelope mirrors the reducer-root refused-base
// fixture: a provider alert whose package pin lives only in the delta's
// refused base full generation.
func refusedBaseStoreAlertEnvelope(repoID, packageID string) facts.Envelope {
	return facts.Envelope{
		FactID:           "alert-base-only",
		ScopeID:          repoID,
		GenerationID:     "security-alert-generation-1",
		FactKind:         facts.SecurityAlertRepositoryAlertFactKind,
		SchemaVersion:    facts.SecurityAlertSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"repository_id":         repoID,
			"provider":              "github_dependabot",
			"provider_alert_number": int64(93),
			"provider_state":        "open",
			"package_id":            packageID,
			"package_name":          "base-only-lib",
			"ecosystem":             "npm",
			"manifest_path":         "package-lock.json",
			"cve_ids":               []string{"CVE-2026-10193"},
			"ghsa_ids":              []string{"GHSA-synthetic-10193"},
			"vulnerable_range":      "<1.0.1",
			"patched_version":       "1.0.1",
		},
	}
}

// refusedBaseStoreImpactEnvelope mirrors the reducer-root refused-base
// fixture: an exact impact finding for the alert's advisory, so owned
// dependency evidence is the only missing half.
func refusedBaseStoreImpactEnvelope(repoID, packageID string) facts.Envelope {
	return facts.Envelope{
		FactID:       "impact-base-only",
		ScopeID:      repoID,
		GenerationID: "security-alert-generation-1",
		FactKind:     facts.ReducerSupplyChainImpactFindingFactKind,
		ObservedAt:   time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"repository_id": repoID,
			"package_id":    packageID,
			"cve_id":        "CVE-2026-10193",
			"advisory_id":   "GHSA-abcd-1234",
			"impact_status": "affected_exact",
		},
	}
}

// seedRefusedBaseManifestScope seeds one delta-active repository scope whose
// delta generation points at union-base commit c0 with no full generation
// ever projected there, so the base lookup refuses with union_base_missing.
// The delta carries only its changed pin (delta-lib); the base-only-lib pin
// the alert needs lives nowhere in the database. The active generation is
// the delta, the way ingestion Ack leaves it. Column order mirrors the
// delta-manifest live fixture.
func seedRefusedBaseManifestScope(t *testing.T, ctx context.Context, database *sql.DB, scopeID string) {
	t.Helper()
	exec := func(what, statement string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, statement, args...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	exec("seed scope "+scopeID, `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind,
     partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1,
        clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)`, scopeID)
	exec("seed delta generation", `
INSERT INTO scope_generations
    (scope_id, generation_id, trigger_kind, is_delta, source_commit_sha,
     delta_baseline_commit_sha, delta_union_base_commit_sha,
     observed_at, ingested_at, status)
VALUES ($1, 'delta-1', 'sync', TRUE, 'c1', 'c0', 'c0',
        clock_timestamp(), clock_timestamp(), 'pending')`, scopeID)
	exec("seed delta repository fact", `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('delta-1:repository', $1, 'delta-1', 'repository', 'repository:' || $1,
        'git', 'repository:' || $1, clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object(
            'repo_id', $1::text,
            'path', '/repos/' || $1,
            'delta_generation', TRUE,
            'delta_relative_paths', to_jsonb(ARRAY['package-lock.json']),
            'delta_deleted_relative_paths', to_jsonb(ARRAY[]::text[])))`, scopeID)
	metadata, err := json.Marshal(map[string]any{
		"config_kind":       "dependency",
		"package_manager":   "npm",
		"version":           "9.9.9",
		"value":             "9.9.9",
		"section":           "package-lock",
		"lockfile":          true,
		"dependency_path":   []string{"root", "delta-lib"},
		"dependency_depth":  2,
		"direct_dependency": false,
	})
	if err != nil {
		t.Fatalf("marshal delta pin metadata: %v", err)
	}
	exec("seed delta pin", `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('delta-delta-lib', $1, 'delta-1', 'content_entity', 'content_entity:' || $1 || ':delta-lib',
        'git', 'content_entity:' || $1 || ':delta-lib',
        clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object(
            'repo_id', $1::text,
            'repo_name', 'api',
            'relative_path', 'package-lock.json',
            'entity_type', 'Variable',
            'entity_name', 'delta-lib',
            'entity_metadata', $2::jsonb))`, scopeID, string(metadata))
	exec("supersede active of "+scopeID, `
UPDATE scope_generations SET status = 'superseded'
WHERE scope_id = $1 AND status = 'active'`, scopeID)
	exec("activate delta-1", `
UPDATE scope_generations SET status = 'active', activated_at = clock_timestamp()
WHERE generation_id = 'delta-1'`)
	exec("point scope "+scopeID, `
UPDATE ingestion_scopes SET active_generation_id = 'delta-1' WHERE scope_id = $1`, scopeID)
}

// refusedBaseStoreExecer records the writer's ExecContext calls the way the
// reducer-root fake execer does, so the row-shape assertions below stay
// comparable across the two suites.
type refusedBaseStoreExecer struct {
	calls []refusedBaseStoreExecCall
}

type refusedBaseStoreExecCall struct {
	query string
	args  []any
}

func (f *refusedBaseStoreExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.calls = append(f.calls, refusedBaseStoreExecCall{query: query, args: args})
	return refusedBaseStoreResult{}, nil
}

type refusedBaseStoreResult struct{}

func (refusedBaseStoreResult) LastInsertId() (int64, error) { return 0, nil }
func (refusedBaseStoreResult) RowsAffected() (int64, error) { return 0, nil }

// refusedBaseStoreDecodedRow is one row recovered from a
// factwrite.BatchInsertQuery call: the fact kind plus the payload bytes,
// which is all the status assertions below need.
type refusedBaseStoreDecodedRow struct {
	factKind string
	payload  []byte
}

func refusedBaseStoreDecodedRows(t *testing.T, args []any) []refusedBaseStoreDecodedRow {
	t.Helper()
	if len(args) != 16 {
		t.Fatalf("batched insert args = %d, want 16", len(args))
	}
	factKinds, ok := args[3].([]string)
	if !ok {
		t.Fatalf("fact_kind arg type = %T, want []string", args[3])
	}
	payloads, ok := args[14].([]string)
	if !ok {
		t.Fatalf("payload arg type = %T, want []string", args[14])
	}
	if len(factKinds) != len(payloads) {
		t.Fatalf("fact kinds = %d, payloads = %d, want equal lengths", len(factKinds), len(payloads))
	}
	rows := make([]refusedBaseStoreDecodedRow, len(factKinds))
	for i := range factKinds {
		rows[i] = refusedBaseStoreDecodedRow{factKind: factKinds[i], payload: []byte(payloads[i])}
	}
	return rows
}

// refusedBaseStoreUnionOutcomes collects the delta-union decision counter by
// outcome, mirroring the delta-manifest live fixture.
func refusedBaseStoreUnionOutcomes(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	outcomes := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_package_manifest_dependency_read_delta_union_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is %T, want an int64 sum", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				outcome, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
				outcomes[outcome.AsString()] += dp.Value
			}
		}
	}
	return outcomes
}

func refusedBaseStoreOutcomeEqual(got, want map[string]int64) bool {
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

func refusedBaseStoreWarnings(logs, message string) []string {
	var lines []string
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "level=WARN") && strings.Contains(line, message) {
			lines = append(lines, line)
		}
	}
	return lines
}
