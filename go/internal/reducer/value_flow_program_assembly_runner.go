// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ValueFlowProgramInputLoader loads bounded value-flow Program assembly inputs.
type ValueFlowProgramInputLoader interface {
	LoadPendingValueFlowProgramInputs(ctx context.Context, limit int) ([]ValueFlowProgramInput, error)
}

// ValueFlowProgramAssemblyRunnerConfig configures one value-flow Program
// assembly cycle.
type ValueFlowProgramAssemblyRunnerConfig struct {
	BatchLimit int
}

// ValueFlowProgramAssemblyResult summarizes one loader/assembly cycle.
type ValueFlowProgramAssemblyResult struct {
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

// ValueFlowProgramAssemblyRunner assembles value-flow Programs from active
// CALLS edges and persisted function summaries. It does not solve or write
// graph evidence.
type ValueFlowProgramAssemblyRunner struct {
	InputLoader ValueFlowProgramInputLoader
	Config      ValueFlowProgramAssemblyRunnerConfig
	Logger      *slog.Logger
}

// ProcessOnce loads and assembles one bounded batch of value-flow Programs.
func (r ValueFlowProgramAssemblyRunner) ProcessOnce(ctx context.Context) (ValueFlowProgramAssemblyResult, error) {
	if r.InputLoader == nil {
		return ValueFlowProgramAssemblyResult{}, fmt.Errorf("value-flow program input loader is required")
	}
	start := time.Now()
	inputs, err := r.InputLoader.LoadPendingValueFlowProgramInputs(ctx, r.batchLimit())
	if err != nil {
		return ValueFlowProgramAssemblyResult{}, fmt.Errorf("load value-flow program inputs: %w", err)
	}

	result := ValueFlowProgramAssemblyResult{
		InputsProcessed: len(inputs),
	}
	for _, input := range inputs {
		_, stats := BuildValueFlowProgram(input)
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

func (r ValueFlowProgramAssemblyRunner) batchLimit() int {
	if r.Config.BatchLimit <= 0 {
		return 10
	}
	return r.Config.BatchLimit
}
