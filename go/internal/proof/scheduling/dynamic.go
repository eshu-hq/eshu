// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
	"github.com/jackc/pgx/v5"
)

type dynamicWorkload struct {
	name         string
	terms        []string
	filters      []string
	baseArgs     []any
	allowedRepos []string
	language     string
}

func annotatedScreenError(phase, subject string, started time.Time, err error) error {
	return fmt.Errorf("%s %q elapsed=%s: %w", phase, subject, time.Since(started).Round(time.Millisecond), err)
}

func normalizedTerms(raw []string) []string {
	return codequery.CodeTopicSearchTerms("", "", raw)
}

func dynamicWorkloads(repoID string) []dynamicWorkload {
	commonRare := normalizedTerms([]string{
		"api", "auth", "build", "cache", "client", "config", "database", "error",
		"graph", "handler", "query", "repository", "service", "token", "worker", "zebra",
	})
	punctuation := normalizedTerms([]string{
		"a%c", "a_c", `back\slash`, "foo.bar", "foo-bar", "package.json",
		"request_id", "user%", "api/v1", "sha-256", "abc_def", "x%y",
		"x_y", `path\`, "go.mod", "a+b",
	})
	emptyTerms := make([]string, 16)
	for i := range emptyTerms {
		emptyTerms[i] = fmt.Sprintf("zz7033never%02d", i)
	}
	return []dynamicWorkload{
		{name: "canonical", terms: terms, filters: []string{"eshu_require_content_substring_indexes_ready()"}},
		{name: "common_rare", terms: commonRare, filters: []string{"eshu_require_content_substring_indexes_ready()"}},
		{name: "punctuation", terms: punctuation, filters: []string{"eshu_require_content_substring_indexes_ready()"}},
		{name: "explicit_repo", terms: terms, filters: []string{"repo_id = $1"}, baseArgs: []any{repoID}, allowedRepos: []string{repoID}},
		{name: "grant", terms: commonRare, filters: []string{"eshu_require_content_substring_indexes_ready()", "repo_id = ANY($1)"}, baseArgs: []any{[]string{repoID}}, allowedRepos: []string{repoID}},
		{name: "language", terms: terms, filters: []string{"eshu_require_content_substring_indexes_ready()", "coalesce(language, '') = $1"}, baseArgs: []any{"go"}, language: "go"},
		{name: "empty", terms: emptyTerms, filters: []string{"eshu_require_content_substring_indexes_ready()"}},
	}
}

func selectDiagnosticWorkload(name, repoID string) (dynamicWorkload, error) {
	if name != "punctuation" {
		return dynamicWorkload{}, fmt.Errorf("unsupported diagnostic workload %q", name)
	}
	for _, workload := range dynamicWorkloads(repoID) {
		if workload.name == name {
			return workload, nil
		}
	}
	return dynamicWorkload{}, fmt.Errorf("diagnostic workload %q missing", name)
}

func readDynamicTerms(ctx context.Context, txs []pgx.Tx, workload dynamicWorkload) ([]codetopicparallel.ProbeRow, error) {
	queue := make(chan string, len(workload.terms))
	for _, term := range workload.terms {
		queue <- term
	}
	close(queue)
	parts, err := codetopicparallel.RunPartitions(ctx, 4, func(workerCtx context.Context, index int) ([]codetopicparallel.ProbeRow, error) {
		rows := make([]codetopicparallel.ProbeRow, 0)
		for term := range queue {
			if err := workerCtx.Err(); err != nil {
				return nil, fmt.Errorf("dynamic worker canceled before term %q: %w", term, err)
			}
			probeStarted := time.Now()
			part, _, queryErr := runGroupFiltered(workerCtx, txs[index], []string{term}, workload.filters, workload.baseArgs)
			if queryErr != nil {
				return nil, annotatedScreenError("candidate_term", term, probeStarted, queryErr)
			}
			rows = append(rows, part...)
		}
		return rows, nil
	})
	if err != nil {
		return nil, fmt.Errorf("run dynamic term partitions: %w", err)
	}
	rows := make([]codetopicparallel.ProbeRow, 0, 8000)
	for _, part := range parts {
		rows = append(rows, part...)
	}
	return rows, nil
}

func readBaselineTerms(ctx context.Context, txs []pgx.Tx, workload dynamicWorkload) ([]codetopicparallel.ProbeRow, error) {
	groups := partitionTerms(workload.terms, 4)
	parts, err := codetopicparallel.RunPartitions(ctx, 4, func(workerCtx context.Context, index int) ([]codetopicparallel.ProbeRow, error) {
		probeStarted := time.Now()
		rows, _, queryErr := runGroupFiltered(workerCtx, txs[index], groups[index], workload.filters, workload.baseArgs)
		if queryErr != nil {
			return nil, annotatedScreenError("baseline_group", strings.Join(groups[index], ","), probeStarted, queryErr)
		}
		return rows, nil
	})
	if err != nil {
		return nil, fmt.Errorf("run baseline term partitions: %w", err)
	}
	rows := make([]codetopicparallel.ProbeRow, 0, 8000)
	for _, part := range parts {
		rows = append(rows, part...)
	}
	return rows, nil
}

func runDynamicRequest(ctx context.Context, txs []pgx.Tx, workload dynamicWorkload, candidate bool) (measuredRequest, error) {
	started := time.Now()
	var rows []codetopicparallel.ProbeRow
	var err error
	if candidate {
		rows, err = readDynamicTerms(ctx, txs, workload)
	} else {
		rows, err = readBaselineTerms(ctx, txs, workload)
	}
	if err != nil {
		return measuredRequest{}, err
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return measuredRequest{}, fmt.Errorf("encode timed probe rows: %w", err)
	}
	assembled, err := queryAssembledRows(ctx, txs[0].Query, payload)
	if err != nil {
		return measuredRequest{}, fmt.Errorf("assemble timed page: %w", err)
	}
	measuredDuration := time.Since(started)
	page, err := summarizeDiagnosticPage(assembled)
	if err != nil {
		return measuredRequest{}, fmt.Errorf("fingerprint timed page: %w", err)
	}
	return measuredRequest{rows: rows, pageHash: page.fullHash, duration: measuredDuration}, nil
}

func runTimingRounds(ctx context.Context, txs []pgx.Tx, workload dynamicWorkload, warmup [2]measuredRequest, warmupHashes [2]string) error {
	baselineSamples := make([]time.Duration, 0, 6)
	candidateSamples := make([]time.Duration, 0, 6)
	witness := newTimingWitness(warmup, warmupHashes)
	for round := range 3 {
		var block [4]measuredRequest
		for index, candidate := range timingOrder(1) {
			result, err := runDynamicRequest(ctx, txs, workload, candidate)
			if err != nil {
				return fmt.Errorf("timing round %d request %d candidate=%t: %w", round, index, candidate, err)
			}
			block[index] = result
		}
		if err := validateMeasuredRound(block, witness, workload, candidateCap); err != nil {
			return fmt.Errorf("timing round %d invalid: %w", round, err)
		}
		for index, result := range block {
			validatedHash := warmupHashes[0]
			if index == 1 || index == 2 {
				validatedHash = warmupHashes[1]
			}
			if err := validateTimedRows(ctx, txs[0], result.rows, workload, validatedHash); err != nil {
				return fmt.Errorf("timing round %d request %d persisted eligibility: %w", round, index, err)
			}
		}
		baselineSamples = append(baselineSamples, block[0].duration, block[3].duration)
		candidateSamples = append(candidateSamples, block[1].duration, block[2].duration)
		baselineBlock := medianDuration([]time.Duration{block[0].duration, block[3].duration})
		candidateBlock := medianDuration([]time.Duration{block[1].duration, block[2].duration})
		fmt.Printf("dynamic_timing_round=%d baseline_ms=%.3f,%.3f candidate_ms=%.3f,%.3f pages_equal=%t,%t\n",
			round, float64(block[0].duration.Microseconds())/1000, float64(block[3].duration.Microseconds())/1000,
			float64(block[1].duration.Microseconds())/1000, float64(block[2].duration.Microseconds())/1000,
			block[0].pageHash == block[1].pageHash, block[3].pageHash == block[2].pageHash)
		if blockRegression(baselineBlock, candidateBlock) {
			return fmt.Errorf("timing round %d candidate exceeded 10%% regression stop threshold", round)
		}
	}
	fmt.Printf("dynamic_timing_baseline_median_ms=%.3f candidate_median_ms=%.3f samples_per_route=%d\n",
		float64(medianDuration(baselineSamples).Microseconds())/1000,
		float64(medianDuration(candidateSamples).Microseconds())/1000,
		len(baselineSamples))
	return nil
}

func rollbackDynamicTransactions[T interface{ Rollback(context.Context) error }](txs []T) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var cleanupErr error
	for index, tx := range txs {
		if err := tx.Rollback(cleanupCtx); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("rollback dynamic transaction %d: %w", index, err))
		}
	}
	return cleanupErr
}

func shareDynamicSnapshot(ctx context.Context, txs []pgx.Tx) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("share dynamic snapshot context: %w", err)
	}
	if len(txs) != 4 {
		return fmt.Errorf("dynamic proof requires exactly four transactions")
	}
	var snapshotID string
	if err := txs[0].QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&snapshotID); err != nil {
		return fmt.Errorf("export dynamic snapshot: %w", err)
	}
	if !validSnapshotID(snapshotID) {
		return fmt.Errorf("invalid exported snapshot identifier")
	}
	for _, tx := range txs[1:] {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("import dynamic snapshot context: %w", err)
		}
		if _, err := tx.Exec(ctx, "SET TRANSACTION SNAPSHOT '"+snapshotID+"'"); err != nil {
			return fmt.Errorf("import dynamic snapshot: %w", err)
		}
	}
	for _, tx := range txs {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("bound dynamic transaction context: %w", err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '5s'"); err != nil {
			return fmt.Errorf("bound dynamic statement: %w", err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '1s'"); err != nil {
			return fmt.Errorf("bound dynamic lock wait: %w", err)
		}
	}
	return nil
}

func runDynamicCase(ctx context.Context, connections []*pgx.Conn, workload dynamicWorkload, timing bool) (resultErr error) {
	if len(connections) != 4 {
		return fmt.Errorf("dynamic proof requires exactly four connections")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("run dynamic case context: %w", err)
	}
	started := time.Now()
	txs := make([]pgx.Tx, 0, 4)
	defer func() {
		resultErr = errors.Join(resultErr, rollbackDynamicTransactions(txs))
	}()
	for _, conn := range connections {
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return fmt.Errorf("begin dynamic reader transaction: %w", err)
		}
		txs = append(txs, tx)
	}
	if err := shareDynamicSnapshot(ctx, txs); err != nil {
		return err
	}
	baselineStarted := time.Now()
	baselineRows, err := readBaselineTerms(ctx, txs, workload)
	if err != nil {
		return annotatedScreenError("baseline", workload.name, baselineStarted, err)
	}
	candidateStarted := time.Now()
	candidateRows, err := readDynamicTerms(ctx, txs, workload)
	if err != nil {
		return annotatedScreenError("candidate", workload.name, candidateStarted, err)
	}
	differenceStarted := time.Now()
	leftOnly, rightOnly, err := exactDifference(ctx, txs[0], baselineRows, candidateRows)
	if err != nil {
		return annotatedScreenError("exact_difference", workload.name, differenceStarted, err)
	}
	assemblyStarted := time.Now()
	baselinePage, err := assemblyHash(ctx, txs[0], baselineRows)
	if err != nil {
		return annotatedScreenError("baseline_assembly", workload.name, assemblyStarted, err)
	}
	assemblyStarted = time.Now()
	candidatePage, err := assemblyHash(ctx, txs[0], candidateRows)
	if err != nil {
		return annotatedScreenError("candidate_assembly", workload.name, assemblyStarted, err)
	}
	fmt.Printf("dynamic_case=%s baseline_rows=%d candidate_rows=%d left_only=%d right_only=%d page_equal=%t\n",
		workload.name, len(baselineRows), len(candidateRows), leftOnly, rightOnly, baselinePage == candidatePage)
	if err := validateConditionalPools(baselineRows, candidateRows, workload.terms, candidateCap); err != nil {
		return fmt.Errorf("conditional pool contract: %w", err)
	}
	if err := validateScope(candidateRows, workload.allowedRepos, workload.language); err != nil {
		return fmt.Errorf("candidate scope: %w", err)
	}
	for _, route := range []struct {
		name string
		rows []codetopicparallel.ProbeRow
	}{{"baseline", baselineRows}, {"candidate", candidateRows}} {
		if err := verifyPersistedEligibility(ctx, txs[0], route.rows, workload, candidateCap); err != nil {
			return fmt.Errorf("%s persisted eligibility: %w", route.name, err)
		}
	}
	if leftOnly == 0 && rightOnly == 0 && baselinePage != candidatePage {
		return fmt.Errorf("same probe rows produced different assembled pages")
	}
	baselineHash, err := hashRows(baselineRows)
	if err != nil {
		return err
	}
	candidateHash, err := hashRows(candidateRows)
	if err != nil {
		return err
	}
	timingStatus := "not_run"
	if timing {
		timingStatus = "pending"
	}
	fmt.Printf("dynamic_case=%s conditional_pools=pass persisted_eligibility=pass timing=%s\n", workload.name, timingStatus)
	fmt.Printf("dynamic_case=%s snapshot_age_ms=%d\n", workload.name, time.Since(started).Milliseconds())
	if timing {
		warmup := [2]measuredRequest{{rows: baselineRows, pageHash: baselinePage}, {rows: candidateRows, pageHash: candidatePage}}
		if err := runTimingRounds(ctx, txs, workload, warmup, [2]string{baselineHash, candidateHash}); err != nil {
			return err
		}
		fmt.Printf("dynamic_case=%s timing=completed snapshot_age_ms=%d\n", workload.name, time.Since(started).Milliseconds())
	}
	return nil
}
