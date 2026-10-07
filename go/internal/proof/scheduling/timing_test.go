// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

func TestTimingOrderInterleavesBothRoutes(t *testing.T) {
	got := timingOrder(3)
	want := []bool{
		false, true, true, false,
		false, true, true, false,
		false, true, true, false,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("timing order = %v, want %v", got, want)
	}
	if got := timingOrder(0); len(got) != 0 {
		t.Fatalf("zero-round timing order = %v", got)
	}
}

func TestTimedProbeFingerprintDetectsChangedRows(t *testing.T) {
	first := []codetopicparallel.ProbeRow{oracleEntity("first")}
	second := []codetopicparallel.ProbeRow{oracleEntity("second")}
	firstHash, err := hashRows(first)
	if err != nil {
		t.Fatal(err)
	}
	if same, err := timedProbeFingerprint(first, firstHash); err != nil || !same {
		t.Fatalf("same validated rows must match: same=%t err=%v", same, err)
	}
	if same, err := timedProbeFingerprint(second, firstHash); err != nil || same {
		t.Fatalf("changed rows must require persisted revalidation: same=%t err=%v", same, err)
	}
}

func TestValidateMeasuredRoundEnforcesUncappedPageParity(t *testing.T) {
	row := codetopicparallel.ProbeRow{SourceKind: "entity", MatchedTerm: "topic"}
	block := [4]measuredRequest{
		{rows: []codetopicparallel.ProbeRow{row}, pageHash: "same"},
		{rows: []codetopicparallel.ProbeRow{row}, pageHash: "same"},
		{rows: []codetopicparallel.ProbeRow{row}, pageHash: "same"},
		{rows: []codetopicparallel.ProbeRow{row}, pageHash: "same"},
	}
	workload := dynamicWorkload{terms: []string{"topic"}}
	if err := validateMeasuredRound(block, timingWitnessFor(t, block[0], block[1]), workload, 2); err != nil {
		t.Fatalf("matching uncapped round failed: %v", err)
	}
	block[1].pageHash = "different"
	if err := validateMeasuredRound(block, timingWitnessFor(t, block[0], block[0]), workload, 2); err == nil {
		t.Fatal("uncapped page difference must fail")
	}
	if err := validateMeasuredRound(block, timingWitnessFor(t, block[0], block[0]), workload, 1); err == nil {
		t.Fatal("same-route page instability must fail")
	}
	block[2].pageHash = "different"
	if err := validateMeasuredRound(block, timingWitnessFor(t, block[0], block[0]), workload, 1); err == nil {
		t.Fatal("same capped rows must keep the same page")
	}
}

func timingWitnessFor(t *testing.T, baseline, candidate measuredRequest) *timingWitness {
	t.Helper()
	baselineHash, err := hashRows(baseline.rows)
	if err != nil {
		t.Fatal(err)
	}
	candidateHash, err := hashRows(candidate.rows)
	if err != nil {
		t.Fatal(err)
	}
	return newTimingWitness([2]measuredRequest{baseline, candidate}, [2]string{baselineHash, candidateHash})
}

func TestValidateMeasuredRoundAllowsCappedSameRouteMembershipDrift(t *testing.T) {
	rows := func(first, second string) []codetopicparallel.ProbeRow {
		return []codetopicparallel.ProbeRow{
			probe("entity", "topic", first),
			probe("entity", "topic", second),
		}
	}
	block := [4]measuredRequest{
		{rows: rows("one", "four"), pageHash: "baseline-one"},
		{rows: rows("one", "five"), pageHash: "candidate-one"},
		{rows: rows("one", "six"), pageHash: "candidate-two"},
		{rows: rows("one", "seven"), pageHash: "baseline-two"},
	}
	witness := timingWitnessFor(t,
		measuredRequest{rows: rows("one", "two"), pageHash: "baseline-warmup"},
		measuredRequest{rows: rows("one", "three"), pageHash: "candidate-warmup"})
	if err := validateMeasuredRound(block, witness, dynamicWorkload{terms: []string{"topic"}}, 2); err != nil {
		t.Fatalf("valid capped selections should not force page equality: %v", err)
	}
}

