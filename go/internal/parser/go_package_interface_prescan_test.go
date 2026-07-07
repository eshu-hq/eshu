// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"runtime"
	"testing"
)

// TestEffectivePackagePrescanWorkersDefaultTracksUsableCPUs pins the
// non-positive-configured default to the cgroup-aware usable CPU count
// (runtime.GOMAXPROCS(0), what cpubudget.UsableCPUs returns), capped at
// defaultPackagePrescanWorkerCap. Before #4759 this default read
// runtime.NumCPU(), which always reports the HOST cpu count and would
// over-subscribe worker pools inside a cgroup CPU-quota container (K8s
// resources.limits.cpu, Docker --cpus). The oracle uses GOMAXPROCS(0), not
// NumCPU(): in a cgroup-limited container the production path now sizes off
// the reduced GOMAXPROCS while NumCPU stays host-wide.
func TestEffectivePackagePrescanWorkersDefaultTracksUsableCPUs(t *testing.T) {
	t.Parallel()

	got := effectivePackagePrescanWorkers(0)

	want := runtime.GOMAXPROCS(0)
	if want < 1 {
		want = 1
	}
	if want > defaultPackagePrescanWorkerCap {
		want = defaultPackagePrescanWorkerCap
	}
	if got != want {
		t.Fatalf("effectivePackagePrescanWorkers(0) = %d, want %d (GOMAXPROCS=%d, cap=%d)",
			got, want, runtime.GOMAXPROCS(0), defaultPackagePrescanWorkerCap)
	}
}

// TestEffectivePackagePrescanWorkersClampTracksUsableCPUs pins the
// positive-configured-value clamp (UsableCPUs*2) to the same cgroup-aware
// oracle so a stale operator override cannot over-subscribe the container.
func TestEffectivePackagePrescanWorkersClampTracksUsableCPUs(t *testing.T) {
	t.Parallel()

	cpu := runtime.GOMAXPROCS(0)
	if cpu < 1 {
		cpu = 1
	}
	configured := cpu*2 + 100

	got := effectivePackagePrescanWorkers(configured)
	want := cpu * 2
	if got != want {
		t.Fatalf("effectivePackagePrescanWorkers(%d) = %d, want %d (GOMAXPROCS=%d)",
			configured, got, want, cpu)
	}
}

func TestPackagePrescanPassWorkerCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		workers   int
		fileCount int
		want      int
	}{
		{
			name:      "empty file set disables workers",
			workers:   4,
			fileCount: 0,
			want:      0,
		},
		{
			name:      "nonpositive worker request defaults to one",
			workers:   0,
			fileCount: 3,
			want:      1,
		},
		{
			name:      "negative worker request defaults to one",
			workers:   -2,
			fileCount: 3,
			want:      1,
		},
		{
			name:      "requested workers below file count are preserved",
			workers:   2,
			fileCount: 5,
			want:      2,
		},
		{
			name:      "requested workers are capped at file count",
			workers:   8,
			fileCount: 3,
			want:      3,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := packagePrescanPassWorkerCount(test.workers, test.fileCount)
			if got != test.want {
				t.Fatalf("packagePrescanPassWorkerCount(%d, %d) = %d, want %d", test.workers, test.fileCount, got, test.want)
			}
		})
	}
}
