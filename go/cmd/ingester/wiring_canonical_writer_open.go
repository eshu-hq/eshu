// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newIngesterCanonicalGate builds the single in-flight permit pool that bounds
// the ingester's concurrent canonical NornicDB writes (#4729 / #4456), matching
// bootstrap-index and the reducer so all three writer processes honor
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT (or its canonical-class override
// ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT) instead of leaving the ingester
// unbounded. A non-positive ceiling (both env vars unset) yields a nil gate,
// which graphbackpressure.WrapExecutorWithGate treats as a passthrough.
//
// The gate is passed INTO canonicalExecutorForGraphBackend, which wraps the
// inner GroupExecutor layer beneath the phase-group fan-out — NOT the outer
// nornicDBPhaseGroupExecutor — so a permit bounds each concurrent inner
// ExecuteGroup and the phase-group capability survives. This writer is opened
// once at process startup and shared across every canonical write, so a single
// gate bounds the whole process.
func newIngesterCanonicalGate(
	getenv func(string) string,
	instruments *telemetry.Instruments,
) *sourcecypher.BackpressureGate {
	return graphbackpressure.NewGate(
		graphbackpressure.ClassMaxInFlight(getenv, graphbackpressure.CanonicalMaxInFlightEnv),
		instruments,
		graphbackpressure.CanonicalGateName,
	)
}

// openIngesterCanonicalWriter opens the canonical graph writer for the
// configured backend (Neo4j or NornicDB), applying the backend-specific
// batching and phase-group tuning knobs read from the environment. database
// is the already-open Postgres handle the ingester's main() owns (see
// cmd/ingester/main.go); it is threaded in, not opened here, so the
// #5443 MATCHES_STATE ownership resolver reuses the process's single
// Postgres connection pool instead of duplicating a lifecycle
// (cmd/projector's openProjectorCanonicalWriter follows the same pattern).
func openIngesterCanonicalWriter(
	parent context.Context,
	database postgres.SQLDB,
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
	canonicalExecutor := canonicalExecutorForGraphBackend(
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
		newIngesterCanonicalGate(getenv, instruments),
	)
	writer := sourcecypher.NewCanonicalNodeWriter(
		canonicalExecutor,
		neo4jBatchSize(getenv),
		instruments,
	).WithTracer(tracer).WithTerraformStateOwnershipResolver(
		ingesterTerraformStateOwnershipResolver{resolver: tfstatebackend.NewResolver(postgres.PostgresTerraformBackendQuery{DB: database})},
	).WithTerraformStateConfigMatchResolver(
		ingesterTerraformStateConfigMatchResolver{driver: driver, databaseName: cfg.DatabaseName},
	).WithKustomizeOverlayResolver(
		ingesterKustomizeOverlayResolver{driver: driver, databaseName: cfg.DatabaseName},
	)
	labelBatchSizes := map[string]int(nil)
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
	}
	writer = configureIngesterCanonicalWriter(writer, ingesterCanonicalWriterConfig{
		GraphBackend:                     graphBackend,
		FileBatchSize:                    fileBatchSize,
		EntityBatchSize:                  entityBatchSize,
		EntityLabelBatchSizes:            labelBatchSizes,
		NornicDBBatchedEntityContainment: nornicDBBatchedEntityContainment,
	})

	return writer, ingesterNeo4jDriverCloser{Driver: driver}, nil
}
