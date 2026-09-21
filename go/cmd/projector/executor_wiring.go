// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/graph/capture"
	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func projectorCanonicalExecutorForGraphBackend(
	rawExecutor sourcecypher.Executor,
	graphBackend runtimecfg.GraphBackend,
	nornicDBConfig projectorNornicDBConfig,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	captureSession *capture.Session,
) sourcecypher.Executor {
	instrumentedExecutor := &sourcecypher.InstrumentedExecutor{
		Inner: &sourcecypher.RetryingExecutor{
			Inner:       rawExecutor,
			MaxRetries:  3,
			Instruments: instruments,
		},
		Tracer:      tracer,
		Instruments: instruments,
	}
	var outer sourcecypher.Executor = instrumentedExecutor
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		canonicalTimeout := projectorNornicDBCanonicalWriteTimeout(getenv)
		bounded := sourcecypher.TimeoutExecutor{
			Inner:       instrumentedExecutor,
			Timeout:     canonicalTimeout,
			TimeoutHint: canonicalWriteTimeoutEnv,
		}
		gate := graphbackpressure.NewGate(
			graphbackpressure.ClassMaxInFlight(getenv, graphbackpressure.CanonicalMaxInFlightEnv),
			instruments,
			graphbackpressure.CanonicalGateName,
		)
		inner := graphbackpressure.WrapExecutorWithGate(bounded, gate)
		// Capture the inner grouped layer, never the phase-only outer this
		// function returns below: the outer fan-out calls Execute and
		// ExecuteGroup on this value, both of which the capture decorator
		// records and forwards. A nil session returns it unchanged.
		inner = captureSession.Writer(inner)
		var drainReader storagenornicdb.DrainReader
		if reader, ok := rawExecutor.(storagenornicdb.DrainReader); ok {
			drainReader = projectorTimeoutDrainReader{
				inner:       reader,
				timeout:     canonicalTimeout,
				timeoutHint: canonicalWriteTimeoutEnv,
			}
			if gate != nil {
				drainReader = projectorGatedDrainReader{inner: drainReader, gate: gate}
			}
		}
		return storagenornicdb.PhaseGroupExecutor{
			Inner:                       inner,
			MaxStatements:               nornicDBConfig.PhaseGroupStatements,
			DirectoryMaxStatements:      storagenornicdb.DefaultDirectoryPhaseStatements,
			FileMaxStatements:           nornicDBConfig.FilePhaseGroupStatements,
			StructuralEdgeMaxStatements: nornicDBConfig.StructuralEdgePhaseGroupStatements,
			EntityMaxStatements:         nornicDBConfig.EntityPhaseGroupStatements,
			EntityLabelMaxStatements:    nornicDBConfig.EntityLabelPhaseStatements,
			EntityPhaseConcurrency:      nornicDBConfig.EntityPhaseConcurrency,
			DrainReader:                 drainReader,
			RetractBatchSize:            nornicDBConfig.CanonicalRetractBatchSize,
			Instruments:                 instruments,
		}
	}
	// Bound concurrent canonical writes so a slow graph backend slows intake
	// instead of dead-lettering recoverable projector work (issue #3560). The
	// wrapper sits outside retry/timeout so one permit covers a whole write
	// attempt; a non-positive ESHU_GRAPH_WRITE_MAX_IN_FLIGHT leaves it a
	// passthrough. A nil session returns the chain unchanged.
	return captureSession.Writer(graphbackpressure.Wrap(
		outer,
		graphbackpressure.MaxInFlight(getenv),
		instruments,
	))
}
