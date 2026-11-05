// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10006 disposable PostgreSQL proof that the Terraform drift overlay readers merge a delta-active git scope's base full generation: the backend anchor resolves at the live generation and no false removed_from_config candidate appears; reads ESHU_TFSTATE_DRIFT_OVERLAY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_TFSTATE_DRIFT_OVERLAY_PROOF_DISPOSABLE ESHU_TFSTATE_DRIFT_OVERLAY_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/tfstate"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// driftOverlayLiveFixture is one disposable database bootstrapped with the
// production schema, read through a statement-counting FactStore plus the
// tfstate seams wired to the overlay.
type driftOverlayLiveFixture struct {
	t        *testing.T
	ctx      context.Context
	database *sql.DB
	counter  *driftOverlayCounter
	store    *FactStore
	overlay  TerraformDriftOverlay
	reader   *sdkmetric.ManualReader
}

func openDriftOverlayLiveFixture(t *testing.T) *driftOverlayLiveFixture {
	t.Helper()
	dsn := os.Getenv("ESHU_TFSTATE_DRIFT_OVERLAY_PROOF_DSN")
	optIn := os.Getenv("ESHU_TFSTATE_DRIFT_OVERLAY_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	counter := &driftOverlayCounter{db: database}
	store := NewFactStore(counter).WithInstruments(instruments).
		WithLogger(slog.New(slog.NewTextHandler(testLogWriter{t: t}, nil)))
	return &driftOverlayLiveFixture{
		t: t, ctx: ctx, database: database, counter: counter,
		store: store, overlay: TerraformDriftOverlay{Store: store}, reader: reader,
	}
}

// testLogWriter drops store WARN lines from the test output; overlay
// decisions are asserted through metrics and rows, not logs.
type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) { return len(p), nil }

// driftOverlayCounter counts every QueryContext statement (pool and
// snapshot transactions alike) so each phase asserts its exact
// statement mix: the target lookup always runs; the base scan, base
// resolve, retention probe, and overlay inputs run only when a
// baseline-carrying delta-active scope exists.
type driftOverlayCounter struct {
	db *sql.DB

	mu      sync.Mutex
	queries []string
}

func (c *driftOverlayCounter) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	c.mu.Lock()
	c.queries = append(c.queries, query)
	c.mu.Unlock()
	return c.db.QueryContext(ctx, query, args...)
}

func (c *driftOverlayCounter) ExecContext(ctx context.Context, query string, args ...any) (result sql.Result, err error) {
	return c.db.ExecContext(ctx, query, args...)
}

func (c *driftOverlayCounter) BeginReadOnlyRepeatableRead(ctx context.Context) (db.Transaction, error) {
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return &driftOverlayCountingTx{tx: tx, counter: c}, nil
}

func (c *driftOverlayCounter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queries = nil
}

func (c *driftOverlayCounter) statements() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.queries...)
}

// driftOverlayCountingTx counts snapshot statements into the shared
// counter. Commit and Rollback delegate straight through.
type driftOverlayCountingTx struct {
	tx      *sql.Tx
	counter *driftOverlayCounter
}

func (t *driftOverlayCountingTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	t.counter.mu.Lock()
	t.counter.queries = append(t.counter.queries, query)
	t.counter.mu.Unlock()
	return t.tx.QueryContext(ctx, query, args...)
}

func (t *driftOverlayCountingTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}

func (t *driftOverlayCountingTx) Commit() error { return t.tx.Commit() }

func (t *driftOverlayCountingTx) Rollback() error { return t.tx.Rollback() }

