// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// PlatformInfraMaterializationHandler reduces one per-repository
// platform_infra_materialization follow-up into canonical
// Repository-[:PROVISIONS_PLATFORM]->Platform edges. It extracts
// Terraform/terragrunt IaC platform-provisioning signals from the repository's
// facts and writes the inferred platforms directly via the locked infrastructure
// materializer.
//
// This is the dedicated home for the infrastructure-provisioning verb: it
// replaces the side-effect that previously rode the deployment_mapping handler.
// The write path is deliberately the direct, advisory-locked materializer (not
// the platform_infra shared-projection worker) because Platform.id carries a
// UNIQUE constraint and workload_materialization also MERGEs Platform nodes; the
// two must serialize through the shared platform_graph reducer conflict key to
// avoid a commit-time MERGE race. Routing platform writes through the
// shared-projection worker — a separate subsystem with no shared serialization —
// would reintroduce that race.
type PlatformInfraMaterializationHandler struct {
	FactLoader                 FactLoader
	InfrastructureMaterializer *InfrastructurePlatformMaterializer
	PlatformGraphLocker        PlatformGraphLocker
}

// Handle executes the platform infrastructure materialization path.
func (h PlatformInfraMaterializationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainPlatformInfraMaterialization {
		return Result{}, fmt.Errorf(
			"platform infra materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("platform infra materialization fact loader is required")
	}
	if h.InfrastructureMaterializer == nil {
		return Result{}, fmt.Errorf("platform infra materialization materializer is required")
	}

	timing := platformInfraMaterializationTiming{intent: intent}
	totalStart := time.Now()

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factKindRepository, factKindFile, factKindParsedFile},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for platform infra materialization: %w", err)
	}
	timing.loadDuration = time.Since(loadStart)
	timing.factCount = len(envelopes)
	inputReady := len(envelopes) > 0

	extractStart := time.Now()
	rows := ExtractInfrastructurePlatformRows(envelopes)
	timing.platformRowCount = len(rows)
	timing.extractDuration = time.Since(extractStart)

	if len(rows) > 0 {
		writeStart := time.Now()
		result, materializeErr := h.materializeInfrastructurePlatforms(ctx, rows)
		timing.writeDuration = time.Since(writeStart)
		if materializeErr != nil {
			return Result{}, fmt.Errorf("materialize infrastructure platforms: %w", materializeErr)
		}
		timing.platformEdgesWritten = result.PlatformEdgesWritten
	}

	timing.totalDuration = time.Since(totalStart)
	logPlatformInfraMaterializationCompleted(ctx, timing)

	summary := fmt.Sprintf(
		"materialized %d platform(s) from %d repository fact(s)",
		timing.platformEdgesWritten,
		timing.factCount,
	)
	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainPlatformInfraMaterialization,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: summary,
		CanonicalWrites: timing.platformEdgesWritten,
		SubDurations: map[string]float64{
			"load_facts":        timing.loadDuration.Seconds(),
			"extract_rows":      timing.extractDuration.Seconds(),
			"infra_graph_write": timing.writeDuration.Seconds(),
			"total":             timing.totalDuration.Seconds(),
		},
		SubSignals: materializationDiagnosticSignals(inputReady, timing.platformEdgesWritten),
	}, nil
}

// materializeInfrastructurePlatforms writes the inferred PROVISIONS_PLATFORM rows
// under the platform graph advisory locks when a locker is configured, so
// concurrent platform writers serialize on Platform.id. When no locker is set
// (tests), it writes directly.
func (h PlatformInfraMaterializationHandler) materializeInfrastructurePlatforms(
	ctx context.Context,
	rows []InfrastructurePlatformRow,
) (InfrastructurePlatformResult, error) {
	if h.PlatformGraphLocker == nil {
		return h.InfrastructureMaterializer.Materialize(ctx, rows)
	}

	var result InfrastructurePlatformResult
	err := h.PlatformGraphLocker.WithPlatformLocks(
		ctx,
		infrastructurePlatformLockIDs(rows),
		func(lockCtx context.Context) error {
			var writeErr error
			result, writeErr = h.InfrastructureMaterializer.Materialize(lockCtx, rows)
			return writeErr
		},
	)
	if err != nil {
		return InfrastructurePlatformResult{}, err
	}
	return result, nil
}

// infrastructurePlatformLockIDs returns the de-duplicated, sorted platform IDs to
// lock for one materialization pass so concurrent writers serialize per platform.
func infrastructurePlatformLockIDs(rows []InfrastructurePlatformRow) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		if key := strings.TrimSpace(row.PlatformID); key != "" {
			keys = append(keys, key)
		}
	}
	return uniqueSortedStrings(keys)
}

type platformInfraMaterializationTiming struct {
	intent               Intent
	factCount            int
	platformRowCount     int
	platformEdgesWritten int
	loadDuration         time.Duration
	extractDuration      time.Duration
	writeDuration        time.Duration
	totalDuration        time.Duration
}

func logPlatformInfraMaterializationCompleted(ctx context.Context, timing platformInfraMaterializationTiming) {
	slog.InfoContext(
		ctx, "platform infra materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("platform_row_count", timing.platformRowCount),
		slog.Int("platform_edges_written", timing.platformEdgesWritten),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("infra_graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
