// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"
)

const testTimeline = "00000001"

func testLineage(t *testing.T) (*writerLineage, *writerIdentity) {
	t.Helper()
	lineage := newWriterLineage(physicalIdentity{systemID: "7", database: "eshu", incarnation: "100"}, lineageObservation{timeline: testTimeline, flush: 1000}, nil)
	base := lineage.identity()
	return lineage, base
}

func restarted(incarnation string) physicalIdentity {
	return physicalIdentity{systemID: "7", database: "eshu", incarnation: incarnation}
}

func TestWriterLineageFastPathIssuesNoQuery(t *testing.T) {
	lineage, base := testLineage(t)
	// A nil connection proves the unchanged-incarnation path never queries.
	if err := lineage.validate(context.Background(), nil, base, restarted("100")); err != nil {
		t.Fatalf("published incarnation rejected: %v", err)
	}
}

func TestWriterLineageAdmitsSameLineageRestart(t *testing.T) {
	lineage, base := testLineage(t)
	if err := lineage.admit(base, restarted("200"), lineageObservation{timeline: testTimeline, flush: 1000}); err != nil {
		t.Fatalf("same-lineage restart rejected: %v", err)
	}
	if got := lineage.identity(); got.incarnation != "200" || got.timeline != testTimeline {
		t.Fatalf("published identity = %+v", got)
	}
	if lineage.latched.Load() {
		t.Fatal("accepted restart latched the Access")
	}
}

func TestWriterLineageRejectsAndLatches(t *testing.T) {
	for name, observed := range map[string]lineageObservation{
		"new timeline":          {timeline: "00000002", flush: 5000},
		"flush below watermark": {timeline: testTimeline, flush: 999},
	} {
		t.Run(name, func(t *testing.T) {
			lineage, base := testLineage(t)
			if err := lineage.admit(base, restarted("200"), observed); !errors.Is(err, ErrWrongTopology) {
				t.Fatalf("admit = %v, want ErrWrongTopology", err)
			}
			if !lineage.latched.Load() || lineage.identity() != base {
				t.Fatal("rejection did not latch or it published the rejected identity")
			}
			// The latch holds even for a later dial that would otherwise pass.
			if err := lineage.validate(context.Background(), nil, base, restarted("100")); !errors.Is(err, ErrWrongTopology) {
				t.Fatalf("latched validate = %v", err)
			}
			if err := lineage.admit(base, restarted("300"), lineageObservation{timeline: testTimeline, flush: 9000}); !errors.Is(err, ErrWrongTopology) {
				t.Fatalf("latched admit = %v", err)
			}
		})
	}
}

// TestWriterLineageLatchWinsOverAnAlreadyPublishedIncarnation covers a dial that
// took its base before a restart was accepted and finishes after a divergent
// primary latched the Access: it matches the published incarnation, but a
// latched Access serves no connection at all.
func TestWriterLineageLatchWinsOverAnAlreadyPublishedIncarnation(t *testing.T) {
	lineage, staleBase := testLineage(t)
	if err := lineage.admit(staleBase, restarted("200"), lineageObservation{timeline: testTimeline, flush: 1000}); err != nil {
		t.Fatalf("restart rejected: %v", err)
	}
	published := lineage.identity()
	if err := lineage.admit(published, restarted("300"), lineageObservation{timeline: "00000002", flush: 5000}); !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("divergent primary admit = %v, want ErrWrongTopology", err)
	}
	if err := lineage.admit(staleBase, restarted("200"), lineageObservation{timeline: testTimeline, flush: 1000}); !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("stale dial after the latch = %v, want ErrWrongTopology", err)
	}
}

func TestWriterLineageForeignSystemIsNotLatched(t *testing.T) {
	lineage, base := testLineage(t)
	foreign := physicalIdentity{systemID: "8", database: "eshu", incarnation: "200"}
	if err := lineage.validate(context.Background(), nil, base, foreign); !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("foreign system = %v", err)
	}
	if lineage.latched.Load() {
		t.Fatal("a foreign system identifier latched the Access")
	}
}

