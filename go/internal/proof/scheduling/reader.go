// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

type (
	readerRequest    func(context.Context, bool) (measuredRequest, error)
	readerCheckpoint func(context.Context, int) error
	readerValidation func(context.Context, int, bool, measuredRequest) error
)

func runReaderSchedule(ctx context.Context, request readerRequest, checkpoint readerCheckpoint, validate readerValidation) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("reader schedule context: %w", err)
	}
	if err := checkpoint(ctx, 0); err != nil {
		return fmt.Errorf("reader checkpoint 0: %w", err)
	}
	baselineSamples := make([]time.Duration, 0, 6)
	candidateSamples := make([]time.Duration, 0, 6)
	var block [4]measuredRequest
	for index, candidate := range timingOrder(3) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("reader request context: %w", err)
		}
		result, err := request(ctx, candidate)
		if err != nil {
			return fmt.Errorf("reader request %d candidate=%t: %w", index, candidate, err)
		}
		if err := validate(ctx, index, candidate, result); err != nil {
			return fmt.Errorf("reader request %d correctness: %w", index, err)
		}
		if err := checkpoint(ctx, index+1); err != nil {
			return fmt.Errorf("reader checkpoint %d: %w", index+1, err)
		}
		block[index%4] = result
		if index%4 != 3 {
			continue
		}
		round := index / 4
		baselineSamples = append(baselineSamples, block[0].duration, block[3].duration)
		candidateSamples = append(candidateSamples, block[1].duration, block[2].duration)
		fmt.Printf("dynamic_timing_round=%d baseline_ms=%.3f,%.3f candidate_ms=%.3f,%.3f pages_equal=%t,%t\n",
			round, float64(block[0].duration.Microseconds())/1000, float64(block[3].duration.Microseconds())/1000,
			float64(block[1].duration.Microseconds())/1000, float64(block[2].duration.Microseconds())/1000,
			block[0].pageHash == block[1].pageHash, block[3].pageHash == block[2].pageHash)
		if blockRegression(medianDuration([]time.Duration{block[0].duration, block[3].duration}),
			medianDuration([]time.Duration{block[1].duration, block[2].duration})) {
			return fmt.Errorf("timing round %d candidate exceeded 10%% regression stop threshold", round)
		}
	}
	fmt.Printf("dynamic_timing_baseline_median_ms=%.3f candidate_median_ms=%.3f samples_per_route=%d\n",
		float64(medianDuration(baselineSamples).Microseconds())/1000,
		float64(medianDuration(candidateSamples).Microseconds())/1000, len(baselineSamples))
	return nil
}

type readerWitness struct {
	workload  dynamicWorkload
	tx        pgx.Tx
	seed      [2]measuredRequest
	seedHash  [2]string
	seen      [2]bool
	pageByRow map[string]string
	block     [4]measuredRequest
	timing    *timingWitness
}

func newReaderWitness(tx pgx.Tx, workload dynamicWorkload) *readerWitness {
	return &readerWitness{tx: tx, workload: workload, pageByRow: make(map[string]string)}
}

func (w *readerWitness) validate(ctx context.Context, index int, candidate bool, result measuredRequest) error {
	route := 0
	if candidate {
		route = 1
	}
	if err := validateScope(result.rows, w.workload.allowedRepos, w.workload.language); err != nil {
		return err
	}
	if !w.seen[route] {
		if err := verifyPersistedEligibility(ctx, w.tx, result.rows, w.workload, candidateCap); err != nil {
			return fmt.Errorf("first %s persisted eligibility: %w", []string{"baseline", "candidate"}[route], err)
		}
		w.seed[route] = result
		w.seen[route] = true
	} else {
		if err := validateConditionalPools(w.seed[route].rows, result.rows, w.workload.terms, candidateCap); err != nil {
			return err
		}
		if err := validateTimedRows(ctx, w.tx, result.rows, w.workload, w.seedHash[route]); err != nil {
			return err
		}
		if !hasCappedPool(w.seed[route].rows, candidateCap) && result.pageHash != w.seed[route].pageHash {
			return fmt.Errorf("uncapped %s page changed", []string{"baseline", "candidate"}[route])
		}
	}
	rowHash, err := hashRows(result.rows)
	if err != nil {
		return err
	}
	if page, seen := w.pageByRow[rowHash]; seen && page != result.pageHash {
		return fmt.Errorf("same probe rows produced different assembled pages")
	}
	w.pageByRow[rowHash] = result.pageHash
	w.block[index%4] = result
	if index == 0 {
		w.seedHash[0] = rowHash
		return nil
	}
	if index == 1 {
		w.seedHash[1] = rowHash
		if err := validateConditionalPools(w.seed[0].rows, w.seed[1].rows, w.workload.terms, candidateCap); err != nil {
			return err
		}
		leftOnly, rightOnly, err := exactDifference(ctx, w.tx, w.seed[0].rows, w.seed[1].rows)
		if err != nil {
			return err
		}
		if leftOnly == 0 && rightOnly == 0 && w.seed[0].pageHash != w.seed[1].pageHash {
			return fmt.Errorf("same probe rows produced different assembled pages")
		}
		if err := writeReaderWitnessPools(os.Stdout, w.seed[0].rows, w.seed[1].rows, w.workload.terms, candidateCap, w.seen[0] && w.seen[1]); err != nil {
			return fmt.Errorf("reader witness pool output: %w", err)
		}
		fmt.Printf("dynamic_case=%s baseline_rows=%d candidate_rows=%d left_only=%d right_only=%d page_equal=%t\n",
			w.workload.name, len(w.seed[0].rows), len(w.seed[1].rows), leftOnly, rightOnly,
			w.seed[0].pageHash == w.seed[1].pageHash)
		fmt.Printf("dynamic_case=%s conditional_pools=pass persisted_eligibility=pass timing=pending\n", w.workload.name)
		w.timing = newTimingWitness(w.seed, w.seedHash)
		return nil
	}
	if index%4 == 3 {
		if err := validateMeasuredRound(w.block, w.timing, w.workload, candidateCap); err != nil {
			return err
		}
	}
	return nil
}

func runDynamicReaderCase(ctx context.Context, connections []*pgx.Conn, workload dynamicWorkload, limits readerLagLimits, resource readerResourceConfig) (resultErr error) {
	if len(connections) != 4 {
		return fmt.Errorf("reader proof requires exactly four connections")
	}
	txs := make([]pgx.Tx, 0, 4)
	defer func() { resultErr = errors.Join(resultErr, rollbackDynamicTransactions(txs)) }()
	for _, conn := range connections {
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return fmt.Errorf("begin reader proof transaction: %w", err)
		}
		txs = append(txs, tx)
	}
	if err := shareDynamicSnapshot(ctx, txs); err != nil {
		return err
	}
	witness := newReaderWitness(txs[0], workload)
	request := func(requestCtx context.Context, candidate bool) (measuredRequest, error) {
		return runDynamicRequest(requestCtx, txs, workload, candidate)
	}
	checkpoint := func(checkCtx context.Context, barrier int) error {
		if err := checkReaderHealth(checkCtx, txs[0], barrier, limits); err != nil {
			return err
		}
		if err := checkReaderResourceFile(checkCtx, resource); err != nil {
			return err
		}
		fmt.Printf("reader_resource_barrier=%d gate=pass\n", barrier)
		return nil
	}
	if err := runReaderSchedule(ctx, request, checkpoint, witness.validate); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "dynamic_case=%s timing=completed\n", workload.name); err != nil {
		return fmt.Errorf("write reader timing completion: %w", err)
	}
	return nil
}
