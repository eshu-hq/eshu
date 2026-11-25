// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

// Bar1 seed-read acceptance thresholds. They encode the corrected Bar1
// reading from the arbiter ruling on eshu-hq/eshu#10817 (recorded in
// docs/internal/arbiter/10817-canary-bar1-ruling-2026-11-23.md, tracked by
// eshu-hq/eshu#11386): the seed floor measures bucket_bytes over bucket-read
// time alone (open + gunzip + scan, nvd_seed_read_ms), never over satellite
// latency or core upsert.
const (
	// seedBar1RateBytesPerSec is the observed-rate bar: 8 MiB/s, applying
	// only where the touched bucket bytes total at least
	// seedBar1SmallBucketBytes.
	seedBar1RateBytesPerSec = 8 << 20
	// seedBar1SmallBucketBytes is the leg boundary: below 1 MiB of touched
	// bucket bytes the bar is an absolute read duration, not a rate.
	seedBar1SmallBucketBytes = 1 << 20
	// seedBar1SmallBucketMaxReadMS is the small-bucket leg: an absolute read
	// duration of at most 2 s.
	seedBar1SmallBucketMaxReadMS = 2000
	// seedFloorBudgetBytesPerSec is the deadline-budget divisor: 4 MiB/s.
	// It prices the floor the window was admitted under; it is never an
	// observed-rate bar.
	seedFloorBudgetBytesPerSec = 4 << 20
	// seedFloorOverheadMS is the fixed overhead allowance in the deadline
	// proof: 75 s.
	seedFloorOverheadMS = 75 * 1000
)

// SeedReadMeetsBar1 reports whether one served window's recorded seed pair
// (nvd_seed_read_ms, nvd_seed_bytes) meets Bar1 decision 1: at >= 1 MiB of
// touched bucket bytes the observed bucket-read rate must reach 8 MiB/s;
// below 1 MiB the read duration must fit within 2 s. The comparison is integer
// cross-multiplication, so exact boundaries pass without float error.
func SeedReadMeetsBar1(fig SeedFigures) bool {
	if fig.Bytes >= seedBar1SmallBucketBytes {
		return fig.Bytes*1000 >= int64(seedBar1RateBytesPerSec)*fig.ReadMS
	}
	return fig.ReadMS <= seedBar1SmallBucketMaxReadMS
}

// SeedReadFitsFloor reports whether one served window's recorded seed pair
// meets Bar1 decision 3, the deadline proof: the actual read duration must
// fit bucket_bytes/4MiB/s + 75s.
func SeedReadFitsFloor(fig SeedFigures) bool {
	return fig.ReadMS <= fig.Bytes*1000/int64(seedFloorBudgetBytesPerSec)+seedFloorOverheadMS
}
