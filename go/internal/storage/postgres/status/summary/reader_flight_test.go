// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestReaderSharesOneLiveStatementAcrossPerTransactionStores builds a new
// store for every request, as the API and MCP snapshot factories do, over one
// process-wide reader: 50 concurrent fallbacks run the live statement once, and
// every request reports the leader's clock as its as_of.
func TestReaderSharesOneLiveStatementAcrossPerTransactionStores(t *testing.T) {
	t.Parallel()

	const requests = 50
	q := &sourceQueryer{installed: true, liveGate: make(chan struct{}), liveStarted: make(chan struct{}, 1)}
	reader := pgstatus.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: true, StaleAfter: 33 * time.Second})
	var wg sync.WaitGroup
	snapshots := make([]statuspkg.RawSnapshot, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := pgstatus.NewStatusStore(q).WithSummaryReader(reader) // one store per transaction
			asOf := sourceTestNow.Add(time.Duration(i) * time.Millisecond)
			snapshot, err := store.ReadStatusSnapshotFiltered(context.Background(), asOf, statuspkg.FullSnapshotSelection())
			if err != nil {
				t.Errorf("request %d: %v", i, err)
				return
			}
			snapshots[i] = snapshot
		}()
	}
	<-q.liveStarted
	deadline := time.Now().Add(10 * time.Second)
	for reader.Waiting() < requests-1 {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d requests joined the in-flight live statement", reader.Waiting(), requests-1)
		}
		time.Sleep(time.Millisecond)
	}
	close(q.liveGate)
	wg.Wait()

	if got := q.liveRuns.Load(); got != 1 {
		t.Fatalf("live statement ran %d times for %d concurrent fallbacks across per-transaction stores, want exactly 1 per process", got, requests)
	}
	leaderAsOf := snapshots[0].ActiveWorkSource.AsOf
	for i, snapshot := range snapshots {
		source := snapshot.ActiveWorkSource
		if source.Source != statuspkg.ActiveWorkSourceLiveFallback || source.Reason != statuspkg.ActiveWorkReasonMissing || source.Stale {
			t.Fatalf("request %d source = %+v, want live_fallback/missing, stale=false", i, source)
		}
		if len(snapshot.StageCounts) != 1 || snapshot.StageCounts[0].Count != 100 {
			t.Fatalf("request %d stage counts = %+v, want the shared live answer", i, snapshot.StageCounts)
		}
	}
	// Followers report the leader's clock: the one as_of the live statement ran at.
	for i, snapshot := range snapshots {
		if !snapshot.ActiveWorkSource.AsOf.Equal(leaderAsOf) {
			t.Fatalf("request %d as_of = %v, want the leader's %v", i, snapshot.ActiveWorkSource.AsOf, leaderAsOf)
		}
	}
}

func TestReaderFollowerStopsWhenItsRequestContextEnds(t *testing.T) {
	t.Parallel()

	q := &sourceQueryer{installed: true, liveGate: make(chan struct{}), liveStarted: make(chan struct{}, 1)}
	reader := pgstatus.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: true, StaleAfter: 33 * time.Second})
	leaderDone := make(chan error, 1)
	go func() {
		_, err := pgstatus.NewStatusStore(q).WithSummaryReader(reader).
			ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, statuspkg.FullSnapshotSelection())
		leaderDone <- err
	}()
	<-q.liveStarted

	ctx, cancel := context.WithCancel(context.Background())
	followerDone := make(chan error, 1)
	go func() {
		_, err := pgstatus.NewStatusStore(q).WithSummaryReader(reader).
			ReadStatusSnapshotFiltered(ctx, sourceTestNow, statuspkg.FullSnapshotSelection())
		followerDone <- err
	}()
	for reader.Waiting() < 1 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-followerDone:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("follower error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled follower kept its transaction waiting for the leader")
	}
	if q.liveRuns.Load() != 1 {
		t.Fatalf("live statement ran %d times, want only the leader's", q.liveRuns.Load())
	}
	close(q.liveGate)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader error = %v", err)
	}
}

func TestReaderSharedLiveResultsAreIndependentCopies(t *testing.T) {
	t.Parallel()

	q := &sourceQueryer{installed: true, liveGate: make(chan struct{}), liveStarted: make(chan struct{}, 1)}
	reader := pgstatus.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: true, StaleAfter: 33 * time.Second})
	results := make([]statuspkg.RawSnapshot, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _ = pgstatus.NewStatusStore(q).WithSummaryReader(reader).
				ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, statuspkg.FullSnapshotSelection())
		}()
	}
	<-q.liveStarted
	for reader.Waiting() < 1 {
		time.Sleep(time.Millisecond)
	}
	close(q.liveGate)
	wg.Wait()
	results[0].StageCounts[0].Count = -1
	if results[1].StageCounts[0].Count == -1 {
		t.Fatal("two requests share one StageCounts backing array; a mutation by one changes the other")
	}
}

// TestReaderRollingUpgradeFlipsBackToTheModel: a row written by another
// statement version is a version fallback and is never decoded; once the
// writer replaces it with this binary's digest, the next read serves the model.
func TestReaderRollingUpgradeFlipsBackToTheModel(t *testing.T) {
	t.Parallel()

	storedAt := sourceTestNow.Add(-5 * time.Second)
	q := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, "old-binary-digest", 1, `{"future":"encoding"}`)}
	store := readerStore(q, true)
	if got := readSnapshot(t, store).ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceLiveFallback || got.Reason != statuspkg.ActiveWorkReasonVersion {
		t.Fatalf("first read source = %+v, want live_fallback/version", got)
	}
	q.row = storedRow(storedAt.Add(time.Second), summary.SchemaVersion, pgstatus.ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))
	if got := readSnapshot(t, store).ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceModel || got.Reason != statuspkg.ActiveWorkReasonFresh {
		t.Fatalf("second read source = %+v, want model/fresh after the writer flipped the digest", got)
	}
}

func TestReaderRecordsTheModelReadUnderItsOwnLabel(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	storedAt := sourceTestNow.Add(-10 * time.Second)
	q := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, pgstatus.ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))}
	store := readerStore(q, true)
	store.Instruments = instruments
	readSnapshot(t, store)

	if len(q.labels) != 2 || q.labels[0] != "active_work_summary_model" || q.labels[1] != "terraform_state_model" {
		t.Fatalf("the row reads carried labels %q, want active_work_summary_model then terraform_state_model", q.labels)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	reads := map[string]uint64{}
	var counted int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Histogram[float64]:
				if m.Name != "eshu_dp_status_snapshot_read_duration_seconds" {
					continue
				}
				for _, point := range data.DataPoints {
					read, _ := point.Attributes.Value(attribute.Key("read"))
					reads[read.AsString()] += point.Count
				}
			case metricdata.Sum[int64]:
				if m.Name == "eshu_dp_status_summary_read_total" {
					for _, point := range data.DataPoints {
						counted += point.Value
					}
				}
			}
		}
	}
	if reads["active_work_summary_model"] != 1 || reads["active_work_summary"] != 0 {
		t.Fatalf("read samples = %v, want one active_work_summary_model and no live active_work_summary", reads)
	}
	if counted != 2 {
		t.Fatalf("eshu_dp_status_summary_read_total = %d, want 2: one read per model (active work served, terraform_state missing)", counted)
	}
}
