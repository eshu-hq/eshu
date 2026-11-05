// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9777 step 2 disposable PostgreSQL proof that a 7,352-id change burst admits without loss, drains one slot per pass to quiescence with the watermark over the drained timestamps, and costs one whole-hit-list resolve per pass; isolated bootstrap schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
)

// burstOwnedNames are fixture-owned names in feed spelling, one per owned
// ecosystem the burst cycles through. Every seventh burst tuple names one
// of these (a resolve hit); the rest name synthetic packages no inventory
// holds (resolve misses that drain trivially).
var burstOwnedNames = []vulnerability.OSVDeltaChangedRecord{
	{AdvisoryID: "", Ecosystem: "npm", PackageName: "left-pad"},
	{AdvisoryID: "", Ecosystem: "npm", PackageName: "@scope/pkg"},
	{AdvisoryID: "", Ecosystem: "pypi", PackageName: "foo-bar"},
	{AdvisoryID: "", Ecosystem: "maven", PackageName: "org.apache:commons"},
	{AdvisoryID: "", Ecosystem: "maven", PackageName: "com.x:lib_2.13"},
	{AdvisoryID: "", Ecosystem: "go", PackageName: "github.com/foo/bar"},
	{AdvisoryID: "", Ecosystem: "nuget", PackageName: "newtonsoft.json"},
	{AdvisoryID: "", Ecosystem: "composer", PackageName: "foo/bar"},
	{AdvisoryID: "", Ecosystem: "swift", PackageName: "github.com/apple/swift-nio"},
}

var burstEcosystems = []string{
	"npm", "pypi", "maven", "go", "nuget", "swift", "rubygems", "composer", "cargo", "pub", "hex",
}

// TestOSVDeltaReplanBurstDrainsWithoutLossLive proves the queue absorbs a
// full-day 7,352-id synthetic burst: every valid tuple admits (nothing
// dropped), each pass resolves the whole hit list once and drains one
// slot's scopes, overflow waits, and quiescence leaves no pending row with
// the watermark on the newest drained feed time. Timings print for the
// step-2 intersection cost note.
func TestOSVDeltaReplanBurstDrainsWithoutLossLive(t *testing.T) {
	ctx, database, store, replan := replanLiveStores(t)
	instance := "replan-burst"
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	const burst = 7352

	changed := make([]vulnerability.OSVDeltaChangedRecord, 0, burst)
	for i := 0; i < burst; i++ {
		eco := burstEcosystems[i%len(burstEcosystems)]
		name := fmt.Sprintf("burst-pkg-%d", i)
		if i%7 == 0 {
			hit := burstOwnedNames[(i/7)%len(burstOwnedNames)]
			eco, name = hit.Ecosystem, hit.PackageName
		}
		changed = append(changed, replanChangedRecord(
			fmt.Sprintf("GHSA-burst-%04d", i), eco, name, i%11 == 0, base.Add(time.Duration(i)*time.Second),
		))
	}

	intersectStart := time.Now()
	intersection := vulnerability.IntersectOSVDeltaChangedNames(changed, nil)
	intersectElapsed := time.Since(intersectStart)
	if intersection.ParseFailed != 0 {
		t.Fatalf("burst intersect parsed %d failures, want 0", intersection.ParseFailed)
	}
	if len(intersection.Changes) != burst {
		t.Fatalf("burst intersect validated %d tuples, want %d", len(intersection.Changes), burst)
	}

	admitStart := time.Now()
	admitted := admitReplanFlow(ctx, t, replan, instance, changed, nil)
	admitElapsed := time.Since(admitStart)
	if admitted != burst {
		t.Fatalf("burst admitted = %d, want %d (nothing dropped)", admitted, burst)
	}

	var passes int
	var resolveElapsed time.Duration
	var prevWatermark time.Time
	for {
		pending, err := replan.ReadPendingReplan(ctx, instance)
		if err != nil {
			t.Fatalf("read pending: %v", err)
		}
		if len(pending) == 0 {
			break
		}
		if passes >= 1200 {
			t.Fatalf("burst did not drain in 1200 passes (%d rows left)", len(pending))
		}
		pendingRecords := make([]vulnerability.OSVDeltaChangedRecord, 0, len(pending))
		for _, row := range pending {
			pendingRecords = append(pendingRecords, vulnerability.OSVDeltaChangedRecord{
				AdvisoryID: row.AdvisoryID, Ecosystem: row.Ecosystem, PackageName: row.PackageName,
				Withdrawn: row.Withdrawn, FeedTimestamp: row.FeedTimestamp.UTC().Format(time.RFC3339), RowID: row.ID,
			})
		}
		drainIntersection := vulnerability.IntersectOSVDeltaChangedNames(pendingRecords, nil)
		resolveStart := time.Now()
		resolved, err := store.ResolveOwnedPackageNames(ctx, toResolverLookups(drainIntersection.Lookups))
		resolveElapsed += time.Since(resolveStart)
		if err != nil {
			t.Fatalf("pass %d resolve: %v", passes, err)
		}
		plan := drainAndCommit(ctx, t, database, replan, instance, drainIntersection, resolved)
		if plan.SlotsPlanned > 1 {
			t.Fatalf("pass %d drained %d slots, want at most 1", passes, plan.SlotsPlanned)
		}
		committed, set, _, _, err := replan.ReadCommitWatermark(ctx, instance)
		if err != nil {
			t.Fatalf("read watermark: %v", err)
		}
		if !set {
			t.Fatalf("pass %d left the watermark unset", passes)
		}
		if committed.Before(prevWatermark) {
			t.Fatalf("watermark regressed from %v to %v", prevWatermark, committed)
		}
		prevWatermark = committed
		passes++
	}
	committed, set, _, _, err := replan.ReadCommitWatermark(ctx, instance)
	if err != nil {
		t.Fatalf("read final watermark: %v", err)
	}
	wantMax := base.Add(time.Duration(burst-1) * time.Second)
	if !set || !committed.Equal(wantMax) {
		t.Fatalf("final watermark = (%v, %v), want the newest drained feed time %v", committed, set, wantMax)
	}
	t.Logf("replan burst: %d tuples intersected in %v, admitted in %v, drained in %d passes with %v total resolve time",
		burst, intersectElapsed, admitElapsed, passes, resolveElapsed)
}
