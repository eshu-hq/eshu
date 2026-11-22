// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// This file pins the L4 re-key overhead contract (#10408, criterion 3.3): a
// pass that carries advisory-group member sets costs the same statements as
// an ordinary changed pass, and a pass whose groups did not change issues no
// capture statement at all. The member union rides the changed-set helpers in
// memory; it adds payload bytes, never statements.

// overheadQueries renders the statement stream of one write as its ordered
// query names, so two passes can be compared statement for statement.
func overheadQueries(t *testing.T, beginner fakeImpactBeginner) []string {
	t.Helper()
	out := make([]string, 0, len(beginner.state.all))
	for _, call := range beginner.state.all {
		out = append(out, call.Query)
	}
	return out
}

// TestRekeyPassIssuesNoNewStatement pins the statement parity: a re-key merge
// pass (member-carrying rows, one retired group) and an ordinary changed pass
// (same row count, no members) execute the same statements in the same order.
// A member-only refresh, and an unchanged rerun with members, capture one
// batch and zero batches respectively, exactly like their member-less twins.
func TestRekeyPassIssuesNoNewStatement(t *testing.T) {
	t.Parallel()

	// One changed row out of three: the same change shape, with and without
	// the merged member set on the changed row.
	ordinaryWrite := changedTestWrite(
		changedTestFinding("CVE-2026-0901"),
		changedTestFinding("CVE-2026-0902"),
		changedTestFinding("CVE-2026-0903"),
	)
	rekeyWrite := rekeyWrite("generation-changed",
		rekeyFinding("CVE-2026-0901", "CVE-2026-0901", "CVE-2026-0904"),
		rekeyFinding("CVE-2026-0902", "CVE-2026-0902"),
		rekeyFinding("CVE-2026-0903", "CVE-2026-0903"),
	)
	// Seed both beginners with the stored rows of the unchanged findings, so
	// only the first row is new in each arm.
	seedUnchanged := func(beginner fakeImpactBeginner, write SupplyChainImpactWrite, findings []SupplyChainImpactFinding) {
		beginner.state.stored = nil
		for _, finding := range findings[1:] {
			beginner.state.stored = append(beginner.state.stored, storedLike(write, finding))
		}
	}

	ordinaryWriter, ordinaryBeginner, ordinaryBatches, _ := captureWriterRecording()
	seedUnchanged(ordinaryBeginner, ordinaryWrite, ordinaryWrite.Findings)
	if _, err := ordinaryWriter.WriteSupplyChainImpactFindings(context.Background(), ordinaryWrite); err != nil {
		t.Fatalf("ordinary changed pass: %v", err)
	}
	rekeyWriter, rekeyBeginner, rekeyBatches, _ := captureWriterRecording()
	seedUnchanged(rekeyBeginner, rekeyWrite, rekeyWrite.Findings)
	if _, err := rekeyWriter.WriteSupplyChainImpactFindings(context.Background(), rekeyWrite); err != nil {
		t.Fatalf("re-key changed pass: %v", err)
	}

	ordinaryStream, rekeyStream := overheadQueries(t, ordinaryBeginner), overheadQueries(t, rekeyBeginner)
	if len(ordinaryStream) != len(rekeyStream) {
		t.Fatalf("statement counts differ: ordinary %d, re-key %d\nordinary: %q\nre-key: %q",
			len(ordinaryStream), len(rekeyStream), ordinaryStream, rekeyStream)
	}
	for i := range ordinaryStream {
		if ordinaryStream[i] != rekeyStream[i] {
			t.Fatalf("statement %d differs: ordinary %q, re-key %q", i, ordinaryStream[i], rekeyStream[i])
		}
	}
	if len(*ordinaryBatches) != 1 || len(*rekeyBatches) != 1 {
		t.Fatalf("capture batches: ordinary %d, re-key %d, want exactly one each", len(*ordinaryBatches), len(*rekeyBatches))
	}

	// No-group-change path: an unchanged rerun captures nothing and issues
	// the same stream with or without members.
	ordinaryRerun, ordinaryRerunBeginner, ordinaryRerunBatches, _ := captureWriterRecording()
	ordinaryRerunBeginner.state.stored = nil
	for _, finding := range ordinaryWrite.Findings {
		ordinaryRerunBeginner.state.stored = append(ordinaryRerunBeginner.state.stored, storedLike(ordinaryWrite, finding))
	}
	if _, err := ordinaryRerun.WriteSupplyChainImpactFindings(context.Background(), ordinaryWrite); err != nil {
		t.Fatalf("ordinary unchanged rerun: %v", err)
	}
	rekeyRerun, rekeyRerunBeginner, rekeyRerunBatches, _ := captureWriterRecording()
	rekeyRerunBeginner.state.stored = nil
	for _, finding := range rekeyWrite.Findings {
		stored := storedLike(rekeyWrite, finding)
		stored.members = append([]string(nil), finding.AdvisoryGroupMembers...)
		rekeyRerunBeginner.state.stored = append(rekeyRerunBeginner.state.stored, stored)
	}
	if _, err := rekeyRerun.WriteSupplyChainImpactFindings(context.Background(), rekeyWrite); err != nil {
		t.Fatalf("re-key unchanged rerun: %v", err)
	}
	if len(*ordinaryRerunBatches) != 0 || len(*rekeyRerunBatches) != 0 {
		t.Fatalf("unchanged rerun captured batches: ordinary %d, re-key %d, want zero both",
			len(*ordinaryRerunBatches), len(*rekeyRerunBatches))
	}
	ordinaryRerunStream, rekeyRerunStream := overheadQueries(t, ordinaryRerunBeginner), overheadQueries(t, rekeyRerunBeginner)
	if len(ordinaryRerunStream) != len(rekeyRerunStream) {
		t.Fatalf("unchanged-rerun statement counts differ: ordinary %d, re-key %d", len(ordinaryRerunStream), len(rekeyRerunStream))
	}
}