// requireStatementMix asserts the overlay statement family presence per
// phase: the delta-target lookup always runs; base scan, base resolve,
// retention probe, and overlay inputs run only for delta-active scopes.
func (f *driftOverlayLiveFixture) requireStatementMix(phase string, wantOverlay bool) {
	f.t.Helper()
	statements := f.counter.statements()
	seenTargets := false
	overlayMarkers := map[string]bool{
		"active_delta":                false,
		"CROSS JOIN LATERAL":          false,
		"generation_retention_events": false,
		"stable_fact_key = ANY":       false,
	}
	for _, query := range statements {
		if strings.Contains(query, "active.is_delta = true") {
			seenTargets = true
		}
		for marker := range overlayMarkers {
			if strings.Contains(query, marker) {
				overlayMarkers[marker] = true
			}
		}
	}
	if !seenTargets {
		f.t.Fatalf("%s: delta-target lookup did not run (%d statements)", phase, len(statements))
	}
	for marker, seen := range overlayMarkers {
		if seen != wantOverlay {
			f.t.Fatalf("%s: overlay marker %q ran=%v, want overlay engaged=%v (%d statements)",
				phase, marker, seen, wantOverlay, len(statements))
		}
	}
	f.t.Logf("%s: %d statements (overlay engaged=%v) %s", phase, len(statements), wantOverlay, driftOverlayFamilyBreakdown(statements))
}

// TestTerraformDriftOverlayReadsLive is the GREEN proof for #10006: the
// same repro the tfstate RED test captures on the active-only methods,
// now read through the overlay entry points wired into the tfstate
// seams. Fixture: config scope repository:repo-drift whose full
// generation holds backend.tf plus main.tf (two resources); state scope
// with an aligned snapshot and two state resources.
//
//   - base (full active): 1 canonical row at gen-full, 0
//     removed_from_config, overlay disengaged (targets only).
//   - delta (delta touching only outputs.tf active): 1 canonical row
//     at gen-delta, 0 removed_from_config, overlay engaged.
//   - delete (delta deleting backend.tf active): 0 canonical rows — a
//     deleted file is not resurrected — and still 0 removed_from_config.
//   - tombstone (delta tombstoning main.tf active): canonical still
//     resolves, and the really-removed addresses surface as 2 TRUE
//     removed_from_config candidates, proving the fix suppresses only
//     the false positives.
func TestTerraformDriftOverlayReadsLive(t *testing.T) {
	f := openDriftOverlayLiveFixture(t)
	ctx := f.ctx
	seedDriftOverlayLiveFixture(t, ctx, f.database)

	const (
		configScope = "repository:repo-drift"
		repoID      = "repo-drift"
	)
	bucket, key := "drift-bucket", "envs/prod.tfstate"
	hash := terraformstate.ScopeLocatorHash(terraformstate.BackendS3, "s3://"+bucket+"/"+key)
	stateScope := "state_snapshot:s3:" + hash

	backendQuery := f.overlay
	loader := tfstate.PostgresDriftEvidenceLoader{DB: f.counter, OverlaidConfig: f.overlay.LoadDriftConfig}

	checkPhase := func(phase, anchorGen string, wantCanonical, wantRemoved int) {
		t.Helper()
		f.counter.reset()
		rows, err := backendQuery.ListTerraformBackendsByLocator(ctx, "s3", hash)
		if err != nil {
			t.Fatalf("%s: ListTerraformBackendsByLocator() error = %v", phase, err)
		}
		if got := len(rows); got != wantCanonical {
			t.Fatalf("%s: backend-canonical rows = %d, want %d", phase, got, wantCanonical)
		}
		for _, row := range rows {
			if row.RepoID != repoID {
				t.Errorf("%s: canonical RepoID = %q, want %q", phase, row.RepoID, repoID)
			}
			if row.CommitID != anchorGen {
				t.Errorf("%s: canonical CommitID = %q, want anchor %q", phase, row.CommitID, anchorGen)
			}
		}
		anchor := tfstatebackend.CommitAnchor{RepoID: repoID, ScopeID: configScope, CommitID: anchorGen}
		addressed, err := loader.LoadDriftEvidence(ctx, stateScope, anchor)
		if err != nil {
			t.Fatalf("%s: LoadDriftEvidence() error = %v", phase, err)
		}
		if got := countRemovedFromConfigCandidates(tfconfigstate.BuildCandidates(addressed, anchor, stateScope)); got != wantRemoved {
			t.Errorf("%s: removed_from_config candidates = %d, want %d", phase, got, wantRemoved)
		}
		f.requireStatementMix(phase, anchorGen != "gen-full")
	}

	checkPhase("base", "gen-full", 1, 0)

	activateDriftOverlayGeneration(t, ctx, f.database, configScope, "gen-full", "gen-delta")
	checkPhase("delta", "gen-delta", 1, 0)

	seedDriftOverlayDelta(t, ctx, f.database, "gen-delta-2", "commit-delta-2",
		`{"repo_id":"repo-drift","local_path":"/repo","delta_generation":true,`+
			`"delta_relative_paths":["outputs.tf"],"delta_deleted_relative_paths":["backend.tf"]}`,
		[]driftOverlayFile{{
			factID: "fact-delta2-outputs", stableKey: "file:repo-drift:outputs.tf", relativePath: "outputs.tf",
			payload: `{"repo_id":"repo-drift","relative_path":"outputs.tf","parsed_file_data":{"terraform_resources":[],"terraform_modules":[]}}`,
		}})
	activateDriftOverlayGeneration(t, ctx, f.database, configScope, "gen-delta", "gen-delta-2")
	checkPhase("delete", "gen-delta-2", 0, 0)

	seedDriftOverlayDelta(t, ctx, f.database, "gen-delta-3", "commit-delta-3",
		`{"repo_id":"repo-drift","local_path":"/repo","delta_generation":true,`+
			`"delta_relative_paths":["main.tf"],"delta_deleted_relative_paths":[]}`,
		[]driftOverlayFile{
			{
				factID: "fact-delta3-outputs", stableKey: "file:repo-drift:outputs.tf", relativePath: "outputs.tf",
				payload: `{"repo_id":"repo-drift","relative_path":"outputs.tf","parsed_file_data":{"terraform_resources":[],"terraform_modules":[]}}`,
			},
			{
				factID: "fact-delta3-main-tombstone", stableKey: "file:repo-drift:main.tf", relativePath: "main.tf", tombstone: true,
				payload: `{"repo_id":"repo-drift","relative_path":"main.tf"}`,
			},
		})
	activateDriftOverlayGeneration(t, ctx, f.database, configScope, "gen-delta-2", "gen-delta-3")
	checkPhase("tombstone", "gen-delta-3", 1, 2)

	f.requireDriftUnionCount("config", "union_base_found", 1)
	f.requireDriftUnionCount("canonical", "union_base_found", 1)
	logDriftOverlayAfterPlans(t, ctx, f.database, configScope)
}

