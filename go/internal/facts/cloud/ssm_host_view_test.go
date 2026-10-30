// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import (
	"testing"
	"time"
)

var ssmViewTestHost = SSMHostTuple{
	Partition:  "aws",
	AccountID:  "000000000001",
	Region:     "us-east-1",
	InstanceID: "i-00000000000000001",
}

var ssmViewTestNow = time.Date(2026, 10, 29, 23, 0, 0, 0, time.UTC)

func TestAssessSSMHostApplicationStreamComplete(t *testing.T) {
	capture := "2026-10-29T22:31:19Z"
	rows := []SSMHostApplicationViewRow{
		{Host: ssmViewTestHost, CaptureTime: capture, RowHash: "aaa"},
		{Host: ssmViewTestHost, CaptureTime: capture, RowHash: "bbb"},
		{Host: ssmViewTestHost, CaptureTime: capture, RowHash: "aaa"},
	}
	got := AssessSSMHostApplicationStream(rows, ssmViewTestNow, time.Hour)
	if !got.Complete {
		t.Fatalf("Complete = false, reason %q, want true", got.Reason)
	}
	if got.Stale {
		t.Fatalf("Stale = true on a fresh capture")
	}
	if got.RowCount != 2 {
		t.Fatalf("RowCount = %d, want 2 (duplicate row hash deduplicates)", got.RowCount)
	}
	wantCapture, _ := time.Parse(time.RFC3339, capture)
	if !got.CaptureTime.Equal(wantCapture) {
		t.Fatalf("CaptureTime = %v, want %v", got.CaptureTime, wantCapture)
	}
}

func TestAssessSSMHostApplicationStreamIncoherentCapture(t *testing.T) {
	rows := []SSMHostApplicationViewRow{
		{Host: ssmViewTestHost, CaptureTime: "2026-10-29T22:31:19Z", RowHash: "aaa"},
		{Host: ssmViewTestHost, CaptureTime: "2026-10-29T22:35:44Z", RowHash: "bbb"},
	}
	got := AssessSSMHostApplicationStream(rows, ssmViewTestNow, time.Hour)
	if got.Complete {
		t.Fatalf("Complete = true on mixed capture times, want false (never pick a winner)")
	}
	if got.Reason == "" {
		t.Fatalf("Reason is empty on an incoherent stream")
	}
}

func TestAssessSSMHostApplicationStreamMissingCapture(t *testing.T) {
	rows := []SSMHostApplicationViewRow{
		{Host: ssmViewTestHost, RowHash: "aaa"},
	}
	got := AssessSSMHostApplicationStream(rows, ssmViewTestNow, time.Hour)
	if got.Complete {
		t.Fatalf("Complete = true on a missing capture time, want false (undated is unknown, never empty)")
	}
}

func TestAssessSSMHostApplicationStreamTombstonedIsUnknown(t *testing.T) {
	rows := []SSMHostApplicationViewRow{
		{Host: ssmViewTestHost, CaptureTime: "2026-10-29T22:31:19Z", RowHash: "aaa", Tombstone: true},
	}
	got := AssessSSMHostApplicationStream(rows, ssmViewTestNow, time.Hour)
	if got.Complete {
		t.Fatalf("Complete = true on a fully tombstoned stream, want false")
	}
	if got.Reason == "" {
		t.Fatalf("Reason is empty on a tombstoned stream")
	}
}

func TestAssessSSMHostApplicationStreamStale(t *testing.T) {
	rows := []SSMHostApplicationViewRow{
		{Host: ssmViewTestHost, CaptureTime: "2026-10-29T20:00:00Z", RowHash: "aaa"},
	}
	got := AssessSSMHostApplicationStream(rows, ssmViewTestNow, time.Hour)
	if got.Complete {
		t.Fatalf("Complete = true on an aged capture, want false")
	}
	if !got.Stale {
		t.Fatalf("Stale = false on a capture past the bound, reason %q", got.Reason)
	}
}

