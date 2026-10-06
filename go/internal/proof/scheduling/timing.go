// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
	"github.com/jackc/pgx/v5"
)

type measuredRequest struct {
	rows     []codetopicparallel.ProbeRow
	pageHash string
	duration time.Duration
}

func timedProbeFingerprint(rows []codetopicparallel.ProbeRow, validatedHash string) (bool, error) {
	currentHash, err := hashRows(rows)
	if err != nil {
		return false, err
	}
	return currentHash == validatedHash, nil
}

func validateTimedRows(ctx context.Context, tx pgx.Tx, rows []codetopicparallel.ProbeRow, workload dynamicWorkload, validatedHash string) error {
	same, err := timedProbeFingerprint(rows, validatedHash)
	if err != nil {
		return err
	}
	if same {
		return nil
	}
	return verifyPersistedEligibility(ctx, tx, rows, workload, candidateCap)
}

func timingOrder(rounds int) []bool {
	if rounds <= 0 {
		return nil
	}
	order := make([]bool, 0, rounds*4)
	for range rounds {
		order = append(order, false, true, true, false)
	}
	return order
}

func medianDuration(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	ordered := slices.Clone(samples)
	slices.Sort(ordered)
	mid := len(ordered) / 2
	if len(ordered)%2 == 1 {
		return ordered[mid]
	}
	return (ordered[mid-1] + ordered[mid]) / 2
}

func blockRegression(baseline, candidate time.Duration) bool {
	return baseline > 0 && candidate > baseline+baseline/10
}

func selectTimingWorkload(name, repoID string) (dynamicWorkload, error) {
	if name != "canonical" {
		return dynamicWorkload{}, fmt.Errorf("unsupported timing workload %q", name)
	}
	for _, workload := range dynamicWorkloads(repoID) {
		if workload.name == name {
			return workload, nil
		}
	}
	return dynamicWorkload{}, fmt.Errorf("timing workload %q missing", name)
}

func validateMeasuredRound(block [4]measuredRequest, workload dynamicWorkload, cap int) error {
	if block[0].pageHash != block[3].pageHash || block[1].pageHash != block[2].pageHash {
		return fmt.Errorf("same-route page changed within timing round")
	}
	for _, pair := range [][2]int{{0, 1}, {3, 2}} {
		baseline, candidate := block[pair[0]], block[pair[1]]
		if err := validateConditionalPools(baseline.rows, candidate.rows, workload.terms, cap); err != nil {
			return fmt.Errorf("round pair %d/%d: %w", pair[0], pair[1], err)
		}
		if err := validateScope(candidate.rows, workload.allowedRepos, workload.language); err != nil {
			return fmt.Errorf("round candidate %d: %w", pair[1], err)
		}
		allowed := make(map[string]struct{}, len(workload.terms))
		for _, term := range workload.terms {
			allowed[term] = struct{}{}
		}
		pools, err := indexPools(baseline.rows, allowed, cap)
		if err != nil {
			return err
		}
		uncapped := true
		for _, rows := range pools {
			if len(rows) == cap {
				uncapped = false
				break
			}
		}
		if uncapped && baseline.pageHash != candidate.pageHash {
			return fmt.Errorf("uncapped round pair %d/%d changed assembled page", pair[0], pair[1])
		}
	}
	return nil
}
