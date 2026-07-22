// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/projector"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type bootstrapDB interface {
	postgres.ExecQueryer
	Close() error
}

type graphDeps struct {
	writer projector.CanonicalWriter
	close  func() error
}

type bootstrapCommitter interface {
	collector.Committer
	BackfillAllRelationshipEvidence(context.Context, trace.Tracer, *telemetry.Instruments) error
	MaterializeIaCReachability(context.Context, trace.Tracer, *telemetry.Instruments) error
	ReopenDeploymentMappingWorkItems(context.Context, trace.Tracer, *telemetry.Instruments) error
	// ReopenCodeImportRepoEdgeWorkItems replays succeeded code_import_repo_edge
	// reducer work items so they re-run once the cross-scope package-registry
	// owner facts they join against are present — the same after-the-fact
	// dependency ReopenDeploymentMappingWorkItems handles. Idempotent.
	ReopenCodeImportRepoEdgeWorkItems(context.Context, trace.Tracer, *telemetry.Instruments) error
	// ReopenSucceededReducerWorkItems replays succeeded reducer work items for the
	// given additive correlation domains (e.g. deployable_unit_correlation) so they
	// re-run once the resolved relationships they consume — produced by the
	// deployment_mapping reopen + cross-repo resolution in an earlier drain — exist.
	// Idempotent.
	ReopenSucceededReducerWorkItems(context.Context, trace.Tracer, *telemetry.Instruments, []string) error
	// EnqueueConfigStateDriftIntents enqueues one config_state_drift reducer
	// intent per state_snapshot:* scope with an active generation. Phase 3.5
	// trigger required by the facts-first bootstrap ordering: drift consumes
	// both config-side parser facts and state-side collector facts, so the
	// reducer must re-claim those scopes after Phase 3 has reopened
	// deployment_mapping (see CLAUDE.md "Facts-First Bootstrap Ordering" and
	// the AGENTS.md note on Phase-4 re-trigger consumers). Idempotent: the
	// reducer queue dedupes on (domain, scope, generation).
	EnqueueConfigStateDriftIntents(context.Context, trace.Tracer, *telemetry.Instruments) error
}

type collectorDeps struct {
	source    collector.Source
	committer bootstrapCommitter
	// commitLanes is the number of concurrent commit lanes drainCollector
	// runs (#5130). Sized by commitLaneCount from
	// ESHU_BOOTSTRAP_COMMIT_LANES with a measured-plateau default.
	commitLanes int
}

type projectorDeps struct {
	workSource        projector.ProjectorWorkSource
	factStore         projector.FactStore
	runner            projector.ProjectionRunner
	workSink          projector.ProjectorWorkSink
	heartbeater       projector.ProjectorWorkHeartbeater
	heartbeatInterval time.Duration
}

type (
	openBootstrapDBFn              func(context.Context, func(string) string) (bootstrapDB, error)
	applyBootstrapFn               func(context.Context, bootstrapDB) error
	finalizeContentSearchIndexesFn func(context.Context, bootstrapDB) error
	openGraphFn                    func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error)
	buildCollectorFn               func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (collectorDeps, error)
	buildProjectorFn               func(context.Context, bootstrapDB, projector.CanonicalWriter, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (projectorDeps, error)
	discoveryAdvisorySink          func(collector.DiscoveryAdvisoryReport) error
)

const contentSearchIndexFinalizationTimeout = 15 * time.Minute

func main() {
	if handled, err := printBootstrapIndexVersionFlag(os.Args[1:], os.Stdout); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(
		context.Background(),
		os.Getenv,
		openBootstrapDB,
		applySchema,
		func(ctx context.Context, db bootstrapDB) error {
			beginner, ok := db.(postgres.Beginner)
			if !ok {
				return fmt.Errorf("bootstrap database does not support transactions")
			}
			return postgres.EnsureContentSearchIndexes(ctx, beginner)
		},
		ensureBootstrapGraphSchema,
		openBootstrapGraph,
		buildBootstrapCollector,
		buildBootstrapProjector,
	); err != nil {
		slog.Error("bootstrap-index failed", "error", err)
		os.Exit(1)
	}
}

func printBootstrapIndexVersionFlag(args []string, stdout io.Writer) (bool, error) {
	return buildinfo.PrintVersionFlag(args, stdout, "eshu-bootstrap-index")
}

