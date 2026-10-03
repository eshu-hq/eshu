// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import (
	"context"
	"sync"
	"testing"
	"time"
)

var allReaderStages = []ReaderStage{
	ReaderStageBorrow, ReaderStageIdentity, ReaderStageReplay, ReaderStageBusinessQuery,
}

func TestStageTimingsConcurrentAddIsExact(t *testing.T) {
	ctx, timings := WithStageTimings(context.Background())
	if timings == nil || StageTimingsFrom(ctx) != timings {
		t.Fatalf("WithStageTimings did not attach the returned accumulator: %p vs %p", timings, StageTimingsFrom(ctx))
	}
	if timings.Recorded() {
		t.Fatal("fresh accumulator reports Recorded")
	}
	const goroutines, perGoroutine = 32, 500
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stage := allReaderStages[g%len(allReaderStages)]
			for range perGoroutine {
				StageTimingsFrom(ctx).Add(stage, 2*time.Millisecond)
			}
		}()
	}
	wg.Wait()
	wantCount := int64(goroutines / len(allReaderStages) * perGoroutine)
	wantSeconds := float64(wantCount) * 0.002
	for _, stage := range allReaderStages {
		if got := timings.Count(stage); got != wantCount {
			t.Errorf("Count(%d) = %d, want %d", stage, got, wantCount)
		}
		if got := timings.Seconds(stage); got < wantSeconds-1e-9 || got > wantSeconds+1e-9 {
			t.Errorf("Seconds(%d) = %v, want %v", stage, got, wantSeconds)
		}
	}
	if !timings.Recorded() {
		t.Fatal("Recorded() = false after Add")
	}
}

func TestStageTimingsAbsentAccumulatorIsSafe(t *testing.T) {
	if got := StageTimingsFrom(context.Background()); got != nil {
		t.Fatalf("StageTimingsFrom(plain ctx) = %v, want nil", got)
	}
	var nilTimings *StageTimings
	nilTimings.Add(ReaderStageBorrow, time.Second) // must not panic
	if nilTimings.Recorded() || nilTimings.Count(ReaderStageBorrow) != 0 || nilTimings.Seconds(ReaderStageBorrow) != 0 {
		t.Fatal("nil accumulator must read as empty")
	}
}

func TestStageTimingsRecordedAndBounds(t *testing.T) {
	_, timings := WithStageTimings(context.Background())
	timings.Add(ReaderStage(99), time.Second)    // outside the closed set
	timings.Add(ReaderStage(-1), time.Second)    // outside the closed set
	timings.Add(ReaderStageReplay, -time.Second) // negative clamps to zero but still counts
	if got := timings.Seconds(ReaderStageReplay); got != 0 {
		t.Fatalf("negative duration Seconds = %v, want 0", got)
	}
	if got := timings.Count(ReaderStageReplay); got != 1 {
		t.Fatalf("Count(replay) = %d, want 1", got)
	}
	if got := timings.Count(ReaderStage(99)); got != 0 {
		t.Fatalf("out-of-set Count = %d, want 0", got)
	}
	if !timings.Recorded() {
		t.Fatal("Recorded() = false after an in-set Add")
	}
	_, outOfSetOnly := WithStageTimings(context.Background())
	outOfSetOnly.Add(ReaderStage(99), time.Second)
	if outOfSetOnly.Recorded() {
		t.Fatal("an out-of-set stage must not mark the accumulator Recorded")
	}
}

func TestStageTimingsSumsRepeatedBorrow(t *testing.T) {
	_, timings := WithStageTimings(context.Background())
	timings.Add(ReaderStageBorrow, 10*time.Millisecond)
	timings.Add(ReaderStageBorrow, 30*time.Millisecond)
	if timings.Count(ReaderStageBorrow) != 2 {
		t.Fatalf("Count = %d, want 2", timings.Count(ReaderStageBorrow))
	}
	if got := timings.Seconds(ReaderStageBorrow); got < 0.0399999 || got > 0.0400001 {
		t.Fatalf("Seconds = %v, want the 0.040 sum", got)
	}
}
