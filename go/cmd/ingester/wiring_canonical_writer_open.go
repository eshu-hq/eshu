// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/projector"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// openIngesterCanonicalWriter opens the canonical graph writer for the
// configured backend (Neo4j or NornicDB), applying the backend-specific
// batching and phase-group tuning knobs read from the environment.
func openIngesterCanonicalWriter(
	parent context.Context,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) (projector.CanonicalWriter, io.Closer, error) {
	if writer, closer, ok := maybeLocalLightweightCanonicalWriter(getenv); ok {
		return writer, closer, nil
	}
	graphBackend, err := runtimecfg.LoadGraphBackend(getenv)
	if err != nil {
		return nil, nil, err
	}
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(parent, getenv)
	if err != nil {
		return nil, nil, err
	}
	failAfterDriverOpen := func(err error) (projector.CanonicalWriter, io.Closer, error) {
		_ = closeIngesterNeo4jDriver(driver)
		return nil, nil, err
	}

	profileGroupStatements, err := neo4jProfileGroupStatements(getenv)
	if err != nil {
		return failAfterDriverOpen(err)
	}
	rawExecutor := ingesterNeo4jExecutor{
		Driver:                 driver,
		DatabaseName:           cfg.DatabaseName,
		TxTimeout:              canonicalTransactionTimeout(graphBackend, getenv),
		ProfileGroupStatements: profileGroupStatements,
		Instruments:            instruments,
	}

	nornicDBGroupedWrites := false
	phaseGroupStatements := defaultNornicDBPhaseGroupStatements
	filePhaseStatements := defaultNornicDBFilePhaseStatements
	fileBatchSize := 0
	entityPhaseStatements := defaultNornicDBEntityPhaseStatements
	entityBatchSize := 0
	entityLabelPhaseStatements := map[string]int(nil)
	nornicDBBatchedEntityContainment := false
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		nornicDBGroupedWrites, err = nornicDBCanonicalGroupedWrites(getenv)
		if err != nil {
			return failAfterDriverOpen(err)
		}
		phaseGroupStatements, err = nornicDBPhaseGroupStatements(getenv)
		if err != nil {
			return failAfterDriverOpen(err)
		}
		filePhaseStatements, err = nornicDBFilePhaseGroupStatements(getenv)
		if err != nil {
			return failAfterDriverOpen(err)
		}
		fileBatchSize, err = nornicDBFileBatchSize(getenv)
		if err != nil {
			return failAfterDriverOpen(err)
		}
		entityPhaseStatements, err = nornicDBEntityPhaseGroupStatements(getenv)
		if err != nil {
			return failAfterDriverOpen(err)
		}
		entityBatchSize, err = nornicDBEntityBatchSize(getenv)
		if err != nil {
			return failAfterDriverOpen(err)
		}
		entityLabelPhaseStatements, err = nornicDBEntityLabelPhaseGroupStatements(getenv, entityPhaseStatements)
		if err != nil {
			return failAfterDriverOpen(err)
		}
		nornicDBBatchedEntityContainment, err = nornicDBBatchedEntityContainmentEnabled(getenv)
		if err != nil {
			return failAfterDriverOpen(err)
		}
	}

	entityPhaseConcurrency := 0
	retractBatchSize := defaultNornicDBCanonicalRetractBatchSize
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		entityPhaseConcurrency, err = nornicDBEntityPhaseConcurrency(getenv)
		if err != nil {
			return failAfterDriverOpen(err)
		}
		retractBatchSize, err = nornicDBCanonicalRetractBatchSize(getenv)
		if err != nil {
			return failAfterDriverOpen(err)
		}
	}
	writer := sourcecypher.NewCanonicalNodeWriter(
		canonicalExecutorForGraphBackend(
			rawExecutor,
			graphBackend,
			nornicDBCanonicalWriteTimeout(getenv),
			nornicDBGroupedWrites,
			phaseGroupStatements,
			filePhaseStatements,
			entityPhaseStatements,
			entityLabelPhaseStatements,
			entityPhaseConcurrency,
			retractBatchSize,
			tracer,
			instruments,
		),
		neo4jBatchSize(getenv),
		instruments,
	).WithTracer(tracer)
	labelBatchSizes := map[string]int(nil)
	orderedLabels := []string(nil)
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		if nornicDBBatchedEntityContainment {
			slog.Info("NornicDB batched entity containment enabled",
				"graph_backend", string(graphBackend),
				"env_var", nornicDBBatchedEntityContainmentEnv)
		}
		labelBatchSizes, err = nornicDBEntityLabelBatchSizes(getenv, entityBatchSize)
		if err != nil {
			return failAfterDriverOpen(err)
		}
		orderedLabels = orderedEntityBatchLabels(labelBatchSizes)
	}
	writer = configureIngesterCanonicalWriter(writer, ingesterCanonicalWriterConfig{
		GraphBackend:                      graphBackend,
		FileBatchSize:                     fileBatchSize,
		EntityBatchSize:                   entityBatchSize,
		EntityLabelBatchSizes:             labelBatchSizes,
		NornicDBBatchedEntityContainment:  nornicDBBatchedEntityContainment,
		OrderedEntityLabelBatchSizeLabels: orderedLabels,
	})

	return writer, ingesterNeo4jDriverCloser{Driver: driver}, nil
}
