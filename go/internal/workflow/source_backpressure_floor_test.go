// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"testing"
	"time"
)

// signalFromRows builds the observation the store derives from a source's
// claimable row ages: total depth, oldest age, and how many rows are past
// the engage and release ages. It mirrors the two COUNT FILTER columns.
func signalFromRows(cfg SourceBackpressureConfig, source string, ages []time.Duration) SourceQueueSignal {
	signal := SourceQueueSignal{SourceSystem: source, ClaimableDepth: int64(len(ages))}
	for _, age := range ages {
		if age > signal.OldestClaimableAge {
			signal.OldestClaimableAge = age
		}
		if age > cfg.EngageAge {
			signal.RowsPastEngageAge++
		}
		if age > cfg.ReleaseAge {
			signal.RowsPastReleaseAge++
		}
	}
	return signal
}

func repeatAge(age time.Duration, n int) []time.Duration {
	ages := make([]time.Duration, n)
	for i := range ages {
		ages[i] = age
	}
	return ages
}

// TestDecideSourceSlowedOneOldRowAmongFreshRowsDoesNotEngage is #10882 F5:
// the floor counts rows past the engage age, not all claimable rows. One
// poisoned old row on a busy source (1,000 fresh rows) must not slow it.
func TestDecideSourceSlowedOneOldRowAmongFreshRowsDoesNotEngage(t *testing.T) {
	t.Parallel()

	cfg := testSourceBackpressureConfig()
	ages := append(repeatAge(time.Minute, 1000), 3*time.Hour)
	got := DecideSourceSlowed(cfg, signalFromRows(cfg, "git", ages), SourceBackpressureLatch{}, 0)
	if got.Slowed {
		t.Fatal("one old row + 1000 fresh rows slowed the source, want not slowed")
	}
}

// TestDecideSourceSlowedFloorOldRowsEngage: exactly DepthFloor rows past the
// engage age engage; one fewer does not.
func TestDecideSourceSlowedFloorOldRowsEngage(t *testing.T) {
	t.Parallel()

	cfg := testSourceBackpressureConfig()
	atFloor := append(repeatAge(time.Minute, 500), repeatAge(2*time.Hour, int(cfg.DepthFloor))...)
	if got := DecideSourceSlowed(cfg, signalFromRows(cfg, "git", atFloor), SourceBackpressureLatch{}, 0); !got.Slowed {
		t.Fatal("DepthFloor old rows did not engage, want slowed")
	}
	belowFloor := append(repeatAge(time.Minute, 500), repeatAge(2*time.Hour, int(cfg.DepthFloor)-1)...)
	if got := DecideSourceSlowed(cfg, signalFromRows(cfg, "git", belowFloor), SourceBackpressureLatch{}, 0); got.Slowed {
		t.Fatal("DepthFloor-1 old rows engaged, want not slowed")
	}
}

// TestDecideSourceSlowedReleasesWhenFewerThanFloorRowsPastReleaseAge: the
// release side uses the same floor over the release age, so a handful of
// poisoned rows left behind cannot hold the latch once the real backlog has
// cleared, and a real backlog of floor rows still holds it.
func TestDecideSourceSlowedReleasesWhenFewerThanFloorRowsPastReleaseAge(t *testing.T) {
	t.Parallel()

	cfg := testSourceBackpressureConfig()
	engaged := SourceBackpressureLatch{Engaged: true}
	released := append(repeatAge(time.Minute, 400), repeatAge(30*time.Minute, int(cfg.DepthFloor)-1)...)
	if got := DecideSourceSlowed(cfg, signalFromRows(cfg, "git", released), engaged, 0); got.Slowed {
		t.Fatal("engaged with DepthFloor-1 rows past the release age stayed slowed, want released")
	}
	held := append(repeatAge(time.Minute, 400), repeatAge(30*time.Minute, int(cfg.DepthFloor))...)
	if got := DecideSourceSlowed(cfg, signalFromRows(cfg, "git", held), engaged, 0); !got.Slowed {
		t.Fatal("engaged with DepthFloor rows past the release age released, want slowed")
	}
}

// TestDecideSourceSlowedFloorOneReproducesAgeOnlyVerdicts is the equivalence
// table: with DepthFloor=1 the row-count verdict equals the oldest-age verdict
// shipped before (engage when oldest > engage age with depth >= 1, release
// when oldest <= release age) for every latch state and age boundary.
func TestDecideSourceSlowedFloorOneReproducesAgeOnlyVerdicts(t *testing.T) {
	t.Parallel()

	cfg := testSourceBackpressureConfig()
	cfg.DepthFloor = 1
	boundary := []time.Duration{
		0, time.Second, cfg.ReleaseAge - time.Second, cfg.ReleaseAge, cfg.ReleaseAge + time.Second,
		cfg.EngageAge - time.Second, cfg.EngageAge, cfg.EngageAge + time.Second, 10 * cfg.EngageAge,
	}
	shapes := [][]time.Duration{nil}
	for _, age := range boundary {
		shapes = append(shapes, []time.Duration{age}, []time.Duration{age, time.Second}, append(repeatAge(time.Second, 50), age))
	}
	for _, engaged := range []bool{false, true} {
		for i, ages := range shapes {
			signal := signalFromRows(cfg, "git", ages)
			var want bool
			if engaged {
				want = signal.OldestClaimableAge > cfg.ReleaseAge
			} else {
				want = signal.OldestClaimableAge > cfg.EngageAge && signal.ClaimableDepth >= cfg.DepthFloor
			}
			got := DecideSourceSlowed(cfg, signal, SourceBackpressureLatch{Engaged: engaged}, 0)
			if got.Slowed != want {
				t.Fatalf("shape %d engaged=%v ages=%v: slowed=%v, want age-only %v", i, engaged, ages, got.Slowed, want)
			}
		}
	}
}
