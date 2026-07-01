// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func bootstrapStatementPhaseUsesEntityWorkers(phase string) bool {
	return phase == sourcecypher.CanonicalPhaseEntities ||
		phase == sourcecypher.CanonicalPhaseEntityContainment
}

func (e bootstrapNornicDBPhaseGroupExecutor) executeEntityPhaseGroupConcurrently(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
) error {
	grouped := make([]sourcecypher.Statement, 0, len(stmts))
	groupedLabel := ""
	flush := func() error {
		if len(grouped) == 0 {
			return nil
		}
		err := e.executeGroupedChunksConcurrently(
			ctx,
			ge,
			grouped,
			groupedLabel,
			e.phaseGroupStatementLimit(grouped),
		)
		grouped = grouped[:0]
		groupedLabel = ""
		return err
	}

	for _, stmt := range stmts {
		if bootstrapStatementPhaseGroupMode(stmt) == sourcecypher.PhaseGroupModeExecuteOnly {
			if err := flush(); err != nil {
				return err
			}
			startedAt := time.Now()
			if err := e.inner.Execute(ctx, bootstrapSanitizedStatement(stmt)); err != nil {
				return fmt.Errorf(
					"phase-group singleton statement (phase=%s, duration=%s, first_statement=%q): %w",
					bootstrapStatementPhase([]sourcecypher.Statement{stmt}),
					time.Since(startedAt),
					bootstrapOperatorStatementSummary(stmt),
					err,
				)
			}
			continue
		}
		label := bootstrapEntityStatementLabel(stmt)
		if len(grouped) > 0 && groupedLabel != label {
			if err := flush(); err != nil {
				return err
			}
		}
		grouped = append(grouped, stmt)
		if groupedLabel == "" {
			groupedLabel = label
		}
	}
	return flush()
}

func (e bootstrapNornicDBPhaseGroupExecutor) executeGroupedChunksConcurrently(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
	label string,
	maxStatements int,
) error {
	if len(stmts) == 0 {
		return nil
	}
	totalChunks := (len(stmts) + maxStatements - 1) / maxStatements
	if totalChunks <= 1 || e.entityPhaseConcurrency <= 1 {
		return e.executeGroupedChunks(ctx, ge, stmts, maxStatements)
	}

	chunks := make([][]sourcecypher.Statement, 0, totalChunks)
	for start := 0; start < len(stmts); start += maxStatements {
		end := start + maxStatements
		if end > len(stmts) {
			end = len(stmts)
		}
		chunks = append(chunks, append([]sourcecypher.Statement(nil), stmts[start:end]...))
	}

	workers := e.entityPhaseConcurrency
	if workers > len(chunks) {
		workers = len(chunks)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	var wg sync.WaitGroup
	var firstErr error
	var firstErrMu sync.Mutex
	recordErr := func(err error) {
		firstErrMu.Lock()
		defer firstErrMu.Unlock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
	}

	phase := bootstrapStatementPhase(stmts)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				chunk := chunks[index]
				startedAt := time.Now()
				err := ge.ExecuteGroup(runCtx, bootstrapSanitizedPhaseGroupChunk(chunk))
				duration := time.Since(startedAt)
				if err != nil {
					recordErr(fmt.Errorf(
						"phase-group chunk %d/%d (phase=%s, label=%s, size=%d, duration=%s, first_statement=%q): %w",
						index+1,
						len(chunks),
						phase,
						label,
						len(chunk),
						duration,
						bootstrapOperatorStatementSummary(chunk[0]),
						err,
					))
					continue
				}
				slog.Info(
					"bootstrap nornicdb phase-group chunk completed",
					"phase", phase,
					"label", label,
					"chunk_index", index+1,
					"chunk_count", len(chunks),
					"statement_count", len(chunk),
					"duration_s", duration.Seconds(),
					"concurrency", e.entityPhaseConcurrency,
					"first_statement", bootstrapOperatorStatementSummary(chunk[0]),
				)
			}
		}()
	}

	for index := range chunks {
		select {
		case jobs <- index:
		case <-runCtx.Done():
			close(jobs)
			wg.Wait()
			firstErrMu.Lock()
			err := firstErr
			firstErrMu.Unlock()
			if err != nil {
				return err
			}
			return runCtx.Err()
		}
	}
	close(jobs)
	wg.Wait()

	firstErrMu.Lock()
	defer firstErrMu.Unlock()
	return firstErr
}

func bootstrapOperatorStatementSummary(stmt sourcecypher.Statement) string {
	phase, _ := stmt.Parameters[sourcecypher.StatementMetadataPhaseKey].(string)
	label, _ := stmt.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
	fields := make([]string, 0, 2)
	if phase = strings.TrimSpace(phase); phase != "" {
		fields = append(fields, "phase="+phase)
	}
	if label = strings.TrimSpace(label); label != "" {
		fields = append(fields, "label="+label)
	}
	if len(fields) == 0 {
		return "statement"
	}
	return strings.Join(fields, " ")
}
