// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
)

// synthClaim builds the host request the worker sends for one claimed scope:
// the dedicated synthetic kind, a pinned holder identity, and a bounded
// deadline. The emitter must return it verbatim.
func synthClaim() sdkcollector.Claim {
	return sdkcollector.Claim{
		ComponentID:   "dev.eshu.collector.synth-8877-cutover",
		InstanceID:    "synth-8877-cutover-instance-1",
		CollectorKind: "synth_zero_fact",
		SourceSystem:  "dev.eshu.synth-8877-cutover",
		Scope:         sdkcollector.Scope{ID: "synth/scope/1", Kind: "component"},
		SourceRunID:   "run-1",
		GenerationID:  "gen-1",
		WorkItemID:    "work-1",
		FencingToken:  "7",
		Attempt:       1,
		Deadline:      time.Date(2026, time.November, 5, 4, 0, 0, 0, time.UTC),
		ConfigHandle:  "synth-handle",
	}
}

func synthRequest() extensionhost.Request {
	return extensionhost.Request{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		Claim:           synthClaim(),
		Contract:        sdkcollector.Contract{ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1},
	}
}

// TestServeReturnsUnchangedWithEmptyFacts pins the window contract: one
// request in, one unchanged result out, status rows only, zero facts.
func TestServeReturnsUnchangedWithEmptyFacts(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(synthRequest())
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var stdout bytes.Buffer
	if err := serve(bytes.NewReader(raw), &stdout); err != nil {
		t.Fatalf("serve() error = %v, want nil", err)
	}
	var result sdkcollector.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.State != sdkcollector.ResultUnchanged {
		t.Fatalf("result state = %q, want %q", result.State, sdkcollector.ResultUnchanged)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("facts = %d, want 0: the window emitter never emits", len(result.Facts))
	}
	if len(result.Statuses) != 1 {
		t.Fatalf("statuses = %d, want exactly 1", len(result.Statuses))
	}
	status := result.Statuses[0]
	if status.Class != sdkcollector.StatusComplete || status.FactCount != 0 {
		t.Fatalf("status = %+v, want {complete, fact_count 0}", status)
	}
	if result.Generation.ID != "gen-1" {
		t.Fatalf("generation id = %q, want the claim generation", result.Generation.ID)
	}
	if result.Page != nil {
		t.Fatal("page envelope must stay absent: the legacy host path fails closed on it")
	}
}

// TestServeEchoesClaimIdentity proves the emitter returns the host claim
// verbatim: the host terminal-fails any identity mismatch before commit.
func TestServeEchoesClaimIdentity(t *testing.T) {
	t.Parallel()

	want := synthRequest()
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var stdout bytes.Buffer
	if err := serve(bytes.NewReader(raw), &stdout); err != nil {
		t.Fatalf("serve() error = %v, want nil", err)
	}
	var result sdkcollector.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	got, err := json.Marshal(result.Claim)
	if err != nil {
		t.Fatalf("marshal result claim: %v", err)
	}
	wantRaw, err := json.Marshal(want.Claim)
	if err != nil {
		t.Fatalf("marshal want claim: %v", err)
	}
	if !bytes.Equal(got, wantRaw) {
		t.Fatalf("claim echo mismatch:\n got %s\nwant %s", got, wantRaw)
	}
}

// TestServeRejectsMalformedInput proves a undecodable request fails closed
// with no result on stdout.
func TestServeRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	if err := serve(strings.NewReader("{not json"), &stdout); err == nil {
		t.Fatal("serve() error = nil for malformed input, want failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %d bytes, want none on failure", stdout.Len())
	}
}

// BenchmarkServeUnchanged measures the per-claim emitter cost the
// performance-evidence gate tracks: one bounded request in, one unchanged
// result out.
func BenchmarkServeUnchanged(b *testing.B) {
	raw, err := json.Marshal(synthRequest())
	if err != nil {
		b.Fatalf("marshal request: %v", err)
	}
	b.ReportMetric(float64(len(raw)), "input-bytes/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := serve(bytes.NewReader(raw), io.Discard); err != nil {
			b.Fatalf("serve() error = %v", err)
		}
	}
}

// TestServeRejectsOversizeInput proves stdin is bounded: a request past the
// cap fails closed instead of allocating unbounded input.
func TestServeRejectsOversizeInput(t *testing.T) {
	t.Parallel()

	big := bytes.Repeat([]byte("x"), maxRequestBytes+1)
	var stdout bytes.Buffer
	if err := serve(bytes.NewReader(big), &stdout); err == nil {
		t.Fatal("serve() error = nil for oversize input, want failure")
	}
}
