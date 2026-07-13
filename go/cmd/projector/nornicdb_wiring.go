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

type projectorGatedDrainReader struct {
	inner storagenornicdb.DrainReader
	gate  *sourcecypher.BackpressureGate
}

type projectorTimeoutDrainReader struct {
	inner       storagenornicdb.DrainReader
	timeout     time.Duration
	timeoutHint string
}

func (r projectorTimeoutDrainReader) RunWrite(
	ctx context.Context,
	cypher string,
	parameters map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	boundedCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	result, err := r.inner.RunWrite(boundedCtx, cypher, parameters)
	if err == nil {
		return result, nil
	}
	if errors.Is(boundedCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return storagenornicdb.DrainWriteResult{}, sourcecypher.GraphWriteTimeoutError{
			Operation:   "nornicdb drain timed out",
			Timeout:     r.timeout,
			TimeoutHint: r.timeoutHint,
			Cause:       context.DeadlineExceeded,
		}
	}
	return storagenornicdb.DrainWriteResult{}, fmt.Errorf("run nornicdb drain: %w", err)
}

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
	projectorNornicDBCanonicalGroupedWritesEnv          = "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES"
	projectorNornicDBPhaseGroupStatementsEnv            = "ESHU_NORNICDB_PHASE_GROUP_STATEMENTS"
	projectorNornicDBFilePhaseGroupStatementsEnv        = "ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS"
	projectorNornicDBFileBatchSizeEnv                   = "ESHU_NORNICDB_FILE_BATCH_SIZE"
	projectorNornicDBEntityPhaseGroupStatementsEnv      = "ESHU_NORNICDB_ENTITY_PHASE_GROUP_STATEMENTS"
	projectorNornicDBEntityBatchSizeEnv                 = "ESHU_NORNICDB_ENTITY_BATCH_SIZE"
	projectorNornicDBEntityLabelBatchSizesEnv           = "ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES"
	projectorNornicDBEntityLabelPhaseGroupStatementsEnv = "ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS"
	projectorNornicDBBatchedEntityContainmentEnv        = "ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT"
	projectorNornicDBEntityPhaseConcurrencyEnv          = "ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY"
	projectorNornicDBCanonicalRetractBatchEnv           = "ESHU_CANONICAL_RETRACT_BATCH"
)

type projectorNornicDBConfig struct {
	GroupedWritesRequested     bool
	PhaseGroupStatements       int
	FilePhaseGroupStatements   int
	FileBatchSize              int
	EntityPhaseGroupStatements int
	EntityBatchSize            int
	EntityLabelBatchSizes      map[string]int
	EntityLabelPhaseStatements map[string]int
	BatchedEntityContainment   bool
	EntityPhaseConcurrency     int
	CanonicalRetractBatchSize  int
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
		GroupedWritesRequested:     groupedWrites,
		PhaseGroupStatements:       phaseGroupStatements,
		FilePhaseGroupStatements:   filePhaseGroupStatements,
		FileBatchSize:              fileBatchSize,
		EntityPhaseGroupStatements: entityPhaseGroupStatements,
		EntityBatchSize:            entityBatchSize,
		EntityLabelBatchSizes:      entityLabelBatchSizes,
		EntityLabelPhaseStatements: entityLabelPhaseStatements,
		BatchedEntityContainment:   batchedEntityContainment,
		EntityPhaseConcurrency:     entityPhaseConcurrency,
		CanonicalRetractBatchSize:  retractBatchSize,
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