// driftOverlayFamilyBreakdown classifies each statement into the
// overlay family it belongs to, for the handoff statement-count
// evidence: targets (delta-target lookup), base (derived base pages),
// resolve (base-generation lookup), probe (superseded-base retention
// probe), inputs (overlay delta inputs), other (active pages and the
// unchanged state/prior steps).
func driftOverlayFamilyBreakdown(statements []string) string {
	counts := map[string]int{}
	for _, query := range statements {
		family := "other"
		switch {
		case strings.Contains(query, "CROSS JOIN LATERAL"):
			family = "resolve"
		case strings.Contains(query, "generation_retention_events"):
			family = "probe"
		case strings.Contains(query, "stable_fact_key = ANY"):
			family = "inputs"
		case strings.Contains(query, "prior_generations"):
			family = "prior-walk"
		case strings.Contains(query, "terraform_modules") && !strings.Contains(query, "active_generation_id"):
			family = "legacy-modules"
		case strings.Contains(query, "active_delta"):
			family = "base"
		case strings.Contains(query, "active.is_delta = true"):
			family = "targets"
		}
		counts[family]++
	}
	families := []string{"targets", "base", "resolve", "probe", "inputs", "prior-walk", "legacy-modules", "other"}
	var parts []string
	for _, family := range families {
		parts = append(parts, fmt.Sprintf("%s=%d", family, counts[family]))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// countRemovedFromConfigCandidates counts candidates carrying a
// drift_kind evidence atom of removed_from_config.
func countRemovedFromConfigCandidates(candidates []model.Candidate) int {
	count := 0
	for _, candidate := range candidates {
		for _, atom := range candidate.Evidence {
			if atom.EvidenceType == tfconfigstate.EvidenceTypeDriftKind && atom.Value == "removed_from_config" {
				count++
				break
			}
		}
	}
	return count
}

// requireDriftUnionCount asserts the overlay outcome counter reached at
// least want for one read/outcome pair, proving the decisions are
// operator-visible (not only row-visible).
func (f *driftOverlayLiveFixture) requireDriftUnionCount(read, outcome string, want int64) {
	f.t.Helper()
	var metrics metricdata.ResourceMetrics
	if err := f.reader.Collect(f.ctx, &metrics); err != nil {
		f.t.Fatalf("collect metrics: %v", err)
	}
	var got int64
	for _, scope := range metrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != "eshu_dp_terraform_drift_read_delta_union_total" {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				values := map[string]string{}
				for _, attr := range point.Attributes.ToSlice() {
					values[string(attr.Key)] = attr.Value.String()
				}
				if values["read"] == read && values["outcome"] == outcome {
					got += int64(point.Value)
				}
			}
		}
	}
	if got < want {
		var dump []string
		for _, scope := range metrics.ScopeMetrics {
			for _, metric := range scope.Metrics {
				if !strings.Contains(metric.Name, "drift_read_delta_union") {
					continue
				}
				dump = append(dump, metric.Name+": "+describeMetricData(metric.Data))
			}
		}
		f.t.Fatalf("delta_union_total{read=%q,outcome=%q} = %d, want >= %d\ndump:\n%s",
			read, outcome, got, want, strings.Join(dump, "\n"))
	}
	f.t.Logf("delta_union_total{read=%q,outcome=%q} = %d", read, outcome, got)
}

