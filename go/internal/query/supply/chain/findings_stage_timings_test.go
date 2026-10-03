// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// stageTimingsFindingStore stands in for the guarded Postgres reader: the
// findings read records its stage time into the request accumulator the way
// runtime/postgres does, and the runtime-context read reports whether the
// accumulator leaked past the findings read.
type stageTimingsFindingStore struct {
	*graph.FakeRuntimeContextFindingStore
	record               func(*db.StageTimings)
	accumulatorOnContext bool
}

func (s *stageTimingsFindingStore) ListSupplyChainImpactFindings(
	ctx context.Context,
	filter impact.FindingFilter,
) ([]impact.FindingRow, error) {
	if s.record != nil {
		s.record(db.StageTimingsFrom(ctx))
	}
	return s.FakeRuntimeContextFindingStore.ListSupplyChainImpactFindings(ctx, filter)
}

func (s *stageTimingsFindingStore) ListSupplyChainImpactRuntimeContext(
	ctx context.Context,
	repositoryIDs []string,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (map[string]impact.RuntimeContext, error) {
	s.accumulatorOnContext = db.StageTimingsFrom(ctx) != nil
	return s.FakeRuntimeContextFindingStore.ListSupplyChainImpactRuntimeContext(ctx, repositoryIDs, allowedRepositoryIDs, allowedScopeIDs)
}

func serveStageTimingsFindings(t *testing.T, store *stageTimingsFindingStore) []map[string]any {
	t.Helper()
	var logBuf bytes.Buffer
	handler := &Handler{ImpactFindings: store, Logger: slog.New(slog.NewJSONHandler(&logBuf, nil))}
	if rec := serveImpactFindings(t, handler); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	return decodeLogRecords(t, &logBuf)
}

var stageTimingsAttrs = []string{
	"reader_borrow_seconds", "reader_identity_seconds", "reader_replay_seconds", "business_query_seconds",
}

// TestImpactFindingsStageCompletionCarriesReaderStageSeconds is the #7545
// proof: the impact_findings_query completion line carries the summed
// guarded-reader stage seconds the findings read recorded, so an operator can
// attribute one slow call to borrow, identity, replay, or the business query.
func TestImpactFindingsStageCompletionCarriesReaderStageSeconds(t *testing.T) {
	t.Parallel()
	store := &stageTimingsFindingStore{
		FakeRuntimeContextFindingStore: &graph.FakeRuntimeContextFindingStore{
			Rows: []impact.FindingRow{failedStageRow()}, ByRepo: map[string]impact.RuntimeContext{},
		},
		record: func(timings *db.StageTimings) {
			timings.Add(db.ReaderStageBorrow, 40*time.Millisecond)
			timings.Add(db.ReaderStageBorrow, 10*time.Millisecond) // a second borrow is SUMMED
			timings.Add(db.ReaderStageIdentity, 20*time.Millisecond)
			timings.Add(db.ReaderStageReplay, 30*time.Millisecond)
			timings.Add(db.ReaderStageBusinessQuery, 1439*time.Millisecond)
		},
	}
	records := serveStageTimingsFindings(t, store)
	want := map[string]float64{
		"reader_borrow_seconds": 0.05, "reader_identity_seconds": 0.02,
		"reader_replay_seconds": 0.03, "business_query_seconds": 1.439,
	}
	var findings map[string]any
	for _, completed := range recordsWithEvent(records, "supply_chain_query.stage_completed") {
		if completed["stage"] == "impact_findings_query" {
			findings = completed
			continue
		}
		for _, name := range stageTimingsAttrs {
			if _, present := completed[name]; present {
				t.Errorf("stage %v carries %s; only impact_findings_query may", completed["stage"], name)
			}
		}
	}
	if findings == nil {
		t.Fatal("no impact_findings_query completion record")
	}
	for name, seconds := range want {
		got, ok := findings[name].(float64)
		if !ok || got < seconds-1e-9 || got > seconds+1e-9 {
			t.Errorf("%s = %v, want %v; record=%#v", name, findings[name], seconds, findings)
		}
	}
	if findings["rows_fetched"] != float64(1) || findings["error"] != false {
		t.Errorf("existing attrs changed: %#v", findings)
	}
	if store.accumulatorOnContext {
		t.Error("the stage accumulator leaked onto the runtime_context read context")
	}
}

// TestImpactFindingsStageCompletionOmitsReaderStageSecondsWhenUnrecorded keeps
// a non-guarded read honest: no recorded stage, so no zero-valued attributes.
func TestImpactFindingsStageCompletionOmitsReaderStageSecondsWhenUnrecorded(t *testing.T) {
	t.Parallel()
	store := &stageTimingsFindingStore{FakeRuntimeContextFindingStore: &graph.FakeRuntimeContextFindingStore{
		Rows: []impact.FindingRow{failedStageRow()}, ByRepo: map[string]impact.RuntimeContext{},
	}}
	records := serveStageTimingsFindings(t, store)
	found := false
	for _, completed := range recordsWithEvent(records, "supply_chain_query.stage_completed") {
		if completed["stage"] != "impact_findings_query" {
			continue
		}
		found = true
		for _, name := range stageTimingsAttrs {
			if _, present := completed[name]; present {
				t.Errorf("%s present although the store recorded nothing: %#v", name, completed)
			}
		}
	}
	if !found {
		t.Fatal("no impact_findings_query completion record")
	}
}