func run(
	ctx context.Context,
	getenv func(string) string,
	openDBFn openBootstrapDBFn,
	schemaFn applyBootstrapFn,
	finalizeContentSearchIndexesFn finalizeContentSearchIndexesFn,
	graphSchemaFn ensureBootstrapGraphSchemaFn,
	graphFn openGraphFn,
	collectorFn buildCollectorFn,
	projectorFn buildProjectorFn,
) (err error) {
	// Initialize telemetry
	bootstrap, err := telemetry.NewBootstrap("bootstrap-index")
	if err != nil {
		return fmt.Errorf("telemetry bootstrap: %w", err)
	}
	providers, err := telemetry.NewProviders(ctx, bootstrap)
	if err != nil {
		return fmt.Errorf("telemetry providers: %w", err)
	}
	defer func() {
		_ = providers.Shutdown(context.Background())
	}()

	logger := telemetry.NewLogger(bootstrap, "collector", "bootstrap-index")
	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}

	memLimit := runtimecfg.ConfigureMemoryLimit(logger)
	if err := telemetry.RecordGOMEMLIMIT(meter, memLimit); err != nil {
		return fmt.Errorf("register gomemlimit gauge: %w", err)
	}
	logger.Info("starting bootstrap-index")

	pprofSrv, err := runtimecfg.NewPprofServer(getenv)
	if err != nil {
		return fmt.Errorf("pprof server: %w", err)
	}
	if pprofSrv != nil {
		if err := pprofSrv.Start(ctx); err != nil {
			return fmt.Errorf("pprof server start: %w", err)
		}
		logger.Info("pprof server listening", "addr", pprofSrv.Addr())
		defer func() {
			_ = pprofSrv.Stop(context.Background())
		}()
	}

	db, err := openDBFn(ctx, getenv)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	if err = schemaFn(ctx, db); err != nil {
		return err
	}
	if err = graphSchemaFn(ctx, db, getenv, logger); err != nil {
		return err
	}

	gd, err := graphFn(ctx, db, getenv, tracer, instruments)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := gd.close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	cd, err := collectorFn(ctx, db, getenv, tracer, instruments, logger)
	if err != nil {
		return err
	}

	// Build projector deps before starting collector so both can run concurrently.
	// The Postgres projector queue uses FOR UPDATE SKIP LOCKED, so concurrent
	// collection (producing queue items) and projection (claiming them) is safe.
	pd, err := projectorFn(ctx, db, gd.writer, getenv, tracer, instruments, logger)
	if err != nil {
		return err
	}

	workers := projectionWorkerCount(getenv)
	// Bound commit lanes by the shared Postgres pool AFTER the projection
	// worker count is known: every lane holds an open transaction on the
	// pool it shares with the concurrent projector (#5135 review). Log the
	// full derivation — requested, pool size, reserve, effective — so an
	// operator can see WHY the effective count differs from the request.
	requestedLanes := cd.commitLanes
	maxOpenConns := postgresMaxOpenConns(getenv)
	cd.commitLanes = effectiveCommitLanes(requestedLanes, maxOpenConns, workers)
	logger.Info(
		"starting pipelined bootstrap",
		slog.Int("projection_workers", workers),
		slog.Int("commit_lanes_requested", requestedLanes),
		slog.Int("commit_lanes", cd.commitLanes),
		slog.Int("postgres_max_open_conns", maxOpenConns),
		slog.Int("commit_lane_reserve", commitLaneReserve(workers)),
		telemetry.PhaseAttr(telemetry.PhaseEmission),
	)

	reportPath := strings.TrimSpace(getenv("ESHU_DISCOVERY_REPORT"))
	reports := make([]collector.DiscoveryAdvisoryReport, 0)
	var reportSink discoveryAdvisorySink
	if reportPath != "" {
		reportSink = func(report collector.DiscoveryAdvisoryReport) error {
			reports = append(reports, report)
			return nil
		}
	}

	pipelineErr := runPipelined(ctx, cd, pd, workers, tracer, instruments, logger, reportSink)
	if reportPath != "" {
		if writeErr := writeDiscoveryAdvisoryReports(reportPath, reports); writeErr != nil {
			return errors.Join(pipelineErr, writeErr)
		}
	}
	if pipelineErr != nil {
		return pipelineErr
	}

	finalizeStart := time.Now()
	logger.InfoContext(ctx, "content substring index finalization started", "index_state", "building")
	finalizeCtx, cancelFinalize := context.WithTimeout(ctx, contentSearchIndexFinalizationTimeout)
	defer cancelFinalize()
	if err := finalizeContentSearchIndexesFn(finalizeCtx, db); err != nil {
		logger.ErrorContext(
			ctx,
			"content substring index finalization failed",
			"index_state", "failed",
			"duration_seconds", recordContentSearchIndexFinalizationDuration(ctx, instruments, finalizeStart),
			telemetry.FailureClassAttr("content_substring_index_build_failure"),
			"error", err,
		)
		return err
	}
	logger.InfoContext(
		ctx,
		"content substring index finalization complete",
		"index_state", "ready",
		"duration_seconds", recordContentSearchIndexFinalizationDuration(ctx, instruments, finalizeStart),
	)
	return nil
}

func recordContentSearchIndexFinalizationDuration(
	ctx context.Context,
	instruments *telemetry.Instruments,
	start time.Time,
) float64 {
	duration := time.Since(start).Seconds()
	if instruments != nil {
		instruments.BootstrapPipelinePhaseDuration.Record(
			ctx,
			duration,
			metric.WithAttributes(
				telemetry.AttrBootstrapPhase(telemetry.BootstrapPhaseContentIndexFinalization),
				telemetry.AttrCollectorKind("bootstrap-index"),
			),
		)
	}
	return duration
}
