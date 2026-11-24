// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
)

// scriptedNVDBaselineDrainStore serves per-key drain answers the shared
// fake cannot express: a timeout for one generation beside a state for
// another. Keys the script does not name fail like the shared fake, so a
// test that forgets a key holds its year instead of planning on zero.
type scriptedNVDBaselineDrainStore struct {
	states map[vulnerability.NVDBaselineDrainKey]vulnerability.NVDBaselineDrainState
	errs   map[vulnerability.NVDBaselineDrainKey]error
	keys   []vulnerability.NVDBaselineDrainKey
}

func (s *scriptedNVDBaselineDrainStore) ReadNVDBaselineDrain(_ context.Context, key vulnerability.NVDBaselineDrainKey) (vulnerability.NVDBaselineDrainState, error) {
	s.keys = append(s.keys, key)
	if err, ok := s.errs[key]; ok {
		return vulnerability.NVDBaselineDrainState{}, err
	}
	if state, ok := s.states[key]; ok {
		return state, nil
	}
	return vulnerability.NVDBaselineDrainState{}, errBaselineStoreUnreachable
}

// TestLoadNVDBaselineDrainForPlanProbesEveryUndrainedGeneration is the
// failing-first proof for the eshu-hq/eshu#11380 probe-half fix: the loader
// probes the checkpoint's last chunk AND every older undrained window the
// shared set names, then folds to one state per year. A drained last chunk
// behind a projecting orphan holds the year as projecting with the orphan's
// age, instead of releasing the slot the way the last-only probe did.
func TestLoadNVDBaselineDrainForPlanProbesEveryUndrainedGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	last := vulnerability.NVDBaselineDrainKey{ScopeID: "scope-2002-new", GenerationID: "gen-2002-new"}
	orphan := vulnerability.NVDBaselineDrainKey{ScopeID: "scope-2002-old", GenerationID: "gen-2002-old"}
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{
		2002: {
			LastScopeID:      last.ScopeID,
			LastGenerationID: last.GenerationID,
			UndrainedGenerations: []vulnerability.NVDBaselineDrainKey{
				orphan,
				{ScopeID: last.ScopeID, GenerationID: last.GenerationID},
				{ScopeID: "", GenerationID: ""},
			},
		},
	}
	since := now.Add(-2 * time.Hour)
	drainedAt := now.Add(-time.Hour)
	fx.drain.states = map[vulnerability.NVDBaselineDrainKey]vulnerability.NVDBaselineDrainState{
		last:   {Phase: vulnerability.NVDBaselineDrainDrained, DrainedAt: drainedAt},
		orphan: {Phase: vulnerability.NVDBaselineDrainProjecting, NotDoneSince: since},
	}

	got := fx.service.loadNVDBaselineDrainForPlan(context.Background(), fx.checkpoints.checkpoints)

	state, ok := got[2002]
	if !ok {
		t.Fatalf("drain = %+v, want the 2002 year folded from both generations", got)
	}
	if state.Phase != vulnerability.NVDBaselineDrainProjecting || !state.NotDoneSince.Equal(since) {
		t.Fatalf("drain[2002] = %+v, want projecting since %v (the orphan holds the year)", state, since)
	}
	if len(fx.drain.keys) != 2 {
		t.Fatalf("probed keys = %+v, want exactly the last chunk and the orphan once each (deduped, blanks skipped)", fx.drain.keys)
	}
}

// TestLoadNVDBaselineDrainForPlanKeepsSingleKeyBehavior pins the identity:
// a year with no older undrained windows probes exactly its last chunk,
// so every existing single-key fixture keeps its planner input.
func TestLoadNVDBaselineDrainForPlanKeepsSingleKeyBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	key := vulnerability.NVDBaselineDrainKey{ScopeID: "scope-2002", GenerationID: "gen-2002"}
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{
		2002: {LastScopeID: key.ScopeID, LastGenerationID: key.GenerationID},
	}
	since := now.Add(-time.Hour)
	fx.drain.states = map[vulnerability.NVDBaselineDrainKey]vulnerability.NVDBaselineDrainState{
		key: {Phase: vulnerability.NVDBaselineDrainReducing, NotDoneSince: since},
	}

	got := fx.service.loadNVDBaselineDrainForPlan(context.Background(), fx.checkpoints.checkpoints)

	if len(got) != 1 || got[2002].Phase != vulnerability.NVDBaselineDrainReducing || !got[2002].NotDoneSince.Equal(since) {
		t.Fatalf("drain = %+v, want the last chunk's reducing state unchanged", got)
	}
	if len(fx.drain.keys) != 1 || fx.drain.keys[0] != key {
		t.Fatalf("probed keys = %+v, want exactly the last chunk", fx.drain.keys)
	}
}

// TestLoadNVDBaselineDrainForPlanHoldsOnExtraProbeFailure pins the
// fail-closed half: the last chunk drains but an older window's probe
// fails, so the year stays out of the map (drain_unloaded) instead of
// releasing on the drained last chunk alone.
func TestLoadNVDBaselineDrainForPlanHoldsOnExtraProbeFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	last := vulnerability.NVDBaselineDrainKey{ScopeID: "scope-2002-new", GenerationID: "gen-2002-new"}
	orphan := vulnerability.NVDBaselineDrainKey{ScopeID: "scope-2002-old", GenerationID: "gen-2002-old"}
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{
		2002: {
			LastScopeID:          last.ScopeID,
			LastGenerationID:     last.GenerationID,
			UndrainedGenerations: []vulnerability.NVDBaselineDrainKey{orphan},
		},
	}
	fx.service.NVDBaselineDrainStore = &scriptedNVDBaselineDrainStore{
		states: map[vulnerability.NVDBaselineDrainKey]vulnerability.NVDBaselineDrainState{
			last: {Phase: vulnerability.NVDBaselineDrainDrained, DrainedAt: now.Add(-time.Hour)},
		},
		errs: map[vulnerability.NVDBaselineDrainKey]error{
			orphan: &vulnerability.NVDBaselineDrainTimeoutError{Err: errBaselineStoreUnreachable},
		},
	}

	got := fx.service.loadNVDBaselineDrainForPlan(context.Background(), fx.checkpoints.checkpoints)

	if _, ok := got[2002]; ok {
		t.Fatalf("drain = %+v, want no 2002 entry: an unprovable window holds the year", got)
	}
}
