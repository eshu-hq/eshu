// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
)

// projectorGatedDrainReader wraps a storagenornicdb.DrainReader so each
// full-refresh DETACH DELETE drain write, and the bounded existence probe
// that precedes a bare-label drain (#6822), draws a permit from the shared
// canonical graph-write gate. The probe and the drain acquire under distinct
// labels (`canonical_probe` vs `canonical_retract_drain`) so backpressure
// telemetry attributes each correctly instead of folding the probe into the
// drain's label.
type projectorGatedDrainReader struct {
	inner storagenornicdb.DrainReader
	gate  *sourcecypher.BackpressureGate
}

// projectorTimeoutDrainReader gives each full-refresh DETACH DELETE drain
// iteration, and the bounded existence probe that precedes a bare-label drain
// (#6822), its own client deadline. The probe and the drain report distinct
// GraphWriteTimeoutError.Operation strings ("nornicdb probe timed out" vs
// "nornicdb drain timed out") so an operator can tell which one stalled.
type projectorTimeoutDrainReader struct {
	inner       storagenornicdb.DrainReader
	timeout     time.Duration
	timeoutHint string
}

// RunProbe bounds one bounded existence probe with a fresh child context,
// reporting a stalled probe as "nornicdb probe timed out" rather than the
// drain's "nornicdb drain timed out" label.
func (r projectorTimeoutDrainReader) RunProbe(
	ctx context.Context,
	cypher string,
	parameters map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	return r.runBounded(ctx, cypher, parameters, r.inner.RunProbe, "nornicdb probe timed out", "run nornicdb probe")
}

// RunWrite bounds one drain iteration with a fresh child context.
func (r projectorTimeoutDrainReader) RunWrite(
	ctx context.Context,
	cypher string,
	parameters map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	return r.runBounded(ctx, cypher, parameters, r.inner.RunWrite, "nornicdb drain timed out", "run nornicdb drain")
}

// runBounded is the shared per-call deadline logic behind RunProbe and
// RunWrite: it differs only in which inner method it calls and which
// timeout-operation/error-prefix labels the resulting error carries, so the
// probe and the drain can never be confused with each other downstream.
func (r projectorTimeoutDrainReader) runBounded(
	ctx context.Context,
	cypher string,
	parameters map[string]any,
	call func(context.Context, string, map[string]any) (storagenornicdb.DrainWriteResult, error),
	timeoutOperation string,
	wrapPrefix string,
) (storagenornicdb.DrainWriteResult, error) {
	boundedCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	result, err := call(boundedCtx, cypher, parameters)
	if err == nil {
		return result, nil
	}
	if errors.Is(boundedCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return storagenornicdb.DrainWriteResult{}, sourcecypher.GraphWriteTimeoutError{
			Operation:   timeoutOperation,
			Timeout:     r.timeout,
			TimeoutHint: r.timeoutHint,
			Cause:       context.DeadlineExceeded,
		}
	}
	return storagenornicdb.DrainWriteResult{}, fmt.Errorf("%s: %w", wrapPrefix, err)
}

// RunProbe acquires a canonical-gate permit under the `canonical_probe` label
// for the bounded existence probe, then delegates to the wrapped reader.
func (r projectorGatedDrainReader) RunProbe(
	ctx context.Context,
	cypher string,
	parameters map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	release, err := r.gate.Acquire(ctx, string(sourcecypher.OperationCanonicalProbe))
	if err != nil {
		return storagenornicdb.DrainWriteResult{}, err
	}
	defer release()
	return r.inner.RunProbe(ctx, cypher, parameters)
}

// RunWrite acquires a canonical-gate permit for the drain write, then
// delegates to the wrapped reader.
func (r projectorGatedDrainReader) RunWrite(
	ctx context.Context,
	cypher string,
	parameters map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	release, err := r.gate.Acquire(ctx, "canonical_retract_drain")
	if err != nil {
		return storagenornicdb.DrainWriteResult{}, err
	}
	defer release()
	return r.inner.RunWrite(ctx, cypher, parameters)
}

