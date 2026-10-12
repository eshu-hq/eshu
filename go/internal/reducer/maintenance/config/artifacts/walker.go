// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"hash/fnv"
	"time"
)

const (
	// DefaultWorkers is K, the walker's derive workers per process
	// (Proposed in the record).
	DefaultWorkers = 4
	// DefaultSkipAfterFailures is the failure count after which the walker
	// skips a repository until its next content Write or an extractor change.
	DefaultSkipAfterFailures = 5
	// DefaultPageBudget bounds the repositories one walk claim returns.
	DefaultPageBudget = 500
	// DefaultPollInterval is the pause between walk cycles.
	DefaultPollInterval = 5 * time.Minute
)

// Config holds the walker knobs. A zero or negative field takes its default.
// No environment variable reads these yet: the walker is not wired until
// PR3b-2, which registers its variables.
type Config struct {
	Workers           int
	PageBudget        int
	SkipAfterFailures int
	PollInterval      time.Duration
}

// WithDefaults returns c with every non-positive field set to its default.
func (c Config) WithDefaults() Config {
	if c.Workers <= 0 {
		c.Workers = DefaultWorkers
	}
	if c.PageBudget <= 0 {
		c.PageBudget = DefaultPageBudget
	}
	if c.SkipAfterFailures <= 0 {
		c.SkipAfterFailures = DefaultSkipAfterFailures
	}
	if c.PollInterval <= 0 {
		c.PollInterval = DefaultPollInterval
	}
	return c
}

// Claimer claims the next page of the shared repository walk. The Postgres
// implementation is storage/postgres/config/artifacts.ClaimPage; cmd/reducer
// adapts it, so this leaf does not import storage.
type Claimer interface {
	ClaimPage(ctx context.Context, budget int) (page []string, next string, err error)
}

// Deriver runs the derive protocol (protocol section 3) for one repository
// and returns its outcome label (ok, superseded, error, panic, timeout,
// encoding_unsupported). trigger is backfill or repair. PR3b-2 implements it;
// PR3b-1a defines the shape only, so no walker derives anything yet.
type Deriver interface {
	Derive(ctx context.Context, repoID string, trigger string) (outcome string, err error)
}

// Assign splits a claimed page over workers by a stable hash of repo_id, so
// one repository is never handed to two workers of one process, even when it
// appears twice in the input (for example from the dirty list and the walk).
// Each worker keeps input order. A non-positive workers count means one.
//
// This is the in-process half of "K workers partition the work by repo_id".
// Across processes the derive's repository advisory lock and advance-only
// write keep two derives of one repository correct.
func Assign(repoIDs []string, workers int) [][]string {
	if workers <= 0 {
		workers = 1
	}
	parts := make([][]string, workers)
	seen := make(map[string]struct{}, len(repoIDs))
	for _, repoID := range repoIDs {
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		worker := workerFor(repoID, workers)
		parts[worker] = append(parts[worker], repoID)
	}
	return parts
}

// workerFor maps a repository to a worker with FNV-1a, which is stable across
// processes and releases. The modulo runs in int, which holds every uint32 on
// the 64-bit platforms Eshu ships (linux/amd64 and linux/arm64 images), so the result equals the PR3b-1a form
// (Sum32 % uint32(workers)) for every worker count that fits in uint32, and
// stays a valid index above that, where the uint32 form truncated the count.
// Assign never passes fewer than one worker; a count below one maps to 0.
func workerFor(repoID string, workers int) int {
	if workers < 1 {
		return 0
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(repoID))
	return int(hash.Sum32()) % workers
}
