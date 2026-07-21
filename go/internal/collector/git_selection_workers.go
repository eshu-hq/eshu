// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cpubudget"
)

// snapshotWorkerCount returns the number of concurrent snapshot workers.
// Reads ESHU_SNAPSHOT_WORKERS from env; defaults to min(NumCPU, 8).
// With two-lane scheduling and the large-repo semaphore, extra workers
// safely process small repos while large repos hold semaphore slots.
// The two-phase streaming design keeps per-snapshot memory at O(single_file).
func snapshotWorkerCount(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_SNAPSHOT_WORKERS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	n := cpubudget.UsableCPUs()
	if n > 8 {
		n = 8
	}
	if n < 1 {
		n = 1
	}
	return n
}

// streamBufferSize returns the stream channel buffer size.
// Reads ESHU_STREAM_BUFFER from env; defaults to 0 (use worker count).
//
// Each buffered CollectedGeneration holds metadata and a fact channel
// reference — file bodies are re-read from disk on demand via two-phase
// streaming, so the per-slot memory cost is negligible.
func streamBufferSize(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_STREAM_BUFFER")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// parseWorkerCount returns the number of concurrent file parse workers.
// Reads ESHU_PARSE_WORKERS from env; defaults to min(NumCPU, 8).
func parseWorkerCount(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_PARSE_WORKERS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	n := cpubudget.UsableCPUs()
	if n > 8 {
		n = 8
	}
	if n < 1 {
		n = 1
	}
	return n
}

// largeRepoThreshold returns the file-count threshold above which a repository
// is classified as "large" for concurrency limiting.
// Reads ESHU_LARGE_REPO_FILE_THRESHOLD from env; defaults to 1000.
//
// Production data (895 repos, Apr 2026) shows 34 repos above 1000 files
// producing 66.8% of all facts. Repos in the 501–1000 range (40 repos)
// are busy but not memory-dangerous and benefit from full parallelism.
func largeRepoThreshold(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_LARGE_REPO_FILE_THRESHOLD")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 1000
}

// largeRepoMaxConcurrent returns the maximum number of large repositories that
// can be snapshotted concurrently.
// Reads ESHU_LARGE_REPO_MAX_CONCURRENT from env; defaults to 2.
//
// Tuning guide:
//
//	1 = safest for memory; only one large parse at a time
//	2 = good balance; two large repos + remaining workers on small repos
//	4 = aggressive; requires more RAM but faster on large-heavy workloads
func largeRepoMaxConcurrent(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_LARGE_REPO_MAX_CONCURRENT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 2
}