const (
	projectorNornicDBCanonicalGroupedWritesEnv             = "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES"
	projectorNornicDBPhaseGroupStatementsEnv               = "ESHU_NORNICDB_PHASE_GROUP_STATEMENTS"
	projectorNornicDBFilePhaseGroupStatementsEnv           = "ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS"
	projectorNornicDBStructuralEdgePhaseGroupStatementsEnv = "ESHU_NORNICDB_STRUCTURAL_EDGE_PHASE_GROUP_STATEMENTS"
	projectorNornicDBFileBatchSizeEnv                      = "ESHU_NORNICDB_FILE_BATCH_SIZE"
	projectorNornicDBEntityPhaseGroupStatementsEnv         = "ESHU_NORNICDB_ENTITY_PHASE_GROUP_STATEMENTS"
	projectorNornicDBEntityBatchSizeEnv                    = "ESHU_NORNICDB_ENTITY_BATCH_SIZE"
	projectorNornicDBEntityLabelBatchSizesEnv              = "ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES"
	projectorNornicDBEntityLabelPhaseGroupStatementsEnv    = "ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS"
	projectorNornicDBBatchedEntityContainmentEnv           = "ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT"
	projectorNornicDBEntityPhaseConcurrencyEnv             = "ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY"
	projectorNornicDBCanonicalRetractBatchEnv              = "ESHU_CANONICAL_RETRACT_BATCH"
)

type projectorNornicDBConfig struct {
	GroupedWritesRequested             bool
	PhaseGroupStatements               int
	FilePhaseGroupStatements           int
	StructuralEdgePhaseGroupStatements int
	FileBatchSize                      int
	EntityPhaseGroupStatements         int
	EntityBatchSize                    int
	EntityLabelBatchSizes              map[string]int
	EntityLabelPhaseStatements         map[string]int
	BatchedEntityContainment           bool
	EntityPhaseConcurrency             int
	CanonicalRetractBatchSize          int
}

func loadProjectorNornicDBConfig(getenv func(string) string) (projectorNornicDBConfig, error) {
	groupedWrites, err := projectorNornicDBBool(getenv, projectorNornicDBCanonicalGroupedWritesEnv, false)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	phaseGroupStatements, err := projectorNornicDBPositiveInt(
		getenv, projectorNornicDBPhaseGroupStatementsEnv, storagenornicdb.DefaultPhaseGroupStatements,
	)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	filePhaseGroupStatements, err := projectorNornicDBPositiveInt(
		getenv, projectorNornicDBFilePhaseGroupStatementsEnv, storagenornicdb.DefaultFilePhaseStatements,
	)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	structuralEdgePhaseGroupStatements, err := projectorNornicDBPositiveInt(
		getenv, projectorNornicDBStructuralEdgePhaseGroupStatementsEnv, storagenornicdb.DefaultStructuralEdgePhaseStatements,
	)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	fileBatchSize, err := projectorNornicDBPositiveInt(
		getenv, projectorNornicDBFileBatchSizeEnv, storagenornicdb.DefaultFileBatchSize,
	)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	entityPhaseGroupStatements, err := projectorNornicDBPositiveInt(
		getenv, projectorNornicDBEntityPhaseGroupStatementsEnv, storagenornicdb.DefaultEntityPhaseStatements,
	)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	entityBatchSize, err := projectorNornicDBPositiveInt(
		getenv, projectorNornicDBEntityBatchSizeEnv, storagenornicdb.DefaultEntityBatchSize,
	)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	entityLabelBatchSizes, err := projectorNornicDBLabelSizes(
		getenv,
		projectorNornicDBEntityLabelBatchSizesEnv,
		storagenornicdb.DefaultEntityLabelBatchSizes(entityBatchSize),
		entityBatchSize,
	)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	entityLabelPhaseStatements, err := projectorNornicDBLabelSizes(
		getenv,
		projectorNornicDBEntityLabelPhaseGroupStatementsEnv,
		storagenornicdb.DefaultEntityLabelPhaseStatements(entityPhaseGroupStatements),
		entityPhaseGroupStatements,
	)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	batchedEntityContainment, err := projectorNornicDBBool(
		getenv,
		projectorNornicDBBatchedEntityContainmentEnv,
		storagenornicdb.DefaultBatchedEntityContainment,
	)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	entityPhaseConcurrency, err := projectorNornicDBEntityPhaseConcurrency(getenv)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	retractBatchSize, err := projectorNornicDBPositiveInt(
		getenv, projectorNornicDBCanonicalRetractBatchEnv, storagenornicdb.DefaultCanonicalRetractBatchSize,
	)
	if err != nil {
		return projectorNornicDBConfig{}, err
	}
	if retractBatchSize < storagenornicdb.MinCanonicalRetractBatchSize ||
		retractBatchSize > storagenornicdb.MaxCanonicalRetractBatchSize {
		return projectorNornicDBConfig{}, fmt.Errorf(
			"parse %s=%q: must be within %d..%d",
			projectorNornicDBCanonicalRetractBatchEnv,
			strings.TrimSpace(getenv(projectorNornicDBCanonicalRetractBatchEnv)),
			storagenornicdb.MinCanonicalRetractBatchSize,
			storagenornicdb.MaxCanonicalRetractBatchSize,
		)
	}

	return projectorNornicDBConfig{
		GroupedWritesRequested:             groupedWrites,
		PhaseGroupStatements:               phaseGroupStatements,
		FilePhaseGroupStatements:           filePhaseGroupStatements,
		StructuralEdgePhaseGroupStatements: structuralEdgePhaseGroupStatements,
		FileBatchSize:                      fileBatchSize,
		EntityPhaseGroupStatements:         entityPhaseGroupStatements,
		EntityBatchSize:                    entityBatchSize,
		EntityLabelBatchSizes:              entityLabelBatchSizes,
		EntityLabelPhaseStatements:         entityLabelPhaseStatements,
		BatchedEntityContainment:           batchedEntityContainment,
		EntityPhaseConcurrency:             entityPhaseConcurrency,
		CanonicalRetractBatchSize:          retractBatchSize,
	}, nil
}