// Re-key overhead bound constants. Allocation counts are near-deterministic
// (ordinary allocations vary by a few percent between runs), so the allocation
// margins guard against helper changes, not noise; the timing bound is a ratio
// of two per-member costs taken from interleaved minimums, so a sustained
// runner slowdown cancels out of it.
const (
	// overheadRows is the number of changed findings in every measured write.
	overheadRows = 4
	// overheadSmallMembers and overheadLargeMembers are the per-finding
	// advisory-group member counts of the two scale points the bound compares.
	//
	// The large point is 8192 so that a quadratic term that does not allocate
	// still dominates the linear per-member cost under -race, where race
	// instrumentation lifts the linear cost to about 2us per member and hides a
	// quadratic scan at 2048 (#11232 review: such a scan grew only 2.8x there,
	// and 8.3-9.3x at 8192).
	overheadSmallMembers = 256
	overheadLargeMembers = 8192
	// overheadAllocMembers is the mid scale point of the allocation bound. The
	// allocation check does not need the large point (a per-member copy shows at
	// any scale), and a seeded allocating quadratic union stays cheap enough to
	// fail in seconds instead of minutes.
	overheadAllocMembers = 2048
	// overheadParityFactor bounds the allocation count of the realistic
	// re-key fixture against the ordinary changed pass. Measured at about +2%.
	overheadParityFactor = 1.5
	// overheadAllocsPerMember bounds the marginal allocations one extra
	// advisory-group member may cost. A member costs one string per stored
	// member (the parse) plus the map inserts of the new and the departed set,
	// measured at about 1.08 allocations per member; a union that copies or
	// rescans per member costs on the order of log2(N) (14 measured) or N.
	overheadAllocsPerMember = 4.0
	// overheadCostGrowthFactor bounds how much the per-member time cost may
	// grow between the two scale points. A linear union measures about 1.0; a
	// quadratic one grows with the member count itself.
	overheadCostGrowthFactor = 4.0
	// overheadTimingRounds is the number of interleaved rounds one timing
	// attempt takes each scale point's minimum over, and overheadTimingAttempts
	// the number of independent attempts before the growth bound fails.
	overheadTimingRounds   = 15
	overheadTimingAttempts = 3
	// overheadEarlyStopRounds and overheadEarlyStopFactor end one attempt's
	// rounds early once the minimums taken so far already show growth of
	// overheadEarlyStopFactor times the limit. A slow quadratic union would
	// otherwise spend minutes on rounds whose verdict is settled; the attempt
	// still counts as one miss, so the all-attempts rule is unchanged.
	overheadEarlyStopRounds = 3
	overheadEarlyStopFactor = 3.0
)

// overheadFixture is one measured pass: the write under test and the stored
// rows the changed-row read returns for it.
type overheadFixture struct {
	write  SupplyChainImpactWrite
	stored []fakeStoredFinding
}

