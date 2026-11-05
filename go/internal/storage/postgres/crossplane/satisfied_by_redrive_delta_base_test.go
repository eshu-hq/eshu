// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanestore_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossplane"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/crossplane"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// crossplaneRedriveDeltaRow is one K8sResource claim row a delta generation
// carries. stableKey is the row's stable_fact_key: a delta row whose key
// equals a base claim's key overrides it, and a tombstone deletes it.
type crossplaneRedriveDeltaRow struct {
	factID    string
	stableKey string
	group     string
	kind      string
	tombstone bool
}

// seedCrossplaneRedriveDelta activates a git delta generation over
// scopeID's full generation the way the ingestion Ack does. The full
// generation is stamped at commit c0 and superseded; the delta is at c1 and
// records baseline and union base c0, so its #7797 base is the full
// generation. An empty baseline seeds a legacy delta, which has no base.
// The delta carries its delta_generation repository fact, changing
// deploy/other.yaml, plus rows.
func seedCrossplaneRedriveDelta(
	ctx context.Context, t *testing.T, conn crossplaneRedriveExecer,
	scopeID, deltaGenerationID, baseline string, now time.Time, rows ...crossplaneRedriveDeltaRow,
) {
	t.Helper()
	crossplaneRedriveMustExec(ctx, t, conn, `
		UPDATE scope_generations SET source_commit_sha = 'c0', status = 'superseded'
		WHERE scope_id = $1 AND status = 'active'
	`, scopeID)
	crossplaneRedriveMustExec(ctx, t, conn, `
		INSERT INTO scope_generations
		    (generation_id, scope_id, trigger_kind, is_delta, source_commit_sha,
		     delta_baseline_commit_sha, delta_union_base_commit_sha,
		     observed_at, ingested_at, status, activated_at, payload)
		VALUES ($1, $2, 'sync', TRUE, 'c1', NULLIF($3, ''), NULLIF($3, ''), $4, $4, 'active', $4, '{}'::jsonb)
	`, deltaGenerationID, scopeID, baseline, now)
	crossplaneRedriveMustExec(ctx, t, conn, `
		UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
	`, scopeID, deltaGenerationID)
	crossplaneRedriveMustExec(ctx, t, conn, `
		INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		VALUES ($2 || ':repository', $1, $2, 'repository', 'repository:' || $1, 'git', 'repository:' || $1, $3, $3, FALSE,
		        jsonb_build_object('repo_id', $1::text, 'delta_generation', TRUE,
		            'delta_relative_paths', jsonb_build_array('deploy/other.yaml'),
		            'delta_deleted_relative_paths', '[]'::jsonb))
	`, scopeID, deltaGenerationID, now)
	for _, row := range rows {
		crossplaneRedriveMustExec(ctx, t, conn, `
			INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
			VALUES ($1, $2, $3, 'content_entity', $4, 'git', $4, $5, $5, $6,
			        jsonb_build_object('entity_type', 'K8sResource', 'entity_id', $4 || '-uid',
			            'entity_metadata', jsonb_build_object('api_version', $7::text || '/v1', 'kind', $8::text)))
		`, row.factID, scopeID, deltaGenerationID, row.stableKey, now, row.tombstone, row.group, row.kind)
	}
}

// crossplaneRedriveRecordingReplayer records every scheduling call the
// sweep makes before delegating to the real ReducerQueue. onReplay, when
// set, runs before the delegate for one target scope.
type crossplaneRedriveRecordingReplayer struct {
	next     crossplanestore.CrossplaneRedriveIntentReplayer
	mu       sync.Mutex
	calls    []string
	onReplay func(scopeID string)
}

func (r *crossplaneRedriveRecordingReplayer) ReplayCrossplaneSatisfiedByMaterialization(
	ctx context.Context, scopeID, generationID string,
) (bool, error) {
	r.mu.Lock()
	r.calls = append(r.calls, scopeID+"@"+generationID)
	hook := r.onReplay
	r.mu.Unlock()
	if hook != nil {
		hook(scopeID)
	}
	return r.next.ReplayCrossplaneSatisfiedByMaterialization(ctx, scopeID, generationID)
}

// scheduled returns the recorded calls whose scope id starts with prefix,
// and clears every recorded call.
func (r *crossplaneRedriveRecordingReplayer) scheduled(prefix string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, call := range r.calls {
		if strings.HasPrefix(call, prefix) {
			out = append(out, call)
		}
	}
	r.calls = nil
	return out
}