func TestValidateMeasuredRoundChecksEveryRequestAgainstWarmup(t *testing.T) {
	rows := func(entity string) []codetopicparallel.ProbeRow {
		return []codetopicparallel.ProbeRow{probe("entity", "topic", entity)}
	}
	workload := dynamicWorkload{terms: []string{"topic"}}
	warmup := measuredRequest{rows: rows("one"), pageHash: "warmup"}
	block := [4]measuredRequest{
		{rows: rows("two"), pageHash: "same"},
		{rows: rows("two"), pageHash: "same"},
		{rows: rows("two"), pageHash: "same"},
		{rows: rows("two"), pageHash: "same"},
	}
	if err := validateMeasuredRound(block, timingWitnessFor(t, warmup, warmup), workload, 2); err == nil {
		t.Fatal("uncapped membership drift from warmup was accepted")
	}

	warmup = measuredRequest{rows: rows("one"), pageHash: "warmup"}
	block = [4]measuredRequest{
		{rows: rows("one"), pageHash: "warmup"},
		{rows: rows("one"), pageHash: "warmup"},
		{rows: rows("two"), pageHash: "new"},
		{rows: rows("one"), pageHash: "warmup"},
	}
	if err := validateMeasuredRound(block, timingWitnessFor(t, warmup, warmup), workload, 1); err != nil {
		t.Fatalf("changed capped membership on a later request should pass: %v", err)
	}
	block[2].rows = rows("one")
	if err := validateMeasuredRound(block, timingWitnessFor(t, warmup, warmup), workload, 1); err == nil {
		t.Fatal("later request kept warmup rows but changed page")
	}
}

func TestValidateMeasuredRoundRejectsReusedPoolWithDifferentPage(t *testing.T) {
	rows := func(entity string) []codetopicparallel.ProbeRow {
		return []codetopicparallel.ProbeRow{probe("entity", "topic", entity)}
	}
	warmup := measuredRequest{rows: rows("one"), pageHash: "warmup"}
	witness := timingWitnessFor(t, warmup, warmup)
	first := [4]measuredRequest{
		{rows: rows("two"), pageHash: "first"},
		{rows: rows("one"), pageHash: "warmup"},
		{rows: rows("one"), pageHash: "warmup"},
		{rows: rows("two"), pageHash: "first"},
	}
	workload := dynamicWorkload{terms: []string{"topic"}}
	if err := validateMeasuredRound(first, witness, workload, 1); err != nil {
		t.Fatalf("first capped round failed: %v", err)
	}
	second := first
	second[0].pageHash = "second"
	second[3].pageHash = "second"
	if err := validateMeasuredRound(second, witness, workload, 1); err == nil {
		t.Fatal("a repeated capped pool changed page across rounds")
	}
}

func TestValidateMeasuredRoundRejectsCrossRouteReusedPoolWithDifferentPage(t *testing.T) {
	rows := func(entity string) []codetopicparallel.ProbeRow {
		return []codetopicparallel.ProbeRow{probe("entity", "topic", entity)}
	}
	witness := timingWitnessFor(t,
		measuredRequest{rows: rows("baseline-warmup"), pageHash: "baseline-warmup"},
		measuredRequest{rows: rows("candidate-warmup"), pageHash: "candidate-warmup"})
	block := [4]measuredRequest{
		{rows: rows("reused"), pageHash: "first-page"},
		{rows: rows("candidate-one"), pageHash: "candidate-one"},
		{rows: rows("reused"), pageHash: "different-page"},
		{rows: rows("baseline-two"), pageHash: "baseline-two"},
	}
	if err := validateMeasuredRound(block, witness, dynamicWorkload{terms: []string{"topic"}}, 1); err == nil || !strings.Contains(err.Error(), "reused probe rows") {
		t.Fatalf("cross-route reuse must fail through the shared page witness: %v", err)
	}
}

