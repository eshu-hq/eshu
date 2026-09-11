// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package value

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ProgramInputLoader loads bounded value-flow Program assembly inputs.
type ProgramInputLoader interface {
	LoadPendingProgramInputs(ctx context.Context, limit int) ([]ProgramInput, error)
}

// ProgramAssemblyRunnerConfig configures one value-flow Program
// assembly cycle.
type ProgramAssemblyRunnerConfig struct {
	BatchLimit int
}

// ProgramAssemblyResult summarizes one loader/assembly cycle.
type ProgramAssemblyResult struct {
	InputsProcessed            int
	SummaryCount               int
	CallEdgeCount              int
	ProgramEdgeCount           int
	SourceCount                int
	SinkCount                  int
	SkippedMissingIdentity     int
	SkippedMissingSummary      int
	SkippedUnconfirmedCallFlow int
	DurationSeconds            float64
}

// ProgramAssemblyRunner assembles value-flow Programs from active
// CALLS edges and persisted function summaries. It does not solve or write
// graph evidence.
type ProgramAssemblyRunner struct {
	InputLoader ProgramInputLoader
	Config      ProgramAssemblyRunnerConfig
	Logger      *slog.Logger
}

// ProcessOnce loads and assembles one bounded batch of value-flow Programs.
func (r ProgramAssemblyRunner) ProcessOnce(ctx context.Context) (ProgramAssemblyResult, error) {
	if r.InputLoader == nil {
		return ProgramAssemblyResult{}, fmt.Errorf("value-flow program input loader is required")
	}
	start := time.Now()
	inputs, err := r.InputLoader.LoadPendingProgramInputs(ctx, r.batchLimit())
	if err != nil {
		return ProgramAssemblyResult{}, fmt.Errorf("load value-flow program inputs: %w", err)
	}

	result := ProgramAssemblyResult{
		InputsProcessed: len(inputs),
	}
	for _, input := range inputs {
		_, stats := BuildProgram(input)
		result.SummaryCount += stats.SummaryCount
		result.CallEdgeCount += stats.CallEdgeCount
		result.ProgramEdgeCount += stats.ProgramEdgeCount
		result.SourceCount += stats.SourceCount
		result.SinkCount += stats.SinkCount
		result.SkippedMissingIdentity += stats.SkippedCallEdgeMissingID + stats.SkippedCallEdgeMissingCallee
		result.SkippedMissingSummary += stats.SkippedMissingSummary
		result.SkippedUnconfirmedCallFlow += stats.SkippedUnconfirmedCallFlow
	}
	result.DurationSeconds = time.Since(start).Seconds()
	if r.Logger != nil && result.InputsProcessed > 0 {
		r.Logger.Info(
			"value-flow program assembly completed",
			slog.Int("input_count", result.InputsProcessed),
			slog.Int("summary_count", result.SummaryCount),
			slog.Int("call_edge_count", result.CallEdgeCount),
			slog.Int("program_edge_count", result.ProgramEdgeCount),
			slog.Int("source_count", result.SourceCount),
			slog.Int("sink_count", result.SinkCount),
			slog.Int("skipped_missing_identity", result.SkippedMissingIdentity),
			slog.Int("skipped_missing_summary", result.SkippedMissingSummary),
			slog.Int("skipped_unconfirmed_call_flow", result.SkippedUnconfirmedCallFlow),
			slog.Float64("duration_seconds", result.DurationSeconds),
		)
	}
	return result, nil
}

func (r ProgramAssemblyRunner) batchLimit() int {
	if r.Config.BatchLimit <= 0 {
		return 10
	}
	return r.Config.BatchLimit
}
