// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// guardRoutingDB answers the commit path's base-connection reads by query
// identity, not by position: the repository-catalog load runs between the
// prior reads and the guard probe, so a positional script would hand the probe
// response to the catalog. Every unrouted query fails the test.
type guardRoutingDB struct {
	t  *testing.T
	tx *fakeTx

	candidate   []any
	targets     map[string][]any // generation id -> {is_delta, baseline sha}
	bases       map[string][]any // baseline sha -> {generation id, status}
	counts      map[string][]any // generation id -> {collector, total, files}
	anchorPaths []string
	probeErr    error

	queries []fakeQueryCall
}

func (g *guardRoutingDB) Begin(context.Context) (db.Transaction, error) { return g.tx, nil }

func (g *guardRoutingDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected ExecContext on outer db")
}

func (g *guardRoutingDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	g.queries = append(g.queries, fakeQueryCall{query: query, args: args})
	switch query {
	case unchangedGenerationCandidateQuery:
		return &queueFakeRows{rows: [][]any{g.candidate}}, nil
	case deltaUnionTargetQuery:
		return g.rowsFor(g.targets, args[1], false), nil
	case deltaUnionBaseQuery:
		return g.rowsFor(g.bases, args[1], true), nil
	case successorGuardCountQuery:
		return g.rowsFor(g.counts, args[1], false), nil
	case successorGuardAnchorFilePathsQuery:
		if g.probeErr != nil {
			return nil, g.probeErr
		}
		rows := make([][]any, 0, len(g.anchorPaths))
		for _, path := range g.anchorPaths {
			rows = append(rows, []any{path})
		}
		return &queueFakeRows{rows: rows}, nil
	}
	if strings.Contains(query, "FROM fact_records") && strings.Contains(query, "fact_kind = 'repository'") {
		return &queueFakeRows{}, nil
	}
	g.t.Fatalf("unrouted base-connection query:\n%s", query)
	return nil, errors.New("unrouted query")
}

func (g *guardRoutingDB) rowsFor(table map[string][]any, key any, emptyWhenMissing bool) db.Rows {
	row, ok := table[fmt.Sprint(key)]
	if !ok {
		if !emptyWhenMissing {
			g.t.Fatalf("no staged row for key %v", key)
		}
		return &queueFakeRows{}
	}
	return &queueFakeRows{rows: [][]any{row}}
}

func (g *guardRoutingDB) countQueries(query string) int {
	n := 0
	for _, call := range g.queries {
		if call.query == query {
			n++
		}
	}
	return n
}

func (g *guardRoutingDB) enqueuedProjectorWork() bool {
	for _, exec := range g.tx.execs {
		if strings.Contains(exec.query, "INSERT INTO fact_work_items") {
			return true
		}
	}
	return false
}

// guardDeltaFixture is the scope and delta successor the #8750 store tests
// commit. The active prior is the full generation "gen-full" at commit-base.
func guardDeltaFixture(isDelta bool) (scope.IngestionScope, scope.ScopeGeneration) {
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	generation := scope.ScopeGeneration{
		GenerationID: "gen-next", ScopeID: "scope-8750",
		ObservedAt: now.Add(-time.Minute), IngestedAt: now,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint: "hint-next", SourceCommitSHA: "commit-new",
	}
	if isDelta {
		generation.IsDelta = true
		generation.DeltaBaselineCommitSHA = "commit-base"
	}
	return scope.IngestionScope{
		ScopeID: "scope-8750", SourceSystem: "git", ScopeKind: scope.KindRepository,
		CollectorKind: scope.CollectorGit, PartitionKey: "example.com/org/repo",
	}, generation
}

// newGuardRoutingDB stages an active full prior "gen-full" at commit-base with
// the given guard counts; the delta successor's anchor is that same
// generation, so no second count query runs.
func newGuardRoutingDB(t *testing.T, collector, total, files int) *guardRoutingDB {
	t.Helper()
	return &guardRoutingDB{
		t: t, tx: &fakeTx{},
		candidate: []any{"gen-full", "hint-prior", "commit-base", "active", false, false},
		targets:   map[string][]any{"gen-full": {false, ""}},
		bases:     map[string][]any{"commit-base": {"gen-full", "active"}},
		counts:    map[string][]any{"gen-full": {collector, total, files}},
	}
}