func TestValidateMeasuredRoundRejectsCrossRoundCrossRoutePageDrift(t *testing.T) {
	rows := func(entity string) []codetopicparallel.ProbeRow {
		return []codetopicparallel.ProbeRow{probe("entity", "topic", entity)}
	}
	witness := timingWitnessFor(t,
		measuredRequest{rows: rows("baseline-warmup"), pageHash: "baseline-warmup"},
		measuredRequest{rows: rows("candidate-warmup"), pageHash: "candidate-warmup"})
	first := [4]measuredRequest{
		{rows: rows("reused"), pageHash: "first-page"},
		{rows: rows("candidate-one"), pageHash: "candidate-one"},
		{rows: rows("candidate-two"), pageHash: "candidate-two"},
		{rows: rows("baseline-two"), pageHash: "baseline-two"},
	}
	workload := dynamicWorkload{terms: []string{"topic"}}
	if err := validateMeasuredRound(first, witness, workload, 1); err != nil {
		t.Fatalf("first capped round failed: %v", err)
	}
	second := [4]measuredRequest{
		{rows: rows("baseline-three"), pageHash: "baseline-three"},
		{rows: rows("reused"), pageHash: "different-page"},
		{rows: rows("candidate-four"), pageHash: "candidate-four"},
		{rows: rows("baseline-four"), pageHash: "baseline-four"},
	}
	if err := validateMeasuredRound(second, witness, workload, 1); err == nil || !strings.Contains(err.Error(), "reused probe rows") {
		t.Fatalf("cross-round opposite-route reuse must fail through the shared page witness: %v", err)
	}
}

func TestValidateMeasuredRoundRejectsConflictingWarmupPages(t *testing.T) {
	row := probe("entity", "topic", "same")
	baseline := measuredRequest{rows: []codetopicparallel.ProbeRow{row}, pageHash: "first-page"}
	candidate := measuredRequest{rows: []codetopicparallel.ProbeRow{row}, pageHash: "different-page"}
	witness := timingWitnessFor(t, baseline, candidate)
	block := [4]measuredRequest{baseline, candidate, candidate, baseline}
	if err := validateMeasuredRound(block, witness, dynamicWorkload{terms: []string{"topic"}}, 1); err == nil || !strings.Contains(err.Error(), "warmup") {
		t.Fatalf("identical warmup rows must not witness two pages: %v", err)
	}
}

func TestValidateMeasuredRoundRetainsCardinalityAndScopeChecks(t *testing.T) {
	repo := "allowed"
	outside := "outside"
	row := probe("entity", "topic", "one")
	row.RepoID = &repo
	warmup := measuredRequest{rows: []codetopicparallel.ProbeRow{row}, pageHash: "warmup"}
	workload := dynamicWorkload{terms: []string{"topic"}, allowedRepos: []string{repo}}
	block := [4]measuredRequest{
		{rows: warmup.rows, pageHash: "warmup"},
		{rows: warmup.rows, pageHash: "warmup"},
		{rows: warmup.rows, pageHash: "warmup"},
		{rows: warmup.rows, pageHash: "warmup"},
	}
	block[3].rows = nil
	if err := validateMeasuredRound(block, timingWitnessFor(t, warmup, warmup), workload, 1); err == nil {
		t.Fatal("timed pool cardinality changed from warmup")
	}
	block[3] = warmup
	bad := row
	bad.RepoID = &outside
	block[2].rows = []codetopicparallel.ProbeRow{bad}
	if err := validateMeasuredRound(block, timingWitnessFor(t, warmup, warmup), workload, 1); err == nil {
		t.Fatal("timed row escaped repository scope")
	}
}

func TestMedianDurationUsesAllSamples(t *testing.T) {
	got := medianDuration([]time.Duration{5 * time.Millisecond, time.Millisecond, 3 * time.Millisecond, 9 * time.Millisecond})
	if got != 4*time.Millisecond {
		t.Fatalf("median = %s, want 4ms", got)
	}
}

func TestSelectTimingWorkloadOnlyCanonical(t *testing.T) {
	workload, err := selectTimingWorkload("canonical")
	if err != nil || workload.name != "canonical" || len(workload.terms) != 16 {
		t.Fatalf("canonical selection = %#v, %v", workload, err)
	}
	for _, name := range []string{"", "dynamic", "punctuation", "all", "unknown"} {
		if _, err := selectTimingWorkload(name); err == nil {
			t.Errorf("timing selector %q must fail closed", name)
		}
	}
}

func TestBlockRegressionStopsOnlyAboveTenPercent(t *testing.T) {
	if blockRegression(100*time.Millisecond, 110*time.Millisecond) {
		t.Fatal("exactly ten percent is not above the stop threshold")
	}
	if !blockRegression(100*time.Millisecond, 111*time.Millisecond) {
		t.Fatal("eleven percent must stop sampling")
	}
	if blockRegression(0, time.Millisecond) {
		t.Fatal("missing baseline must not be called a measured regression")
	}
}
