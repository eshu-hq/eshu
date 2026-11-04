// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9991 disposable PostgreSQL proof that ListActivePackageManifestDependencyFacts overlays a delta-active git scope on its #7797 base full generation: the unchanged manifest pins of the base stay visible to a reopened impact holder pass; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// deltaManifestFixture is one disposable database seeded through the same
// columns the #7797 base rule and overlay read: source_commit_sha,
// activated_at, the delta's baseline and union-base commits, the delta
// repository fact, and each pin's relative_path.
type deltaManifestFixture struct {
	t        *testing.T
	ctx      context.Context
	database *sql.DB
	reader   *sdkmetric.ManualReader
	store    *FactStore
}

// deltaManifestGeneration is one scope_generations row to seed.
type deltaManifestGeneration struct {
	id        string
	isDelta   bool
	commit    string
	baseline  string
	unionBase string
}

// deltaManifestPin is one manifest dependency content_entity row to seed.
type deltaManifestPin struct {
	factID         string
	key            string
	name           string
	path           string
	packageManager string
	version        string
	tombstone      bool
}

func openDeltaManifestFixture(t *testing.T) *deltaManifestFixture {
	t.Helper()
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return &deltaManifestFixture{
		t: t, ctx: ctx, database: database, reader: reader,
		store: NewFactStore(SQLDB{DB: database}).WithInstruments(instruments),
	}
}

func (f *deltaManifestFixture) exec(what, statement string, args ...any) {
	f.t.Helper()
	if _, err := f.database.ExecContext(f.ctx, statement, args...); err != nil {
		f.t.Fatalf("%s: %v", what, err)
	}
}

func (f *deltaManifestFixture) scope(scopeID string) {
	f.t.Helper()
	f.exec("seed scope "+scopeID, `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind,
     partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1,
        clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)`, scopeID)
}

// generation seeds a pending generation; activate makes it the scope's
// active generation the way the ingestion Ack does.
func (f *deltaManifestFixture) generation(scopeID string, g deltaManifestGeneration) {
	f.t.Helper()
	f.exec("seed generation "+g.id, `
INSERT INTO scope_generations
    (scope_id, generation_id, trigger_kind, is_delta, source_commit_sha,
     delta_baseline_commit_sha, delta_union_base_commit_sha,
     observed_at, ingested_at, status)
VALUES ($1, $2, 'sync', $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''),
        clock_timestamp(), clock_timestamp(), 'pending')`,
		scopeID, g.id, g.isDelta, g.commit, g.baseline, g.unionBase)
}

// activate supersedes the scope's current active generation and activates
// generationID, stamping activated_at.
func (f *deltaManifestFixture) activate(scopeID, generationID string) {
	f.t.Helper()
	f.exec("supersede active of "+scopeID, `
UPDATE scope_generations SET status = 'superseded'
WHERE scope_id = $1 AND status = 'active'`, scopeID)
	f.exec("activate "+generationID, `
UPDATE scope_generations SET status = 'active', activated_at = clock_timestamp()
WHERE generation_id = $1`, generationID)
	f.exec("point scope "+scopeID, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, generationID)
}

func (f *deltaManifestFixture) pin(scopeID, generationID string, pin deltaManifestPin) {
	f.t.Helper()
	if pin.packageManager == "" {
		pin.packageManager = "npm"
	}
	f.exec("seed pin "+pin.factID, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, 'content_entity', $4,
        'git', $4, clock_timestamp(), clock_timestamp(), $5,
        jsonb_build_object(
            'repo_id', $2::text,
            'relative_path', $6::text,
            'entity_type', 'Variable',
            'entity_name', $7::text,
            'entity_metadata', jsonb_build_object(
                'config_kind', 'dependency', 'package_manager', $8::text,
                'version', $9::text)))`,
		pin.factID, scopeID, generationID, pin.key, pin.tombstone, pin.path, pin.name,
		pin.packageManager, pin.version)
}

// deltaRepository seeds the delta generation's repository fact the overlay
// parses: delta_generation plus changed and deleted relative paths.
func (f *deltaManifestFixture) deltaRepository(scopeID, generationID string, changed, deleted []string) {
	f.t.Helper()
	f.exec("seed delta repository fact "+generationID, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($2 || ':repository', $1, $2, 'repository', 'repository:' || $1,
        'git', 'repository:' || $1, clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object(
            'repo_id', $1::text,
            'path', '/repos/' || $1,
            'delta_generation', TRUE,
            'delta_relative_paths', to_jsonb($3::text[]),
            'delta_deleted_relative_paths', to_jsonb($4::text[])))`,
		scopeID, generationID, changed, deleted)
}

