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
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestManifestTriggerReplayMatrix is the manifest-origin case of the fan-out
// replay matrix: every way the queue can redeliver a marker item converges on
// the same holder set and never fans out twice from one stamp.
func TestManifestTriggerReplayMatrix(t *testing.T) {
	t.Parallel()

	origin := map[string]any{"source_scope_id": "vuln-intel://nvd/cve"}
	cases := []struct {
		name         string
		attempt      int
		payload      map[string]any
		outcomes     map[string]FanOutOutcome
		reopenErr    map[string]error
		wantErr      bool
		wantReopened []string
		wantConsume  bool
	}{
		{
			name: "first delivery", attempt: 1,
			wantReopened: []string{"vuln-intel://osv/a", "vuln-intel://osv/b", "vuln-intel://osv/c"},
		},
		{
			name: "idempotent redelivery sees queued holders", attempt: 1,
			outcomes: map[string]FanOutOutcome{
				"vuln-intel://osv/a": FanOutPendingNoop, "vuln-intel://osv/b": FanOutPendingNoop, "vuln-intel://osv/c": FanOutPendingNoop,
			},
			wantReopened: []string{"vuln-intel://osv/a", "vuln-intel://osv/b", "vuln-intel://osv/c"},
		},
		{
			name: "reopen failure fails the pass for retry", attempt: 1,
			reopenErr: map[string]error{"vuln-intel://osv/b": errors.New("lock timeout")}, wantErr: true,
			wantReopened: []string{"vuln-intel://osv/a"},
		},
		{
			name: "retry after a failed attempt reopens again", attempt: 2,
			wantReopened: []string{"vuln-intel://osv/a", "vuln-intel://osv/b", "vuln-intel://osv/c"},
		},
		// A fan-out reopen of the marker item (another pass flagged it in
		// flight) is not a one-hop no-op here: the marker's re-derivation IS
		// the holder fan-out, so the replay re-reads the generation and
		// reopens the holders, then consumes the stamp. It terminates because
		// a holder pass that changes nothing fans out nothing.
		{
			name: "fan-out reopen of a marker item still re-derives and consumes the stamp", attempt: 1,
			payload:      map[string]any{PayloadKeyFanOutOrigin: origin},
			wantReopened: []string{"vuln-intel://osv/a", "vuln-intel://osv/b", "vuln-intel://osv/c"}, wantConsume: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			port := &stubManifestPort{
				stubFanOut: &stubFanOut{
					candidates: standardCandidates(), outcomes: tc.outcomes, reopenErr: tc.reopenErr,
				},
				generations: []ManifestDependencyGeneration{activeFullChange()},
			}
			h, _ := manifestHandler(t, port, nil)
			_, err := h.Handle(context.Background(), manifestIntent(tc.attempt, tc.payload))
			if (err != nil) != tc.wantErr {
				t.Fatalf("Handle() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				var retryable interface{ Retryable() bool }
				if !errors.As(err, &retryable) || !retryable.Retryable() {
					t.Fatalf("error %v is not retryable", err)
				}
			}
			if !reflect.DeepEqual(port.reopened, tc.wantReopened) {
				t.Fatalf("reopened = %v, want %v", port.reopened, tc.wantReopened)
			}
			consumed := false
			for _, call := range port.stateCalls {
				consumed = consumed || call.ConsumeOrigin
			}
			if consumed != tc.wantConsume {
				t.Fatalf("stamp consumed = %v, want %v (state calls %+v)", consumed, tc.wantConsume, port.stateCalls)
			}
		})
	}
}

