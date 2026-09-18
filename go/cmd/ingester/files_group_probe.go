// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const fileGroupTimingEnv = "ESHU_NORNICDB_PROFILE_FILE_GROUPS"

var fileGroupCallSequence atomic.Uint64

type fileResult = sourcecypher.StatementRetractionCounts

type fileResultConsumer func(context.Context) (fileResult, error)

type fileStatementRunner func(context.Context, sourcecypher.Statement) (fileResultConsumer, error)

type fileGroupProbe struct {
	id                  uint64
	logger              *slog.Logger
	started             time.Time
	lastCallbackEnded   time.Time
	lastCallbackSuccess bool
	attempt             int
}

func fileGroupTimingEnabled(getenv func(string) string) (bool, error) {
	raw := strings.TrimSpace(getenv(fileGroupTimingEnv))
	if raw == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s=%q: %w", fileGroupTimingEnv, raw, err)
	}
	return enabled, nil
}

func newFileGroupProbe(logger *slog.Logger, stmts []sourcecypher.Statement) *fileGroupProbe {
	if len(stmts) == 0 {
		return nil
	}
	for _, stmt := range stmts {
		if _, _, ok := sourcecypher.CanonicalFileStatementProfile(stmt); !ok {
			return nil
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &fileGroupProbe{
		id:      fileGroupCallSequence.Add(1),
		logger:  logger,
		started: time.Now(),
	}
}

// runAttempt observes a managed transaction callback. A completed statement
// is only an attempt; ExecuteWrite decides whether the group commits.
func (p *fileGroupProbe) runAttempt(
	ctx context.Context,
	stmts []sourcecypher.Statement,
	run fileStatementRunner,
) ([]sourcecypher.StatementRetractionCounts, error) {
	p.attempt++
	p.lastCallbackSuccess = false
	counts := make([]sourcecypher.StatementRetractionCounts, 0, len(stmts))
	for index, stmt := range stmts {
		templateID, rows, ok := sourcecypher.CanonicalFileStatementProfile(stmt)
		if !ok {
			return nil, fmt.Errorf("file group contains an unknown statement")
		}
		runStarted := time.Now()
		consume, err := run(ctx, stmt)
		runDuration := time.Since(runStarted)
		if err != nil {
			p.logStatement(ctx, index, len(stmts), templateID, rows, runDuration, 0, "run_failed")
			return nil, err
		}
		consumeStarted := time.Now()
		count, err := consume(ctx)
		consumeDuration := time.Since(consumeStarted)
		outcome := "attempt_completed"
		if err != nil {
			outcome = "consume_failed"
		}
		p.logStatement(ctx, index, len(stmts), templateID, rows, runDuration, consumeDuration, outcome)
		if err != nil {
			return nil, err
		}
		counts = append(counts, count)
	}
	p.lastCallbackSuccess = true
	p.lastCallbackEnded = time.Now()
	return counts, nil
}

func (p *fileGroupProbe) logStatement(
	ctx context.Context,
	index, total int,
	templateID string,
	rows int,
	runDuration, consumeDuration time.Duration,
	outcome string,
) {
	attrs := []any{
		telemetry.LogKeyPipelinePhase, telemetry.PhaseProjection,
		telemetry.LogKeyFileGroupCallID, p.id,
		telemetry.LogKeyFileGroupAttempt, p.attempt,
		telemetry.LogKeyFileGroupStatementIndex, index + 1,
		telemetry.LogKeyFileGroupStatementCount, total,
		telemetry.LogKeyFileGroupTemplateID, templateID,
		telemetry.LogKeyFileGroupRowCount, rows,
		telemetry.LogKeyFileGroupRunDuration, runDuration.Seconds(),
		telemetry.LogKeyFileGroupOutcome, outcome,
	}
	if outcome != "run_failed" {
		attrs = append(attrs, telemetry.LogKeyFileGroupConsumeDuration, consumeDuration.Seconds())
	}
	p.logger.InfoContext(ctx, "file graph statement attempt", attrs...)
}

func (p *fileGroupProbe) finish(ctx context.Context, err error) {
	outcome := "succeeded"
	if err != nil {
		outcome = "failed"
	}
	attrs := []any{
		telemetry.LogKeyPipelinePhase, telemetry.PhaseProjection,
		telemetry.LogKeyFileGroupCallID, p.id,
		telemetry.LogKeyFileGroupAttempts, p.attempt,
		telemetry.LogKeyFileGroupOutcome, outcome,
		telemetry.LogKeyFileGroupDuration, time.Since(p.started).Seconds(),
	}
	if p.lastCallbackSuccess {
		attrs = append(attrs, telemetry.LogKeyFileGroupPostCallbackDuration, time.Since(p.lastCallbackEnded).Seconds())
	}
	p.logger.InfoContext(ctx, "file graph group completed", attrs...)
}