// crossplaneRedriveStatementCounter counts the delta-base discovery
// statements (the derived text, which joins the active_delta alias) the
// collaborator runs.
type crossplaneRedriveStatementCounter struct {
	db.ExecQueryer
	mu   sync.Mutex
	base int
}

func (c *crossplaneRedriveStatementCounter) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if strings.Contains(query, "crossplane_satisfied_by_redrive_target_ledger") && strings.Contains(query, "active_delta") {
		c.mu.Lock()
		c.base++
		c.mu.Unlock()
	}
	return c.ExecQueryer.QueryContext(ctx, query, args...)
}

func (c *crossplaneRedriveStatementCounter) baseStatements() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.base
}

// crossplaneRedriveDeltaBaseHarness is one disposable database shared by
// every #10094 case, with the sweeper under test, its recording replayer,
// and its telemetry reader.
type crossplaneRedriveDeltaBaseHarness struct {
	db       *sql.DB
	replayer *crossplaneRedriveRecordingReplayer
	counter  *crossplaneRedriveStatementCounter
	reader   *sdkmetric.ManualReader
	sweeper  crossplanestore.CrossplaneSatisfiedByRedriveSweeper
}

func newCrossplaneRedriveDeltaBaseHarness(t *testing.T) *crossplaneRedriveDeltaBaseHarness {
	t.Helper()
	database, _ := crossplaneRedriveProofDB(t)
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	h := &crossplaneRedriveDeltaBaseHarness{
		db:      database,
		reader:  reader,
		counter: &crossplaneRedriveStatementCounter{ExecQueryer: postgres.SQLDB{DB: database}},
		replayer: &crossplaneRedriveRecordingReplayer{
			next: postgres.NewReducerQueue(postgres.SQLDB{DB: database}, "delta-base-proof", time.Minute),
		},
	}
	h.sweeper = crossplanestore.CrossplaneSatisfiedByRedriveSweeper{
		DB:          postgres.SQLQueryer{DB: database},
		State:       crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: database}),
		Replayer:    h.replayer,
		DeltaBase:   postgres.NewFactStore(h.counter),
		Owner:       "delta-base-proof",
		Instruments: instruments,
	}
	return h
}

// sweep activates an XRD generation for the case's identity, runs the real
// Sweep for it, and returns the case's scheduled targets as scope@generation.
func (h *crossplaneRedriveDeltaBaseHarness) sweep(ctx context.Context, t *testing.T, prefix, group string, now time.Time) []string {
	t.Helper()
	xrdScopeID, xrdGenerationID := prefix+"-xrd", prefix+"-xrd-gen-1"
	seedCrossplaneRedriveXRD(ctx, t, h.db, xrdScopeID, xrdGenerationID, group, crossplaneRedriveDeltaKind, now)
	result, err := h.sweeper.Sweep(ctx, xrdScopeID, xrdGenerationID)
	if err != nil {
		t.Fatalf("Sweep(%s): %v", xrdGenerationID, err)
	}
	if result.Outcome != crossplanestore.CrossplaneRedriveOutcomeCompletedForTest {
		t.Fatalf("Sweep(%s) outcome = %q, want completed (%+v)", xrdGenerationID, result.Outcome, result)
	}
	return h.replayer.scheduled(prefix + "-")
}

// targetsEnqueued returns eshu_dp_crossplane_redrive_targets_enqueued_total
// by its read label.
func (h *crossplaneRedriveDeltaBaseHarness) targetsEnqueued(t *testing.T) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_crossplane_redrive_targets_enqueued_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is %T, want an int64 sum", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				read, _ := dp.Attributes.Value(telemetry.MetricDimensionRead)
				out[read.AsString()] += dp.Value
			}
		}
	}
	return out
}

// crossplaneRedriveDeltaKind is the claim kind of every #10094 case; each
// case owns its own group, so one case's XRD never matches another's claims.
const crossplaneRedriveDeltaKind = "XDeltaClaim"

func assertCrossplaneRedriveScheduled(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("scheduled targets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scheduled targets = %v, want %v", got, want)
		}
	}
}