// overheadScaleFixture builds a re-key write of overheadRows findings, each
// carrying its own id plus members synthetic group members, over stored rows
// from a prior generation that carry a disjoint member set of the same size.
// Every row replaces a stored row under a changed member set, so the pass
// exercises the replaced-row paths in memory: the stored member parse, the
// member-set comparison, and the union of both member sets into the changed and
// touched sets, each proportional to members.
func overheadScaleFixture(members int) overheadFixture {
	findings := make([]SupplyChainImpactFinding, 0, overheadRows)
	priors := make([]SupplyChainImpactFinding, 0, overheadRows)
	for row := range overheadRows {
		cveID := fmt.Sprintf("CVE-2026-%04d", 1100+row)
		group := make([]string, 0, members+1)
		departed := make([]string, 0, members+1)
		group, departed = append(group, cveID), append(departed, cveID)
		for member := range members {
			group = append(group, fmt.Sprintf("GHSA-%d-%05d", row, member))
			departed = append(departed, fmt.Sprintf("GHSA-old-%d-%05d", row, member))
		}
		findings = append(findings, rekeyFinding(cveID, group...))
		priors = append(priors, rekeyFinding(cveID, departed...))
	}
	write := rekeyWrite("generation-overhead", findings...)
	prior := rekeyWrite("generation-prior", priors...)
	stored := make([]fakeStoredFinding, 0, overheadRows)
	for _, finding := range prior.Findings {
		stored = append(stored, rekeyPriorRow(prior, finding))
	}
	return overheadFixture{write: write, stored: stored}
}

// overheadWriter returns a production writer over the fake transaction surface
// with the fixture's stored rows loaded.
func overheadWriter(fixture overheadFixture) PostgresSupplyChainImpactWriter {
	writer, beginner := changedTestWriter()
	beginner.state.stored = fixture.stored
	return writer
}

// overheadAllocs returns the mean allocations of one production writer pass
// over the fixture on the fake transaction surface.
func overheadAllocs(t *testing.T, fixture overheadFixture) float64 {
	t.Helper()
	return testing.AllocsPerRun(5, func() {
		if _, err := overheadWriter(fixture).WriteSupplyChainImpactFindings(context.Background(), fixture.write); err != nil {
			t.Fatalf("overhead pass: %v", err)
		}
	})
}

// overheadTime returns the wall time of one production writer pass over the
// fixture.
func overheadTime(t *testing.T, fixture overheadFixture) time.Duration {
	t.Helper()
	writer := overheadWriter(fixture)
	start := time.Now()
	if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), fixture.write); err != nil {
		t.Fatalf("overhead pass: %v", err)
	}
	return time.Since(start)
}

