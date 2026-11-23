// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import (
	"context"
	"errors"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestFirstUnderlyingDataAtParsesTheWireField pins the Decision-3
// (eshu-hq/eshu#11080) wire read: the producer stamps the touching pass's
// cycle start as an RFC3339Nano string under
// PayloadKeyFirstUnderlyingDataAt for telemetry/provenance and packet
// discrimination. The queue JSON round-trip delivers strings; a hand-built
// intent may carry a time.Time. Anything else reads as absent.
func TestFirstUnderlyingDataAtParsesTheWireField(t *testing.T) {
	t.Parallel()
	stamp := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	now := stamp.Add(10 * time.Hour)

	tests := []struct {
		name    string
		payload map[string]any
		want    time.Time
		wantOK  bool
	}{
		{
			name:    "rfc3339nano string parses",
			payload: map[string]any{PayloadKeyFirstUnderlyingDataAt: "2026-11-09T12:00:00Z"},
			want:    stamp,
			wantOK:  true,
		},
		{
			name:    "time value passes through",
			payload: map[string]any{PayloadKeyFirstUnderlyingDataAt: stamp},
			want:    stamp,
			wantOK:  true,
		},
		{
			name:    "absent field falls back",
			payload: nil,
			wantOK:  false,
		},
		{
			name:    "unparsable string falls back",
			payload: map[string]any{PayloadKeyFirstUnderlyingDataAt: "not-a-time"},
			wantOK:  false,
		},
		{
			name:    "wrong type falls back",
			payload: map[string]any{PayloadKeyFirstUnderlyingDataAt: 12345},
			wantOK:  false,
		},
		{
			name:    "zero time falls back",
			payload: map[string]any{PayloadKeyFirstUnderlyingDataAt: time.Time{}.Format(time.RFC3339Nano)},
			wantOK:  false,
		},
		{
			name:    "future stamp falls back to the post-claim clock",
			payload: map[string]any{PayloadKeyFirstUnderlyingDataAt: now.Add(time.Hour).UTC().Format(time.RFC3339Nano)},
			wantOK:  false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			intent := advisoryGroupIntent(EntityKey("CVE-2026-first-data"))
			intent.Payload = test.payload
			got, ok := firstUnderlyingDataAt(intent, now)
			if ok != test.wantOK {
				t.Fatalf("firstUnderlyingDataAt ok = %v, want %v", ok, test.wantOK)
			}
			if ok && !got.Equal(test.want) {
				t.Fatalf("firstUnderlyingDataAt = %v, want %v", got, test.want)
			}
		})
	}
}

// TestAdvisoryOriginDerivesNVDGHSAOther pins the per-verdict origin
// attribution for watermark_waits_total (#11080): CVE-class group ids read
// nvd, GHSA-class ids read ghsa, everything else reads other. The split
// mirrors the #11078 dirty-log proxy (advisory_id prefix), applied to the
// group id because the verdict fires before any winner row is read.
func TestAdvisoryOriginDerivesNVDGHSAOther(t *testing.T) {
	t.Parallel()
	tests := []struct {
		groupID string
		want    string
	}{
		{"CVE-2026-0001", AdvisoryOriginNVD},
		{"cve-2026-0002", AdvisoryOriginNVD},
		{"GHSA-aaaa-bbbb-cccc", AdvisoryOriginGHSA},
		{"DEBIAN-CVE-2026-0001", AdvisoryOriginOther},
		{"DSA-6000-1", AdvisoryOriginOther},
		{"", AdvisoryOriginOther},
	}
	for _, test := range tests {
		t.Run(test.groupID, func(t *testing.T) {
			t.Parallel()
			if got := advisoryOrigin(test.groupID); got != test.want {
				t.Fatalf("advisoryOrigin(%q) = %q, want %q", test.groupID, got, test.want)
			}
		})
	}
}

// TestStampedAncientDataStillDefersNotReadyOnFirstEval pins the
// lane-ruling correction: a first evaluation whose stamped
// first-underlying-data time is older than the stale bound still defers
// not_ready under the post-claim first-deferral clock. The stamp is
// telemetry/provenance only and must not drive the verdict.
func TestStampedAncientDataStillDefersNotReadyOnFirstEval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := handlerTestEnqueuedAt.Add(10 * time.Minute)
	intent := advisoryGroupIntent(EntityKey("GHSA-ancient-stamp"))
	intent.Payload = map[string]any{
		PayloadKeyFirstUnderlyingDataAt: handlerTestEnqueuedAt.Add(-time.Hour).UTC().Format(time.RFC3339Nano),
	}
	handler := Handler{
		Winners: &fakeWinnersReader{watermark: handlerTestEnqueuedAt.Add(-time.Hour)},
		Writer:  &fakeWriter{},
		Now:     func() time.Time { return now },
	}
	_, err := handler.Handle(ctx, intent)
	if err == nil {
		t.Fatal("stamped ancient data behind the watermark: want a deferral error")
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() != WinnersWatermarkNotReadyFailureClass {
		t.Fatalf("failure class = %v, want %q (the stamp must not drive the verdict)", err, WinnersWatermarkNotReadyFailureClass)
	}
	if !reducercontract.IsExpectedWait(err) {
		t.Fatalf("first-deferral not_ready must stay an expected wait: %v", err)
	}
}

// TestStampedFreshDataKeepsTheNotReadyCycle pins the other side: stamped
// data younger than the bound changes nothing, the post-claim clock still
// governs the verdict.
func TestStampedFreshDataKeepsTheNotReadyCycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := handlerTestEnqueuedAt.Add(10 * time.Minute)
	intent := advisoryGroupIntent(EntityKey("GHSA-fresh-stamp"))
	intent.Payload = map[string]any{
		PayloadKeyFirstUnderlyingDataAt: handlerTestEnqueuedAt.Add(-5 * time.Minute).UTC().Format(time.RFC3339Nano),
	}
	handler := Handler{
		Winners: &fakeWinnersReader{watermark: handlerTestEnqueuedAt.Add(-time.Hour)},
		Writer:  &fakeWriter{},
		Now:     func() time.Time { return now },
	}
	_, err := handler.Handle(ctx, intent)
	if err == nil {
		t.Fatal("fresh stamped data behind the watermark: want a deferral error")
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() != WinnersWatermarkNotReadyFailureClass {
		t.Fatalf("failure class = %v, want %q (fresh stamp leaves the post-claim clock in charge)", err, WinnersWatermarkNotReadyFailureClass)
	}
}
