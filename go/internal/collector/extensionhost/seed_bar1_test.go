// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"testing"
)

// TestSeedReadMeetsBar1PinsCorrectedReading pins the corrected Bar1 reading
// from the arbiter ruling on eshu-hq/eshu#10817 (recorded in
// docs/internal/arbiter/10817-canary-bar1-ruling-2026-11-23.md, decisions 1-3,
// tracked by eshu-hq/eshu#11386): the floor measures bucket_bytes over
// bucket-read time alone (open + gunzip + scan, nvd_seed_read_ms), the rate
// bar >= 8 MiB/s applies only where the touched bucket bytes total >= 1 MiB,
// and below that the bar is an absolute read duration <= 2 s.
//
// The regression it guards is the misdefinition the 2026-11-23 QA canary
// disproved: reusing the 4 MiB/s deadline-budget divisor (or the corrected 8
// MiB/s bar) as an observed end-to-end rate over satellite latency. A 67,303 B
// bucket behind flat ~4.2 s satellite overhead caps at ~0.015 MiB/s at
// infinite disk speed, so any rate bar applied to small buckets fails
// structurally. The small-bucket leg exists for exactly that reason: the
// canary pair below must PASS here while failing a rate-only bar.
func TestSeedReadMeetsBar1PinsCorrectedReading(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		fig  SeedFigures
		want bool
	}{
		// The QA canary shape: a small day bucket read within the absolute
		// leg. Rate is ~0.04 MiB/s, far below 8 MiB/s, yet the window passes
		// because the small-bucket leg measures duration, not rate.
		"canary small bucket within 2s passes": {
			fig:  SeedFigures{ReadMS: 1500, Buckets: 1, Bytes: 67303},
			want: true,
		},
		// Same bucket over the absolute leg fails: the leg is a bar, not a
		// blanket pass for small buckets.
		"canary small bucket over 2s fails": {
			fig:  SeedFigures{ReadMS: 2500, Buckets: 1, Bytes: 67303},
			want: false,
		},
		// Boundary of the absolute leg: exactly 2 s still passes.
		"small bucket at exactly 2s passes": {
			fig:  SeedFigures{ReadMS: 2000, Buckets: 1, Bytes: 67303},
			want: true,
		},
		// Just under 1 MiB stays on the duration leg even with a fast read.
		"just under 1MiB uses duration leg": {
			fig:  SeedFigures{ReadMS: 100, Buckets: 1, Bytes: (1 << 20) - 1},
			want: true,
		},
		// At >= 1 MiB the rate leg applies: 8 MiB in 500 ms is 16 MiB/s.
		"large bucket fast read passes": {
			fig:  SeedFigures{ReadMS: 500, Buckets: 7, Bytes: 8 << 20},
			want: true,
		},
		// At >= 1 MiB the rate leg applies: 8 MiB in 2 s is only 4 MiB/s.
		"large bucket slow read fails": {
			fig:  SeedFigures{ReadMS: 2000, Buckets: 7, Bytes: 8 << 20},
			want: false,
		},
		// Rate boundary: exactly 1 MiB in 125 ms is exactly 8 MiB/s.
		"large bucket at exactly 8MiBps passes": {
			fig:  SeedFigures{ReadMS: 125, Buckets: 1, Bytes: 1 << 20},
			want: true,
		},
		// Just under the rate boundary fails: 1 MiB in 126 ms is ~7.9 MiB/s.
		"large bucket just under 8MiBps fails": {
			fig:  SeedFigures{ReadMS: 126, Buckets: 1, Bytes: 1 << 20},
			want: false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := SeedReadMeetsBar1(tc.fig); got != tc.want {
				t.Fatalf("SeedReadMeetsBar1(%+v) = %v, want %v", tc.fig, got, tc.want)
			}
		})
	}
}

// TestSeedReadFitsFloorPinsDeadlineProof pins decision 3 of the same ruling:
// every canary window's actual read duration must fit
// bucket_bytes/4MiB/s + 75s, the floor it was admitted under. The 4 MiB/s
// figure is the deadline-budget divisor, never an observed-rate bar.
func TestSeedReadFitsFloorPinsDeadlineProof(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		fig  SeedFigures
		want bool
	}{
		// The canary's ~4.3 s end-to-end window fits: the 67,303 B bucket
		// earns ~16 ms of budget on top of the 75 s overhead allowance.
		"canary window fits floor": {
			fig:  SeedFigures{ReadMS: 4300, Buckets: 1, Bytes: 67303},
			want: true,
		},
		// A read past budget + overhead does not fit, however large.
		"read past budget plus overhead fails": {
			fig:  SeedFigures{ReadMS: 75001 + (8<<20)*1000/(4<<20), Buckets: 7, Bytes: 8 << 20},
			want: false,
		},
		// Boundary: exactly budget + overhead still fits.
		"read at exactly budget plus overhead fits": {
			fig:  SeedFigures{ReadMS: 75000 + (8<<20)*1000/(4<<20), Buckets: 7, Bytes: 8 << 20},
			want: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := SeedReadFitsFloor(tc.fig); got != tc.want {
				t.Fatalf("SeedReadFitsFloor(%+v) = %v, want %v", tc.fig, got, tc.want)
			}
		})
	}
}
