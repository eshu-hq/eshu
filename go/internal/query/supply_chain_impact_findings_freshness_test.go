package query

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestSupplyChainImpactWinnersWatermarkGate pins the probe contract: the legacy
// live read issues no watermark query and never claims to serve from winners,
// while the winners read issues exactly the watermark probe and reports serving
// from winners even when the probe itself errors (so the handler can downgrade to
// unavailable instead of falsely reporting fresh).
func TestSupplyChainImpactWinnersWatermarkGate(t *testing.T) {
	t.Parallel()

	recOff := &recordingImpactQueryer{}
	storeOff := NewPostgresSupplyChainImpactFindingStoreWithReadModel(recOff, false)
	off, err := storeOff.SupplyChainImpactWinnersWatermark(context.Background())
	if err != nil {
		t.Fatalf("gate-off watermark returned error: %v", err)
	}
	if off.ServingFromWinners {
		t.Fatal("gate-off read must not report serving from winners")
	}
	if recOff.lastQuery != "" {
		t.Fatalf("gate-off read must not issue a probe query, got %q", recOff.lastQuery)
	}

	recOn := &recordingImpactQueryer{}
	storeOn := NewPostgresSupplyChainImpactFindingStoreWithReadModel(recOn, true)
	on, err := storeOn.SupplyChainImpactWinnersWatermark(context.Background())
	if err == nil {
		t.Fatal("recordingImpactQueryer always errors; expected the probe error to propagate")
	}
	if !on.ServingFromWinners {
		t.Fatal("gate-on read must report serving from winners even when the probe errors")
	}
	if recOn.lastQuery != selectSupplyChainImpactWinnersWatermarkQuery {
		t.Fatalf("gate-on read issued the wrong probe query: %q", recOn.lastQuery)
	}
}

// TestApplyWinnersFreshness exhaustively pins the watermark→envelope mapping: the
// legacy live read stays fresh, a recent resweep stays fresh (with the watermark
// surfaced as observed_at), a resweep older than the window is stale with a
// reducer_backlog cause, an unpopulated table is building, and a probe error is
// reported unavailable rather than silently fresh.
func TestApplyWinnersFreshness(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name         string
		fr           SupplyChainImpactWinnersFreshness
		probeErr     error
		wantState    FreshnessState
		wantCause    FreshnessCause
		wantObserved bool
		wantNext     bool
	}{
		{
			name:      "legacy live read untouched",
			fr:        SupplyChainImpactWinnersFreshness{ServingFromWinners: false},
			wantState: FreshnessFresh,
		},
		{
			name:      "legacy live read untouched even on probe error",
			fr:        SupplyChainImpactWinnersFreshness{ServingFromWinners: false},
			probeErr:  errors.New("boom"),
			wantState: FreshnessFresh,
		},
		{
			name:         "winners fresh within window",
			fr:           SupplyChainImpactWinnersFreshness{ServingFromWinners: true, Present: true, MaterializedAt: base.Add(-30 * time.Second)},
			wantState:    FreshnessFresh,
			wantObserved: true,
		},
		{
			name:         "winners stale beyond window",
			fr:           SupplyChainImpactWinnersFreshness{ServingFromWinners: true, Present: true, MaterializedAt: base.Add(-10 * time.Minute)},
			wantState:    FreshnessStale,
			wantCause:    FreshnessCauseReducerBacklog,
			wantObserved: true,
			wantNext:     true,
		},
		{
			name:      "winners empty is building",
			fr:        SupplyChainImpactWinnersFreshness{ServingFromWinners: true, Present: false},
			wantState: FreshnessBuilding,
			wantCause: FreshnessCauseReducerBacklog,
			wantNext:  true,
		},
		{
			name:      "probe error is unavailable not fresh",
			fr:        SupplyChainImpactWinnersFreshness{ServingFromWinners: true},
			probeErr:  errors.New("boom"),
			wantState: FreshnessUnavailable,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			truth := &TruthEnvelope{Freshness: TruthFreshness{State: FreshnessFresh}}
			applyWinnersFreshness(truth, tc.fr, tc.probeErr, base)

			if truth.Freshness.State != tc.wantState {
				t.Fatalf("state = %q, want %q", truth.Freshness.State, tc.wantState)
			}
			if truth.Freshness.Cause != tc.wantCause {
				t.Fatalf("cause = %q, want %q", truth.Freshness.Cause, tc.wantCause)
			}
			if gotNext := truth.Freshness.NextCheck != nil; gotNext != tc.wantNext {
				t.Fatalf("next_check present = %v, want %v", gotNext, tc.wantNext)
			}
			if gotObserved := truth.Freshness.ObservedAt != ""; gotObserved != tc.wantObserved {
				t.Fatalf("observed_at present = %v, want %v", gotObserved, tc.wantObserved)
			}
		})
	}
}