// testCrossplaneRedriveDeltaBaseClaimDiscoveredLive is the #10094
// regression through the real CrossplaneSatisfiedByRedriveSweeper.Sweep, run
// as the delta_base_leg subtest of the scheduled
// TestCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive.
// The redrive discovery joined only each claim scope's active generation,
// so a claim that lives only in the base full generation of a delta-active
// scope (the delta did not touch its file) was never re-driven when its XRD
// activated in another repository: that scope kept no SATISFIED_BY edge
// until its next commit. The cases share one database, run in order, and
// each owns its scopes and XRD identity.
func testCrossplaneRedriveDeltaBaseClaimDiscoveredLive(t *testing.T) {
	h := newCrossplaneRedriveDeltaBaseHarness(t)
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("full-active claim, no delta-active scope runs no base statement", func(t *testing.T) {
		seedCrossplaneRedriveClaimScope(ctx, t, h.db, "full-app", "full-app-gen", "full.example.org", crossplaneRedriveDeltaKind, 2, now)
		got := h.sweep(ctx, t, "full", "full.example.org", now)
		assertCrossplaneRedriveScheduled(t, got, "full-app@full-app-gen")
		if n := h.counter.baseStatements(); n != 0 {
			t.Fatalf("delta-base discovery statements = %d with no delta-active scope, want 0", n)
		}
	})

	t.Run("claim only in the base of an untouched delta", func(t *testing.T) {
		const group = "base.example.org"
		seedCrossplaneRedriveClaimScope(ctx, t, h.db, "base-app", "base-app-full", group, crossplaneRedriveDeltaKind, 1, now)
		seedCrossplaneRedriveDelta(ctx, t, h.db, "base-app", "base-app-delta", "c0", now)
		before := h.targetsEnqueued(t)
		got := h.sweep(ctx, t, "base", group, now)
		assertCrossplaneRedriveScheduled(t, got, "base-app@base-app-delta")
		after := h.targetsEnqueued(t)
		if delta := after["delta_base"] - before["delta_base"]; delta != 1 {
			t.Fatalf("targets enqueued by the delta_base read = %d, want 1 (counter %v)", delta, after)
		}

		// The scheduled intent heals the edge: the handler reads the
		// delta overlaid on its base and resolves the base claim.
		spy := &crossplaneRedriveSpyEdgeWriter{}
		handler := crossplane.CrossplaneSatisfiedByMaterializationHandler{
			FactLoader: postgres.NewFactStore(postgres.SQLDB{DB: h.db}), EdgeWriter: spy,
		}
		if _, err := handler.Handle(ctx, reducercontract.Intent{
			IntentID: "intent-base-app", ScopeID: "base-app", GenerationID: "base-app-delta",
			Domain: reducer.DomainCrossplaneSatisfiedByMaterialization, AttemptCount: 1,
		}); err != nil {
			t.Fatalf("Handle(base-app-delta): %v", err)
		}
		if len(spy.written) != 1 || spy.written[0]["claim_uid"] != "base-app-uid-0" {
			t.Fatalf("SATISFIED_BY rows = %v, want the base claim base-app-uid-0", spy.written)
		}
	})

	t.Run("delta deletes the claim: discovered, handler writes no edge", func(t *testing.T) {
		const group = "deleted.example.org"
		seedCrossplaneRedriveClaimScope(ctx, t, h.db, "deleted-app", "deleted-app-full", group, crossplaneRedriveDeltaKind, 1, now)
		seedCrossplaneRedriveDelta(ctx, t, h.db, "deleted-app", "deleted-app-delta", "c0", now, crossplaneRedriveDeltaRow{
			factID: "deleted-app-tombstone", stableKey: "deleted-app-0", group: group, kind: crossplaneRedriveDeltaKind, tombstone: true,
		})
		// Discovery is over-inclusive by design (no overlay): the handler is
		// the only truth writer and reads the overlaid snapshot.
		got := h.sweep(ctx, t, "deleted", group, now)
		assertCrossplaneRedriveScheduled(t, got, "deleted-app@deleted-app-delta")
		spy := &crossplaneRedriveSpyEdgeWriter{}
		handler := crossplane.CrossplaneSatisfiedByMaterializationHandler{
			FactLoader: postgres.NewFactStore(postgres.SQLDB{DB: h.db}), EdgeWriter: spy,
		}
		if _, err := handler.Handle(ctx, reducercontract.Intent{
			IntentID: "intent-deleted-app", ScopeID: "deleted-app", GenerationID: "deleted-app-delta",
			Domain: reducer.DomainCrossplaneSatisfiedByMaterialization, AttemptCount: 1,
		}); err != nil {
			t.Fatalf("Handle(deleted-app-delta): %v", err)
		}
		if len(spy.written) != 0 {
			t.Fatalf("SATISFIED_BY rows for a deleted claim = %v, want none", spy.written)
		}
	})

	t.Run("ledger row fences the base leg", func(t *testing.T) {
		const group = "ledger.example.org"
		seedCrossplaneRedriveClaimScope(ctx, t, h.db, "ledger-app", "ledger-app-full", group, crossplaneRedriveDeltaKind, 1, now)
		seedCrossplaneRedriveDelta(ctx, t, h.db, "ledger-app", "ledger-app-delta", "c0", now)
		if err := crossplanestore.NewCrossplaneRedriveTargetLedgerStore(postgres.SQLDB{DB: h.db}).
			RecordRedriven(ctx, "ledger-app", group, crossplaneRedriveDeltaKind); err != nil {
			t.Fatalf("RecordRedriven: %v", err)
		}
		assertCrossplaneRedriveScheduled(t, h.sweep(ctx, t, "ledger", group, now))
	})

	t.Run("legacy delta without a baseline has no base", func(t *testing.T) {
		const group = "legacy.example.org"
		seedCrossplaneRedriveClaimScope(ctx, t, h.db, "legacy-app", "legacy-app-full", group, crossplaneRedriveDeltaKind, 1, now)
		seedCrossplaneRedriveDelta(ctx, t, h.db, "legacy-app", "legacy-app-delta", "", now)
		assertCrossplaneRedriveScheduled(t, h.sweep(ctx, t, "legacy", group, now))
	})

	t.Run("both legs hit one scope: scheduled once", func(t *testing.T) {
		const group = "both.example.org"
		seedCrossplaneRedriveClaimScope(ctx, t, h.db, "both-app", "both-app-full", group, crossplaneRedriveDeltaKind, 1, now)
		seedCrossplaneRedriveDelta(ctx, t, h.db, "both-app", "both-app-delta", "c0", now, crossplaneRedriveDeltaRow{
			factID: "both-app-added", stableKey: "both-app-added", group: group, kind: crossplaneRedriveDeltaKind,
		})
		assertCrossplaneRedriveScheduled(t, h.sweep(ctx, t, "both", group, now), "both-app@both-app-delta")
	})

	t.Run("XRD generation superseded between the legs stops the base leg", func(t *testing.T) {
		const group = "superseded.example.org"
		seedCrossplaneRedriveClaimScope(ctx, t, h.db, "superseded-a", "superseded-a-gen", group, crossplaneRedriveDeltaKind, 1, now)
		seedCrossplaneRedriveClaimScope(ctx, t, h.db, "superseded-b", "superseded-b-full", group, crossplaneRedriveDeltaKind, 1, now)
		seedCrossplaneRedriveDelta(ctx, t, h.db, "superseded-b", "superseded-b-delta", "c0", now)
		h.replayer.onReplay = func(scopeID string) {
			if scopeID == "superseded-a" {
				seedCrossplaneRedriveXRD(ctx, t, h.db, "superseded-xrd", "superseded-xrd-gen-2", group, crossplaneRedriveDeltaKind, now.Add(time.Minute))
			}
		}
		defer func() { h.replayer.onReplay = nil }()
		// Only the active leg's target: the base leg's first page fence
		// sees the XRD generation superseded and stops.
		assertCrossplaneRedriveScheduled(t, h.sweep(ctx, t, "superseded", group, now), "superseded-a@superseded-a-gen")
	})

	t.Run("nil collaborator keeps the active leg only", func(t *testing.T) {
		const group = "nil.example.org"
		seedCrossplaneRedriveClaimScope(ctx, t, h.db, "nil-app", "nil-app-full", group, crossplaneRedriveDeltaKind, 1, now)
		seedCrossplaneRedriveDelta(ctx, t, h.db, "nil-app", "nil-app-delta", "c0", now)
		saved := h.sweeper
		h.sweeper = crossplanestore.CrossplaneSatisfiedByRedriveSweeper{
			DB: saved.DB, State: saved.State, Replayer: saved.Replayer, Owner: saved.Owner,
		}
		defer func() { h.sweeper = saved }()
		assertCrossplaneRedriveScheduled(t, h.sweep(ctx, t, "nil", group, now))
	})
}
