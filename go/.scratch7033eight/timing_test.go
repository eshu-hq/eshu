package main

import (
	"slices"
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
	if err := validateMeasuredRound(block, workload, 2); err != nil {
		t.Fatalf("matching uncapped round failed: %v", err)
	}
	block[1].pageHash = "different"
	if err := validateMeasuredRound(block, workload, 2); err == nil {
		t.Fatal("uncapped page difference must fail")
	}
	if err := validateMeasuredRound(block, workload, 1); err == nil {
		t.Fatal("same-route page instability must fail")
	}
	block[2].pageHash = "different"
	if err := validateMeasuredRound(block, workload, 1); err != nil {
		t.Fatalf("capped page difference may differ: %v", err)
	}
}

func TestMedianDurationUsesAllSamples(t *testing.T) {
	got := medianDuration([]time.Duration{5 * time.Millisecond, time.Millisecond, 3 * time.Millisecond, 9 * time.Millisecond})
	if got != 4*time.Millisecond {
		t.Fatalf("median = %s, want 4ms", got)
	}
}

func TestSelectTimingWorkloadOnlyCanonical(t *testing.T) {
	workload, err := selectTimingWorkload("canonical", "sample-repo")
	if err != nil || workload.name != "canonical" || len(workload.terms) != 16 {
		t.Fatalf("canonical selection = %#v, %v", workload, err)
	}
	for _, name := range []string{"", "dynamic", "punctuation", "all", "unknown"} {
		if _, err := selectTimingWorkload(name, "sample-repo"); err == nil {
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