func projectorNornicDBPositiveInt(
	getenv func(string) string,
	name string,
	fallback int,
) (int, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("parse %s=%q: must be a positive integer", name, raw)
	}
	return value, nil
}

func projectorNornicDBBool(getenv func(string) string, name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s=%q: %w", name, raw, err)
	}
	return value, nil
}

func projectorNornicDBEntityPhaseConcurrency(getenv func(string) string) (int, error) {
	value, err := projectorNornicDBPositiveInt(
		getenv,
		projectorNornicDBEntityPhaseConcurrencyEnv,
		storagenornicdb.DefaultEntityPhaseConcurrency(),
	)
	if err != nil {
		return 0, err
	}
	if value > storagenornicdb.EntityPhaseConcurrencyCap {
		return storagenornicdb.EntityPhaseConcurrencyCap, nil
	}
	return value, nil
}

func projectorNornicDBLabelSizes(
	getenv func(string) string,
	name string,
	defaults map[string]int,
	ceiling int,
) (map[string]int, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return defaults, nil
	}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		label, valueRaw, ok := strings.Cut(entry, "=")
		label = strings.TrimSpace(label)
		if !ok || label == "" {
			return nil, fmt.Errorf("parse %s=%q: entries must be Label=size", name, raw)
		}
		value, err := strconv.Atoi(strings.TrimSpace(valueRaw))
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("parse %s=%q: label %q must have a positive integer size", name, raw, label)
		}
		if ceiling > 0 && value > ceiling {
			value = ceiling
		}
		defaults[label] = value
	}
	return defaults, nil
}

func configureProjectorCanonicalWriter(
	writer *sourcecypher.CanonicalNodeWriter,
	graphBackend runtimecfg.GraphBackend,
	config projectorNornicDBConfig,
) *sourcecypher.CanonicalNodeWriter {
	if writer == nil || graphBackend != runtimecfg.GraphBackendNornicDB {
		return writer
	}
	return storagenornicdb.ConfigureCanonicalWriter(writer, storagenornicdb.WriterConfig{
		FileBatchSize:            config.FileBatchSize,
		EntityBatchSize:          config.EntityBatchSize,
		EntityLabelBatchSizes:    config.EntityLabelBatchSizes,
		BatchedEntityContainment: config.BatchedEntityContainment,
	})
}