// logDriftOverlayAfterPlans logs EXPLAIN (ANALYZE, BUFFERS) for the
// active and derived-base overlay texts on the delta-active fixture:
// the AFTER half of the #10006 plan evidence. Only headline lines are
// logged; the shape stays greppable.
func logDriftOverlayAfterPlans(t *testing.T, ctx context.Context, database *sql.DB, configScope string) {
	t.Helper()
	plan := func(what, query string, args ...any) {
		t.Helper()
		rows, err := database.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+query, args...)
		if err != nil {
			t.Fatalf("EXPLAIN %s: %v", what, err)
		}
		defer func() { _ = rows.Close() }()
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan EXPLAIN %s: %v", what, err)
			}
			lines = append(lines, line)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate EXPLAIN %s: %v", what, err)
		}
		var kept []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "Execution Time") || strings.Contains(trimmed, "Buffers:") ||
				(!strings.HasPrefix(trimmed, "->") && !strings.HasPrefix(trimmed, "Planning")) {
				kept = append(kept, trimmed)
			}
		}
		t.Logf("AFTER plan %s:\n%s", what, strings.Join(kept, "\n"))
	}
	scopes := []string{configScope}
	plan("canonical-active", tfstate.ActiveTerraformBackendCanonicalQuery)
	canonicalBase, err := deriveDeltaOverlayBaseQuery(tfstate.ActiveTerraformBackendCanonicalQuery, 1)
	if err != nil {
		t.Fatalf("derive canonical base: %v", err)
	}
	plan("canonical-base", canonicalBase, scopes)
	plan("config-active", tfstate.ActiveConfigResourcesForOverlayQuery, configScope)
	configBase, err := deriveDeltaOverlayBaseQuery(tfstate.ActiveConfigResourcesForOverlayQuery, 2)
	if err != nil {
		t.Fatalf("derive config base: %v", err)
	}
	plan("config-base", configBase, configScope, scopes)
}
