// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package clock

import (
	"sync"
	"testing"
	"time"
)

func TestSystemClockTracksWallClock(t *testing.T) {
	t.Parallel()
	before := time.Now()
	got := System().Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("System().Now() = %s, want within [%s, %s]", got, before, after)
	}
}

func TestNowFuncAdaptsClosure(t *testing.T) {
	t.Parallel()
	fixed := time.Date(2026, time.June, 28, 12, 0, 0, 0, time.UTC)
	var c Clock = NowFunc(func() time.Time { return fixed })
	if got := c.Now(); !got.Equal(fixed) {
		t.Fatalf("NowFunc.Now() = %s, want %s", got, fixed)
	}
}

func TestSimulatedNowAndAdvance(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.June, 28, 0, 0, 0, 0, time.UTC)
	c := NewSimulated(start)
	if got := c.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %s, want %s", got, start)
	}

	c.Advance(90 * time.Second)
	want := start.Add(90 * time.Second)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("after Advance, Now() = %s, want %s", got, want)
	}

	c.Advance(0) // no-op
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("after Advance(0), Now() = %s, want %s", got, want)
	}
}

func TestSimulatedSet(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.June, 28, 0, 0, 0, 0, time.UTC)
	c := NewSimulated(start)
	target := start.Add(time.Hour)
	c.Set(target)
	if got := c.Now(); !got.Equal(target) {
		t.Fatalf("after Set, Now() = %s, want %s", got, target)
	}
}

func TestSimulatedAdvanceBackwardPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Advance(negative) did not panic")
		}
	}()
	NewSimulated(time.Unix(0, 0)).Advance(-time.Second)
}

func TestSimulatedSetBackwardPanics(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.June, 28, 12, 0, 0, 0, time.UTC)
	c := NewSimulated(start)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Set(earlier) did not panic")
		}
	}()
	c.Set(start.Add(-time.Nanosecond))
}

// TestSimulatedConcurrentAdvance proves one Simulated backs concurrent readers
// and writers safely; replay relies on a single shared clock driving many
// queue/lease components at once.
func TestSimulatedConcurrentAdvance(t *testing.T) {
	t.Parallel()
	c := NewSimulated(time.Unix(0, 0).UTC())
	const writers = 8
	const steps = 100
	var wg sync.WaitGroup
	wg.Add(writers * 2)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < steps; j++ {
				c.Advance(time.Millisecond)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < steps; j++ {
				_ = c.Now()
			}
		}()
	}
	wg.Wait()
	want := time.Unix(0, 0).UTC().Add(time.Duration(writers*steps) * time.Millisecond)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("after concurrent advances, Now() = %s, want %s", got, want)
	}
}