func TestWriterLineageStaleObservationNeitherPublishesNorLatches(t *testing.T) {
	lineage, base := testLineage(t)
	if err := lineage.admit(base, restarted("300"), lineageObservation{timeline: testTimeline, flush: 5000}); err != nil {
		t.Fatal(err)
	}
	// A dial that read incarnation 200 against base 100 before 300 was
	// published is stale, even though its flush is now below the watermark.
	err := lineage.admit(base, restarted("200"), lineageObservation{timeline: testTimeline, flush: 2000})
	if !errors.Is(err, driver.ErrBadConn) || errors.Is(err, ErrWrongTopology) {
		t.Fatalf("stale observation = %v, want retryable ErrBadConn", err)
	}
	if lineage.latched.Load() || lineage.identity().incarnation != "300" {
		t.Fatal("stale observation latched or replaced the published identity")
	}
	// A sibling dial that saw the identity another dial already published
	// converges without publishing again.
	published := lineage.identity()
	if err := lineage.admit(base, restarted("300"), lineageObservation{timeline: testTimeline, flush: 5000}); err != nil || lineage.identity() != published {
		t.Fatalf("converging dial = %v, republished=%v", err, lineage.identity() != published)
	}
}

func TestWriterLineageConcurrentDialsConverge(t *testing.T) {
	lineage, base := testLineage(t)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			observed := lineageObservation{timeline: testTimeline, flush: uint64(1000 + i)}
			errs <- lineage.admit(base, restarted("200"), observed)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent dial after one restart = %v", err)
		}
	}
	if lineage.identity().incarnation != "200" || lineage.latched.Load() {
		t.Fatalf("identity=%+v latched=%v", lineage.identity(), lineage.latched.Load())
	}
}

func TestWriterLineageWatermarkIsAMonotonicMax(t *testing.T) {
	lineage, _ := testLineage(t)
	var wg sync.WaitGroup
	for i := range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lineage.raiseWatermark(uint64(i * 10))
		}()
	}
	wg.Wait()
	if got := lineage.watermark.Load(); got != 1990 {
		t.Fatalf("watermark = %d, want 1990", got)
	}
	lineage.raiseWatermark(5)
	if got := lineage.watermark.Load(); got != 1990 {
		t.Fatalf("watermark moved backward to %d", got)
	}
}

func TestWriterLineageCheckpointAssertsPublishedIdentity(t *testing.T) {
	lineage, base := testLineage(t)
	point := checkpoint{systemID: base.systemID, database: base.database, incarnation: "999"}
	if err := lineage.checkpointTopology(point, "0/FFFF"); !errors.Is(err, ErrWrongTopology) || lineage.watermark.Load() != 1000 {
		t.Fatalf("foreign-incarnation checkpoint = %v watermark=%d", err, lineage.watermark.Load())
	}
	point.incarnation = base.incarnation
	if err := lineage.checkpointTopology(point, "1/0"); err != nil || lineage.watermark.Load() != 1<<32 {
		t.Fatalf("checkpoint = %v watermark=%X", err, lineage.watermark.Load())
	}
	if err := lineage.checkpointTopology(point, "bogus"); err == nil || errors.Is(err, ErrWrongTopology) {
		t.Fatalf("malformed flush = %v", err)
	}
	lineage.latched.Store(true)
	if err := lineage.checkpointTopology(point, "2/0"); !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("latched checkpoint = %v", err)
	}
}

func TestParseLineageAndLSN(t *testing.T) {
	observed, err := parseLineage("0000000A0000000200000003", "2/3000060")
	if err != nil || observed.timeline != "0000000A" || observed.flush != 2<<32|0x3000060 {
		t.Fatalf("observed=%+v err=%v", observed, err)
	}
	for _, bad := range [][2]string{{"short", "0/1"}, {"0000000G0000000200000003", "0/1"}, {"000000010000000200000003", "01"}, {"000000010000000200000003", "x/1"}, {"000000010000000200000003", "123456789/0"}} {
		if _, err := parseLineage(bad[0], bad[1]); err == nil {
			t.Fatalf("parseLineage(%q, %q) accepted malformed input", bad[0], bad[1])
		}
	}
	if got := formatLSN(2<<32 | 0x3000060); got != "2/3000060" {
		t.Fatalf("formatLSN = %s", got)
	}
}