// load runs the production read and returns the loaded fact ids, failing
// when the rows break the (observed_at, fact_id) ordering contract.
func (f *deltaManifestFixture) load(ecosystems, names []string) ([]string, map[string]string) {
	f.t.Helper()
	loaded, err := f.store.ListActivePackageManifestDependencyFacts(f.ctx, ecosystems, names)
	if err != nil {
		f.t.Fatalf("ListActivePackageManifestDependencyFacts(): %v", err)
	}
	ids := make([]string, 0, len(loaded))
	versions := make(map[string]string, len(loaded))
	for i, envelope := range loaded {
		if i > 0 {
			prev := loaded[i-1]
			if envelope.ObservedAt.Before(prev.ObservedAt) ||
				(envelope.ObservedAt.Equal(prev.ObservedAt) && envelope.FactID < prev.FactID) {
				f.t.Fatalf("loaded rows out of (observed_at, fact_id) order at %d: %s after %s", i, envelope.FactID, prev.FactID)
			}
		}
		ids = append(ids, envelope.FactID)
		if metadata, ok := envelope.Payload["entity_metadata"].(map[string]any); ok {
			versions[envelope.FactID], _ = metadata["version"].(string)
		}
	}
	slices.Sort(ids)
	return ids, versions
}

// unionOutcomes returns the delta-union decision counter by outcome.
func (f *deltaManifestFixture) unionOutcomes() map[string]int64 {
	f.t.Helper()
	var rm metricdata.ResourceMetrics
	if err := f.reader.Collect(context.Background(), &rm); err != nil {
		f.t.Fatalf("collect: %v", err)
	}
	outcomes := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_package_manifest_dependency_read_delta_union_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				f.t.Fatalf("%s is %T, want an int64 sum", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				outcome, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
				outcomes[outcome.AsString()] += dp.Value
			}
		}
	}
	return outcomes
}

func assertDeltaManifestIDs(t *testing.T, got, want []string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("loaded facts = %v, want %v", got, want)
	}
}

func assertDeltaManifestOutcomes(t *testing.T, got, want map[string]int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("delta union outcomes = %v, want %v", got, want)
	}
	for outcome, count := range want {
		if got[outcome] != count {
			t.Fatalf("delta union outcomes = %v, want %v", got, want)
		}
	}
}

// seedDeltaManifestBase seeds scopeID with a full generation at commit c0
// holding alpha (web/package.json) and bravo (api/package.json), activated.
func seedDeltaManifestBase(f *deltaManifestFixture, scopeID, fullID string) {
	f.t.Helper()
	f.scope(scopeID)
	f.generation(scopeID, deltaManifestGeneration{id: fullID, commit: "c0"})
	f.pin(scopeID, fullID, deltaManifestPin{factID: "full-alpha", key: "content_entity:" + scopeID + ":alpha", name: "alpha", path: "web/package.json", version: "1"})
	f.pin(scopeID, fullID, deltaManifestPin{factID: "full-bravo", key: "content_entity:" + scopeID + ":bravo", name: "bravo", path: "api/package.json", version: "1"})
	f.activate(scopeID, fullID)
}

// TestListActivePackageManifestDependencyFactsDeltaActiveScopeLive is the
// #9991 regression. A full generation holds two pins in two manifests (alpha
// in web/package.json, bravo in api/package.json); a delta generation changes
// only web/package.json, re-emits alpha, and becomes the scope's active
// generation. A reopened impact holder pass reads manifest rows through this
// loader, so the unchanged bravo pin must stay visible. Before the fix the
// read joined only the active generation and returned [delta-alpha], so the
// pass would retract bravo's findings.
func TestListActivePackageManifestDependencyFactsDeltaActiveScopeLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedDeltaManifestBase(f, "sidecar-live-scope", "sidecar-live-generation")
	f.generation("sidecar-live-scope", deltaManifestGeneration{
		id: "sidecar-live-delta", isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0",
	})
	f.deltaRepository("sidecar-live-scope", "sidecar-live-delta", []string{"web/package.json"}, nil)
	f.pin("sidecar-live-scope", "sidecar-live-delta", deltaManifestPin{
		factID: "delta-alpha", key: "content_entity:sidecar-live-scope:alpha", name: "alpha", path: "web/package.json", version: "2",
	})
	f.activate("sidecar-live-scope", "sidecar-live-delta")

	got, versions := f.load([]string{"npm"}, []string{"alpha", "bravo"})
	t.Logf("loaded manifest facts for delta-active scope = %v", got)
	assertDeltaManifestIDs(t, got, []string{"delta-alpha", "full-bravo"})
	if versions["delta-alpha"] != "2" {
		t.Fatalf("delta-alpha version = %q, want 2 (the delta's row wins)", versions["delta-alpha"])
	}
	assertDeltaManifestOutcomes(t, f.unionOutcomes(), map[string]int64{"union_base_found": 1})
}