// TestManifestTriggerOverflowDrainsAcrossClaims proves the holder cap: more
// than fanOutHolderCap holders reopen across claims through the continuation
// list, and the continuation claim does not re-read the generation.
func TestManifestTriggerOverflowDrainsAcrossClaims(t *testing.T) {
	t.Parallel()

	var candidates []PackageHolderCandidate
	for i := range fanOutHolderCap + 10 {
		candidates = append(candidates, npmHolder(fmt.Sprintf("vuln-intel://osv/h%03d", i), "lodash"))
	}
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: candidates, payload: map[string]any{}},
		generations: []ManifestDependencyGeneration{activeFullChange()},
	}
	h, _ := manifestHandler(t, port, nil)

	if _, err := h.Handle(context.Background(), manifestIntent(1, port.payload)); err != nil {
		t.Fatalf("first claim error = %v", err)
	}
	if len(port.reopened) != fanOutHolderCap {
		t.Fatalf("first claim reopened %d, want the cap %d", len(port.reopened), fanOutHolderCap)
	}
	if got := len(fanOutRemaining(reducercontract.Intent{Payload: port.payload})); got != 10 {
		t.Fatalf("continuation list = %d, want 10", got)
	}
	if _, err := h.Handle(context.Background(), manifestIntent(1, port.payload)); err != nil {
		t.Fatalf("continuation claim error = %v", err)
	}
	if len(port.reopened) != fanOutHolderCap+10 {
		t.Fatalf("reopened after drain = %d, want %d", len(port.reopened), fanOutHolderCap+10)
	}
	if port.reads != 1 {
		t.Fatalf("generation reads = %d, want 1 (the continuation claim drains the list)", port.reads)
	}
	if _, left := port.payload[PayloadKeyFanOutRemaining]; left {
		t.Fatalf("continuation list survived the drain: %v", port.payload[PayloadKeyFanOutRemaining])
	}
}

// TestManifestTriggerHolderReadErrorIsRetryable proves a failed holder read
// fails the pass retryably and counts read_error under the manifest cause.
func TestManifestTriggerHolderReadErrorIsRetryable(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidateErr: errors.New("connection reset")},
		generations: []ManifestDependencyGeneration{activeFullChange()},
	}
	h, _ := manifestHandler(t, port, inst)
	_, err = h.Handle(context.Background(), manifestIntent(1, nil))
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("Handle() error = %v, want retryable", err)
	}
	if got := fanOutCauseCount(t, reader)[fanOutCauseManifestDependency+"/"+fanOutOutcomeReadError]; got != 1 {
		t.Fatalf("manifest_dependency read_error count = %d, want 1", got)
	}
}

// TestManifestTriggerCountsReopensUnderItsCause proves the operator signal:
// the existing fan-out counter carries cause=manifest_dependency.
func TestManifestTriggerCountsReopensUnderItsCause(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &stubManifestPort{
		stubFanOut:  &stubFanOut{candidates: standardCandidates()},
		generations: []ManifestDependencyGeneration{activeFullChange()},
	}
	h, _ := manifestHandler(t, port, inst)
	if _, err := h.Handle(context.Background(), manifestIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := fanOutCauseCount(t, reader)[fanOutCauseManifestDependency+"/"+string(FanOutReopened)]; got != 3 {
		t.Fatalf("manifest_dependency reopened count = %d, want 3", got)
	}
}

// TestNonMarkerIntentStillRunsTheFindingsPass guards the other direction: an
// intent without the marker never takes the branch.
func TestNonMarkerIntentStillRunsTheFindingsPass(t *testing.T) {
	t.Parallel()

	port := &stubManifestPort{stubFanOut: &stubFanOut{}, generations: []ManifestDependencyGeneration{activeFullChange()}}
	writer := &changeWriter{}
	h := fanOutHandler(port, writer, nil)
	if _, err := h.Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if port.reads != 0 || len(writer.writes) != 1 {
		t.Fatalf("non-marker intent: generation reads=%d writes=%d, want 0 and 1", port.reads, len(writer.writes))
	}
}

// fanOutCauseCount sums the fan-out counter by "cause/outcome".
func fanOutCauseCount(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_fanout_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				cause, _ := dp.Attributes.Value("cause")
				outcome, _ := dp.Attributes.Value("outcome")
				out[cause.AsString()+"/"+outcome.AsString()] += dp.Value
			}
		}
	}
	return out
}

// TestManifestTriggerGenerationReadTimeoutIsCountedAsTimeout proves the
// operator signal for a generation read that hit the holder-read statement
// timeout (#9874 F9): the pass fails retryably and counts read_timeout, not
// read_error, under cause=manifest_dependency.
func TestManifestTriggerGenerationReadTimeoutIsCountedAsTimeout(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &stubManifestPort{
		stubFanOut: &stubFanOut{candidates: standardCandidates()},
		readErr:    fmt.Errorf("read manifest view: %w", ErrFanOutReadTimeout),
	}
	h, _ := manifestHandler(t, port, inst)
	_, err = h.Handle(context.Background(), manifestIntent(1, nil))
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("Handle() error = %v, want retryable", err)
	}
	if got := fanOutCauseCount(t, reader)[fanOutCauseManifestDependency+"/"+fanOutOutcomeReadTimeout]; got != 1 {
		t.Fatalf("manifest_dependency read_timeout count = %d, want 1", got)
	}
}
