// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"maps"
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

type timingWitness struct {
	warmup         [2]measuredRequest
	pagesByRows    map[string]string
	warmupConflict bool
}

func newTimingWitness(warmup [2]measuredRequest, rowHashes [2]string) *timingWitness {
	witness := &timingWitness{warmup: warmup, pagesByRows: make(map[string]string)}
	for route, rowHash := range rowHashes {
		if page, seen := witness.pagesByRows[rowHash]; seen && page != warmup[route].pageHash {
			witness.warmupConflict = true
		}
		witness.pagesByRows[rowHash] = warmup[route].pageHash
	}
	return witness
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

func selectTimingWorkload(name string) (dynamicWorkload, error) {
	if name != "canonical" {
		return dynamicWorkload{}, fmt.Errorf("unsupported timing workload %q", name)
	}
	for _, workload := range dynamicWorkloads() {
		if workload.name == name {
			return workload, nil
		}
	}
	return dynamicWorkload{}, fmt.Errorf("timing workload %q missing", name)
}

func hasCappedPool(rows []codetopicparallel.ProbeRow, cap int) bool {
	counts := make(map[poolKey]int)
	for _, row := range rows {
		key := poolKey{kind: row.SourceKind, term: row.MatchedTerm}
		counts[key]++
		if counts[key] == cap {
			return true
		}
	}
	return false
}

func validateMeasuredRound(block [4]measuredRequest, witness *timingWitness, workload dynamicWorkload, cap int) error {
	if witness == nil {
		return fmt.Errorf("timing witness is missing")
	}
	if witness.warmupConflict {
		return fmt.Errorf("warmup assembled different pages from the same probe rows")
	}
	var rowHashes [4]string
	pagesByRows := maps.Clone(witness.pagesByRows)
	for index, result := range block {
		route := 0
		if index == 1 || index == 2 {
			route = 1
		}
		if err := validateConditionalPools(witness.warmup[route].rows, result.rows, workload.terms, cap); err != nil {
			return fmt.Errorf("request %d changed %s pools from warmup: %w", index, []string{"baseline", "candidate"}[route], err)
		}
		if err := validateScope(result.rows, workload.allowedRepos, workload.language); err != nil {
			return fmt.Errorf("request %d scope: %w", index, err)
		}
		rowHash, err := hashRows(result.rows)
		if err != nil {
			return fmt.Errorf("request %d row fingerprint: %w", index, err)
		}
		rowHashes[index] = rowHash
		if page, seen := pagesByRows[rowHash]; seen && page != result.pageHash {
			return fmt.Errorf("request %d reused probe rows with a different page", index)
		}
		pagesByRows[rowHash] = result.pageHash
		if !hasCappedPool(witness.warmup[route].rows, cap) && witness.warmup[route].pageHash != result.pageHash {
			return fmt.Errorf("request %d changed uncapped %s page from warmup", index, []string{"baseline", "candidate"}[route])
		}
	}
	for _, pair := range [][2]int{{0, 3}, {1, 2}} {
		if rowHashes[pair[0]] == rowHashes[pair[1]] && block[pair[0]].pageHash != block[pair[1]].pageHash {
			return fmt.Errorf("same-route requests %d/%d assembled different pages from the same probe rows", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]int{{0, 1}, {3, 2}} {
		baseline, candidate := block[pair[0]], block[pair[1]]
		if err := validateConditionalPools(baseline.rows, candidate.rows, workload.terms, cap); err != nil {
			return fmt.Errorf("round pair %d/%d: %w", pair[0], pair[1], err)
		}
		if rowHashes[pair[0]] == rowHashes[pair[1]] && baseline.pageHash != candidate.pageHash {
			return fmt.Errorf("round pair %d/%d assembled different pages from the same probe rows", pair[0], pair[1])
		}
		if !hasCappedPool(baseline.rows, cap) && baseline.pageHash != candidate.pageHash {
			return fmt.Errorf("uncapped round pair %d/%d changed assembled page", pair[0], pair[1])
		}
	}
	witness.pagesByRows = pagesByRows
	return nil
}
