// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestManifestTriggerReopensAnInFlightHolderWithNoCommittedRows is the #10105
// review P2 regression: an advisory pass that loaded its evidence before the
// manifest commit and has committed no affected_package rows yet is invisible
// to the package-keyed holder read. The marker pass must union the in-flight
// scopes, read FIRST, and flag them through ReopenHolder, or that scope stays
// stale until its next generation.
func TestManifestTriggerReopensAnInFlightHolderWithNoCommittedRows(t *testing.T) {
	t.Parallel()

	port := &stubManifestPort{
		stubFanOut: &stubFanOut{
			inFlight: []string{"vuln-intel://osv/loading", manifestTriggerScope},
			outcomes: map[string]FanOutOutcome{"vuln-intel://osv/loading": FanOutReplayFlagged},
		},
		generations: []ManifestDependencyGeneration{activeFullChange()},
	}
	h, _ := manifestHandler(t, port, nil)
	result, err := h.Handle(context.Background(), manifestIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if want := []string{"vuln-intel://osv/loading"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want the in-flight scope %v and never the own scope", port.reopened, want)
	}
	if want := []string{"in-flight", "candidates"}; !reflect.DeepEqual(port.stubFanOut.reads, want) {
		t.Fatalf("read order = %v, want %v: the in-flight read must come first", port.stubFanOut.reads, want)
	}
	if got := result.SubSignals["fanout_replay_flagged"]; got != 1 {
		t.Fatalf("fanout_replay_flagged = %v, want 1", got)
	}
	if len(port.stamps) != 1 {
		t.Fatalf("stamps = %v, want the single-chunk pass stamped once", port.stamps)
	}
}

// TestManifestTriggerUnionsInFlightAndCommittedHolders proves the in-flight
// scopes join the committed holders, deduplicated, in one sorted set.
func TestManifestTriggerUnionsInFlightAndCommittedHolders(t *testing.T) {
	t.Parallel()

	port := &stubManifestPort{
		stubFanOut: &stubFanOut{
			candidates: standardCandidates(),
			inFlight:   []string{"vuln-intel://osv/a", "vuln-intel://osv/loading"},
		},
		generations: []ManifestDependencyGeneration{activeFullChange()},
	}
	h, _ := manifestHandler(t, port, nil)
	if _, err := h.Handle(context.Background(), manifestIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	want := []string{"vuln-intel://osv/a", "vuln-intel://osv/b", "vuln-intel://osv/c", "vuln-intel://osv/loading"}
	if !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
}

// TestManifestTriggerSkipsTheInFlightReadWhenNothingChanged proves the
// in-flight read runs under the same condition as the holder read: a pass
// with no changed package reads neither.
func TestManifestTriggerSkipsTheInFlightReadWhenNothingChanged(t *testing.T) {
	t.Parallel()

	generation := activeFullChange()
	generation.Current = generation.Baseline
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{inFlight: []string{"vuln-intel://osv/loading"}},
		generations: []ManifestDependencyGeneration{generation},
	}
	h, _ := manifestHandler(t, port, nil)
	if _, err := h.Handle(context.Background(), manifestIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if port.inFlightCalls != 0 || port.nonMarkerReads != 0 || len(port.reopened) != 0 {
		t.Fatalf("in-flight reads = %d/%d, reopened = %v; want none for an unchanged generation",
			port.inFlightCalls, port.nonMarkerReads, port.reopened)
	}
}

// TestManifestTriggerInFlightReadFailureIsRetryable proves an in-flight read
// failure fails the pass retryably and is counted like the CVE path's:
// read_timeout for a timeout, read_error otherwise, under the manifest cause.
func TestManifestTriggerInFlightReadFailureIsRetryable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err     error
		outcome string
	}{
		{fmt.Errorf("in-flight read: %w", ErrFanOutReadTimeout), fanOutOutcomeReadTimeout},
		{errors.New("connection reset"), fanOutOutcomeReadError},
	} {
		reader := sdkmetric.NewManualReader()
		inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
		if err != nil {
			t.Fatalf("NewInstruments() error = %v", err)
		}
		port := &stubManifestPort{
			stubFanOut:  &stubFanOut{candidates: standardCandidates(), inFlightErr: tc.err},
			generations: []ManifestDependencyGeneration{activeFullChange()},
		}
		h, _ := manifestHandler(t, port, inst)
		_, err = h.Handle(context.Background(), manifestIntent(1, nil))
		var retryable interface{ Retryable() bool }
		if !errors.As(err, &retryable) || !retryable.Retryable() {
			t.Fatalf("Handle() error = %v, want retryable", err)
		}
		if got := fanOutCauseCount(t, reader)[fanOutCauseManifestDependency+"/"+tc.outcome]; got != 1 {
			t.Fatalf("manifest_dependency %s count = %d, want 1", tc.outcome, got)
		}
		if len(port.reopened) != 0 || len(port.stamps) != 0 {
			t.Fatalf("failed pass reopened %v and stamped %v; want neither", port.reopened, port.stamps)
		}
	}
}

// TestManifestTriggerNeverFlagsAnotherInFlightMarker is the security review's
// livelock regression: an in-flight scope whose claimed item is itself a
// manifest marker is not a holder (a marker loads no evidence and writes no
// findings), and flagging it would rerun a marker that flags this one back.
// The marker pass reads the in-flight set without markers and never uses the
// CVE path's unfiltered read.
func TestManifestTriggerNeverFlagsAnotherInFlightMarker(t *testing.T) {
	t.Parallel()

	port := &stubManifestPort{
		stubFanOut: &stubFanOut{
			inFlight: []string{"git-repo-other", "vuln-intel://osv/loading"},
		},
		markerInFlight: []string{"git-repo-other"},
		generations:    []ManifestDependencyGeneration{activeFullChange()},
	}
	h, _ := manifestHandler(t, port, nil)
	if _, err := h.Handle(context.Background(), manifestIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if want := []string{"vuln-intel://osv/loading"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v: another scope's in-flight marker must not be flagged", port.reopened, want)
	}
	if port.inFlightCalls != 0 || port.nonMarkerReads != 1 {
		t.Fatalf("unfiltered in-flight reads = %d, marker-free reads = %d; want 0 and 1", port.inFlightCalls, port.nonMarkerReads)
	}
}