// TestRekeyPassOverheadIsMeasured pins the in-memory writer-core cost of a
// re-key pass: the member-union helpers cost a bounded amount, linear in the
// member count, and the realistic re-key pass allocates like an ordinary
// changed pass. The parity arm covers new rows (no stored row to replace); the
// scale arms cover replaced rows whose stored and new member sets differ, so
// the stored-member parse, the member-set comparison, and both unions are
// measured. It runs the production writer over the fake transaction
// surface, so the measured cost covers finding encode, digest, the
// member-union helpers, and the capture filter, but no Postgres round trip: it
// is the writer-core overhead, not an end-to-end claim.
//
// The test deliberately does not call t.Parallel. The previous form compared
// two wall-clock medians taken one after the other beside other parallel
// tests under -race, so a load spike that landed on one arm failed it with no
// code change (+152.6% on CI, #11232). Here the sequential phase of the run
// owns the process (parallel tests resume only after it), which testing.AllocsPerRun
// also requires, so:
//
//   - allocation counts are near-deterministic: the realistic fixture stays within
//     overheadParityFactor of the ordinary pass, and the marginal allocation
//     per added member between two scale points stays within
//     overheadAllocsPerMember;
//   - timing, which catches a union that is slow without allocating, compares
//     the per-member cost at the two scale points from interleaved minimums.
//     A linear union holds the ratio near 1.0; a quadratic one grows it with
//     the member count. The margin is overheadCostGrowthFactor, and the check
//     fails only when every one of overheadTimingAttempts independent
//     attempts exceeds it.
func TestRekeyPassOverheadIsMeasured(t *testing.T) {
	ordinary := changedTestWrite(
		changedTestFinding("CVE-2026-0911"),
		changedTestFinding("CVE-2026-0912"),
		changedTestFinding("CVE-2026-0913"),
		changedTestFinding("CVE-2026-0914"),
	)
	rekey := rekeyWrite("generation-overhead",
		rekeyFinding("CVE-2026-0911", "CVE-2026-0911", "CVE-2026-0915"),
		rekeyFinding("CVE-2026-0912", "CVE-2026-0912"),
		rekeyFinding("CVE-2026-0913", "CVE-2026-0913", "CVE-2026-0916"),
		rekeyFinding("CVE-2026-0914", "CVE-2026-0914"),
	)
	ordinaryAllocs, rekeyAllocs := overheadAllocs(t, overheadFixture{write: ordinary}), overheadAllocs(t, overheadFixture{write: rekey})
	t.Logf("writer-core overhead rows=%d changed=%d: ordinary allocs=%.0f re-key allocs=%.0f ratio=%.3f",
		overheadRows, overheadRows, ordinaryAllocs, rekeyAllocs, rekeyAllocs/ordinaryAllocs)
	if rekeyAllocs > overheadParityFactor*ordinaryAllocs {
		t.Fatalf("re-key pass allocs %.0f exceed %.1fx the ordinary pass allocs %.0f: the member union is not a bounded in-memory cost",
			rekeyAllocs, overheadParityFactor, ordinaryAllocs)
	}

	small := overheadScaleFixture(overheadSmallMembers)
	extraMembers := float64(overheadRows * (overheadAllocMembers - overheadSmallMembers))
	smallAllocs, midAllocs := overheadAllocs(t, small), overheadAllocs(t, overheadScaleFixture(overheadAllocMembers))
	allocsPerMember := (midAllocs - smallAllocs) / extraMembers
	t.Logf("writer-core overhead member scale: members/row %d allocs=%.0f, members/row %d allocs=%.0f, marginal allocs/member=%.3f",
		overheadSmallMembers, smallAllocs, overheadAllocMembers, midAllocs, allocsPerMember)
	if allocsPerMember > overheadAllocsPerMember {
		t.Fatalf("marginal allocs per advisory-group member %.3f exceed %.1f: the member union does super-linear work",
			allocsPerMember, overheadAllocsPerMember)
	}

	// A real super-linear union fails every attempt; a load spike that
	// disturbs the large scale point in every round of one attempt does not
	// repeat across independent attempts, so the check passes on the first
	// attempt within the bound and fails only when all attempts exceed it.
	// An attempt whose small-scale cost is not positive (a disturbed zero
	// point) measured nothing, so it is a miss like an over-limit attempt.
	zero, large := overheadScaleFixture(0), overheadScaleFixture(overheadLargeMembers)
	var growth float64
	measured := false
	for attempt := 1; attempt <= overheadTimingAttempts; attempt++ {
		var smallCost float64
		smallCost, growth = overheadCostGrowth(t, zero, small, large)
		if smallCost <= 0 {
			t.Logf("attempt %d/%d: small-scale per-member cost %.0fns is not positive; the attempt measured nothing", attempt, overheadTimingAttempts, smallCost)
			continue
		}
		measured = true
		if growth <= overheadCostGrowthFactor {
			return
		}
		t.Logf("attempt %d/%d: per-member cost grew %.2fx (limit %.1fx)", attempt, overheadTimingAttempts, growth, overheadCostGrowthFactor)
	}
	if !measured {
		t.Fatalf("small-scale per-member cost was not positive in all %d attempts: the timing scale points no longer measure the member union", overheadTimingAttempts)
	}
	t.Fatalf("per-member cost grew %.2fx between %d and %d members per row in all %d attempts (limit %.1fx): the member union is not linear",
		growth, overheadSmallMembers, overheadLargeMembers, overheadTimingAttempts, overheadCostGrowthFactor)
}

// overheadCostGrowth interleaves the zero, small, and large scale points
// across overheadTimingRounds rounds so a load phase hits all three, takes
// each point's minimum (the pass the scheduler disturbed least), and returns
// the small-scale per-member cost in nanoseconds and the factor by which the
// per-member cost grew from the small to the large scale. A grossly
// super-linear union ends the attempt early (see overheadEarlyStopFactor).
func overheadCostGrowth(t *testing.T, zero, small, large overheadFixture) (smallCost, growth float64) {
	t.Helper()
	minZero, minSmall, minLarge := time.Duration(1<<62), time.Duration(1<<62), time.Duration(1<<62)
	rounds := 0
	for range overheadTimingRounds {
		rounds++
		minZero = min(minZero, overheadTime(t, zero))
		minSmall = min(minSmall, overheadTime(t, small))
		minLarge = min(minLarge, overheadTime(t, large))
		smallCost = float64(minSmall-minZero) / float64(overheadRows*overheadSmallMembers)
		largeCost := float64(minLarge-minSmall) / float64(overheadRows*(overheadLargeMembers-overheadSmallMembers))
		growth = 0
		if smallCost > 0 {
			growth = largeCost / smallCost
		}
		if rounds >= overheadEarlyStopRounds && growth > overheadEarlyStopFactor*overheadCostGrowthFactor {
			break
		}
	}
	largeCost := growth * smallCost
	t.Logf("writer-core overhead timing minimums over %d interleaved rounds: members/row 0=%.1fus %d=%.1fus %d=%.1fus; per-member cost %.0fns -> %.0fns (growth %.2fx)",
		rounds, float64(minZero)/1000, overheadSmallMembers, float64(minSmall)/1000,
		overheadLargeMembers, float64(minLarge)/1000, smallCost, largeCost, growth)
	return smallCost, growth
}