func TestAssessSSMHostApplicationStreamEmptyIsUnknown(t *testing.T) {
	got := AssessSSMHostApplicationStream(nil, ssmViewTestNow, time.Hour)
	if got.Complete || got.Stale {
		t.Fatalf("empty stream assessed complete=%v stale=%v, want both false", got.Complete, got.Stale)
	}
	if got.Reason == "" {
		t.Fatalf("Reason is empty on an empty stream")
	}
}

func TestAssessSSMHostPlatformStreamInventory(t *testing.T) {
	rows := []SSMHostPlatformViewRow{{
		Host:            ssmViewTestHost,
		SourceType:      "ssm_inventory",
		CaptureTime:     "2026-10-29T22:31:19Z",
		ObservationTime: "2026-10-29T22:33:02Z",
	}}
	got := AssessSSMHostPlatformStream(rows, ssmViewTestNow, time.Hour, 24*time.Hour)
	if !got.Complete {
		t.Fatalf("Complete = false, reason %q, want true", got.Reason)
	}
}

func TestAssessSSMHostPlatformStreamDescribeInstanceInformation(t *testing.T) {
	rows := []SSMHostPlatformViewRow{{
		Host:            ssmViewTestHost,
		SourceType:      "describe_instance_information",
		ObservationTime: "2026-10-29T22:50:00Z",
	}}
	got := AssessSSMHostPlatformStream(rows, ssmViewTestNow, time.Hour, 24*time.Hour)
	if !got.Complete {
		t.Fatalf("Complete = false, reason %q, want true (DII carries no capture time)", got.Reason)
	}
}

func TestAssessSSMHostPlatformStreamStaleDIIObservation(t *testing.T) {
	rows := []SSMHostPlatformViewRow{{
		Host:            ssmViewTestHost,
		SourceType:      "describe_instance_information",
		ObservationTime: "2026-10-20T22:50:00Z",
	}}
	got := AssessSSMHostPlatformStream(rows, ssmViewTestNow, time.Hour, 24*time.Hour)
	if got.Complete || !got.Stale {
		t.Fatalf("aged DII observation assessed complete=%v stale=%v, want false/true", got.Complete, got.Stale)
	}
}

func TestSSMHostStreamAssessmentAbsent(t *testing.T) {
	empty := AssessSSMHostApplicationStream(nil, ssmViewTestNow, time.Hour)
	if !empty.Absent() {
		t.Fatalf("empty stream Absent() = false, want true")
	}
	tombstoned := AssessSSMHostApplicationStream(
		[]SSMHostApplicationViewRow{{Host: ssmViewTestHost, CaptureTime: "2026-10-29T22:31:19Z", RowHash: "aaa", Tombstone: true}},
		ssmViewTestNow, time.Hour,
	)
	if !tombstoned.Absent() {
		t.Fatalf("withdrawn stream Absent() = false, want true")
	}
	incoherent := AssessSSMHostApplicationStream(
		[]SSMHostApplicationViewRow{
			{Host: ssmViewTestHost, CaptureTime: "2026-10-29T22:31:19Z", RowHash: "aaa"},
			{Host: ssmViewTestHost, CaptureTime: "2026-10-29T22:35:44Z", RowHash: "bbb"},
		},
		ssmViewTestNow, time.Hour,
	)
	if incoherent.Absent() {
		t.Fatalf("incoherent stream Absent() = true, want false (fresh negative signal, never absence)")
	}
	complete := AssessSSMHostApplicationStream(
		[]SSMHostApplicationViewRow{{Host: ssmViewTestHost, CaptureTime: "2026-10-29T22:31:19Z", RowHash: "aaa"}},
		ssmViewTestNow, time.Hour,
	)
	if complete.Absent() {
		t.Fatalf("complete stream Absent() = true, want false")
	}
}

func TestSSMSkewPairsWithinBound(t *testing.T) {
	app, _ := time.Parse(time.RFC3339, "2026-10-29T22:31:19Z")
	plat, _ := time.Parse(time.RFC3339, "2026-10-29T22:33:02Z")
	if !SSMSkewPairs(app, plat, 5*time.Minute) {
		t.Fatalf("103s skew does not pair within a 5m bound")
	}
	if SSMSkewPairs(app, plat, time.Minute) {
		t.Fatalf("103s skew pairs within a 1m bound")
	}
}