// guardStream builds the successor's stream: an optional delta_generation
// repository fact, live file facts, file tombstones, and plain collector
// entities.
func guardStream(
	generation scope.ScopeGeneration,
	deltaFact bool,
	changed, deleted []string,
	liveFiles, tombstoneFiles []string,
	entities int,
) <-chan facts.Envelope {
	var envelopes []facts.Envelope
	if deltaFact {
		envelopes = append(envelopes, facts.Envelope{
			FactID: "repo", ScopeID: generation.ScopeID, GenerationID: generation.GenerationID,
			FactKind: "repository", StableFactKey: "repository:org/repo", ObservedAt: generation.ObservedAt,
			Payload: map[string]any{
				"repo_id": "org/repo", "path": "/repos/org", "delta_generation": true,
				"delta_relative_paths": toAnySlice(changed), "delta_deleted_relative_paths": toAnySlice(deleted),
			},
		})
	}
	add := func(prefix string, paths []string, tombstone bool) {
		for i, path := range paths {
			envelopes = append(envelopes, facts.Envelope{
				FactID: fmt.Sprintf("%s-%d", prefix, i), ScopeID: generation.ScopeID, GenerationID: generation.GenerationID,
				FactKind: "file", StableFactKey: "file:org/repo:" + path, ObservedAt: generation.ObservedAt,
				IsTombstone: tombstone, Payload: map[string]any{"relative_path": path},
			})
		}
	}
	add("live", liveFiles, false)
	add("tomb", tombstoneFiles, true)
	for i := 0; i < entities; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactID: fmt.Sprintf("entity-%d", i), ScopeID: generation.ScopeID, GenerationID: generation.GenerationID,
			FactKind: "content_entity", StableFactKey: fmt.Sprintf("content_entity:org/repo:%d", i),
			ObservedAt: generation.ObservedAt, Payload: map[string]any{"relative_path": "x.go"},
		})
	}
	return testfixtures.FactChannel(envelopes)
}

func numberedPaths(prefix string, count int) []string {
	paths := make([]string, 0, count)
	for i := 0; i < count; i++ {
		paths = append(paths, fmt.Sprintf("%s%03d.go", prefix, i))
	}
	return paths
}

type guardDecisionKey struct{ kind, rule, outcome string }

// guardDecisions reads eshu_dp_ingestion_successor_guard_decisions_total from
// a manual reader, keyed by kind, rule, and outcome.
func guardDecisions(t *testing.T, reader *sdkmetric.ManualReader) map[guardDecisionKey]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[guardDecisionKey]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_ingestion_successor_guard_decisions_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("decisions counter data type = %T, want Int64 sum", m.Data)
			}
			for _, point := range sum.DataPoints {
				key := guardDecisionKey{}
				for _, kv := range point.Attributes.ToSlice() {
					switch string(kv.Key) {
					case telemetry.MetricDimensionKind:
						key.kind = kv.Value.AsString()
					case telemetry.MetricDimensionRule:
						key.rule = kv.Value.AsString()
					case telemetry.MetricDimensionOutcome:
						key.outcome = kv.Value.AsString()
					}
				}
				out[key] += point.Value
			}
		}
	}
	return out
}

func commitGuardScenario(
	t *testing.T,
	database *guardRoutingDB,
	generation scope.ScopeGeneration,
	scopeValue scope.IngestionScope,
	stream <-chan facts.Envelope,
) (map[guardDecisionKey]int64, error) {
	t.Helper()
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	instruments, reader := newDeltaUnionTestInstruments(t)
	store := NewIngestionStore(database)
	store.Instruments = instruments
	err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, stream)
	return guardDecisions(t, reader), err
}

func requireStored(t *testing.T, database *guardRoutingDB) {
	t.Helper()
	if !database.tx.committed || database.tx.rolledBack {
		t.Fatalf("committed = %v, rolled back = %v, want a committed store", database.tx.committed, database.tx.rolledBack)
	}
	if !database.enqueuedProjectorWork() {
		t.Fatal("stored successor enqueued no projector work")
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
		t.Fatalf("destructive_successor skips = %d, want 0", got)
	}
}

func requireRefused(t *testing.T, database *guardRoutingDB) {
	t.Helper()
	if !database.tx.rolledBack || database.tx.committed {
		t.Fatalf("rolled back = %v, committed = %v, want a rollback with no commit", database.tx.rolledBack, database.tx.committed)
	}
	if database.enqueuedProjectorWork() {
		t.Fatal("refused successor enqueued projector work")
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 1 {
		t.Fatalf("destructive_successor skips = %d, want 1", got)
	}
}

func requireDecisions(t *testing.T, got map[guardDecisionKey]int64, want guardDecisionKey) {
	t.Helper()
	if len(got) != 1 || got[want] != 1 {
		t.Fatalf("guard decisions = %v, want exactly one %v", got, want)
	}
}
