// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main //nolint:filelength // 1123 lines: four-phase bootstrap orchestrator. The phase ordering is a correctness invariant (see cmd/bootstrap-index/AGENTS.md § Phase-ordering invariant and CLAUDE.md § Facts-First Bootstrap Ordering). Tracked for split in audit § T8; the split must preserve the runPipelined call order.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/attribute"
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
	openBootstrapDBFn     func(context.Context, func(string) string) (bootstrapDB, error)
	applyBootstrapFn      func(context.Context, bootstrapDB) error
	openGraphFn           func(context.Context, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error)
	buildCollectorFn      func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (collectorDeps, error)
	buildProjectorFn      func(context.Context, bootstrapDB, projector.CanonicalWriter, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (projectorDeps, error)
	discoveryAdvisorySink func(collector.DiscoveryAdvisoryReport) error
)

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

	gd, err := graphFn(ctx, getenv, tracer, instruments)
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
	logger.Info(
		"starting pipelined bootstrap",
		slog.Int("projection_workers", workers),
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
	return pipelineErr
}

// runPipelined runs collection and projection concurrently. The collector is
// finite (drains all repos then exits). The projector polls the queue, processes
// items as they arrive, and exits after maxEmptyPolls consecutive empty claims
// once the collector has finished.
//
// This pipelining means small repos are fully projected (including Neo4j writes)
// while large repos are still being collected — instead of waiting for all 878
// repos to be collected before any projection begins.
func runPipelined(
	ctx context.Context,
	cd collectorDeps,
	pd projectorDeps,
	workers int,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	advisorySinks ...discoveryAdvisorySink,
) error {
	pipelineStart := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// collectorDone signals that the collector has finished producing queue items.
	// The projector uses this to switch from infinite polling to drain mode.
	collectorDone := make(chan struct{})

	errc := make(chan error, 2)

	// recordPhaseDuration records one named bootstrap pipeline phase duration as
	// both a metric (BootstrapPipelinePhaseDuration) and a structured log line so
	// operators can read the long pole from logs or the metrics port without
	// strace (#3678). The collector_kind label aligns with the per-collector
	// telemetry convention so the two layers join cleanly.
	recordPhaseDuration := func(phase string, d float64) {
		if instruments != nil {
			instruments.BootstrapPipelinePhaseDuration.Record(ctx, d, metric.WithAttributes(
				telemetry.AttrBootstrapPhase(phase),
				telemetry.AttrCollectorKind("bootstrap-index"),
			))
		}
		if logger != nil {
			logger.InfoContext(
				ctx, "bootstrap phase complete",
				slog.String("bootstrap_phase", phase),
				slog.Float64("phase_duration_seconds", d),
				telemetry.PhaseAttr(telemetry.PhaseEmission),
			)
		}
	}
	// recordPhase records a phase that ends now (sequential post-collection work).
	recordPhase := func(phase string, start time.Time) {
		recordPhaseDuration(phase, time.Since(start).Seconds())
	}
	// recordPhaseAt records a phase whose end is a captured timestamp rather than
	// "now". Required for the concurrent projection phase: the projector runs in
	// parallel with collection and backfill, so its true wall time is
	// (projector completion - projectionStart), not time.Since at the point where
	// runPipelined happens to receive the projector's result. Using time.Since
	// here would wrongly fold the backfill wait into the projection duration and
	// defeat the long-pole diagnostic.
	recordPhaseAt := func(phase string, start, end time.Time) {
		recordPhaseDuration(phase, end.Sub(start).Seconds())
	}

	// projectorCompletedAt carries the projector goroutine's actual completion
	// timestamp out to the projection-phase recorder, independent of when
	// runPipelined receives the projector's error from errc.
	projectorCompletedAt := make(chan time.Time, 1)

	// Start collector goroutine
	collectionStart := time.Now()
	go func() {
		defer close(collectorDone)
		err := drainCollector(ctx, cd.source, cd.committer, tracer, instruments, logger, firstDiscoveryAdvisorySink(advisorySinks))
		errc <- err
	}()

	// Start projector goroutine — polls for work, projects concurrently.
	// After collector signals done, drains remaining queue then exits.
	projectionStart := time.Now()
	go func() {
		err := drainProjectorPipelined(ctx, pd, workers, collectorDone, tracer, instruments, logger)
		// Capture the projector's true completion time before publishing the
		// error, so the projection phase duration excludes any later backfill wait.
		projectorCompletedAt <- time.Now()
		errc <- err
	}()

	// Wait for collector to finish first.
	collectorErr := <-errc

	overlapDuration := time.Since(pipelineStart).Seconds()
	recordPhase(telemetry.BootstrapPhaseCollection, collectionStart)

	if collectorErr != nil {
		// Collector failed — cancel projector and drain.
		cancel()
		projectorErr := <-errc
		return errors.Join(collectorErr, projectorErr)
	}

	// Each post-collection phase records its duration even on the error path:
	// the entire point of #3678 is to see WHICH phase is the long pole, and a
	// failing long-pole phase is exactly the case an operator must diagnose. So
	// recordPhase is called before every early return, not only on success.
	backfillStart := time.Now()
	if err := cd.committer.BackfillAllRelationshipEvidence(ctx, tracer, instruments); err != nil {
		recordPhase(telemetry.BootstrapPhaseRelationshipBackfill, backfillStart)
		if logger != nil {
			logger.ErrorContext(
				ctx, "deferred relationship backfill failed",
				slog.String("error", err.Error()),
				telemetry.FailureClassAttr("backfill_deferred_failure"),
			)
		}
		cancel()
		projectorErr := <-errc
		return fmt.Errorf("deferred backfill fatal: %w", errors.Join(err, projectorErr))
	}
	recordPhase(telemetry.BootstrapPhaseRelationshipBackfill, backfillStart)

	// Wait for the source-local projector to drain before reopening reducer work.
	// Otherwise deployment_mapping items emitted after the reopen pass starts
	// could miss reopening and remain soft-gated.
	projectorErr := <-errc
	// Record projection wall time from its own start to its own completion. The
	// projector ran concurrently with collection and the backfill above, so its
	// completion timestamp (captured inside the goroutine) is the only accurate
	// end point; time.Since here would include the backfill wait.
	recordPhaseAt(telemetry.BootstrapPhaseProjection, projectionStart, <-projectorCompletedAt)
	if projectorErr != nil {
		return projectorErr
	}

	iacStart := time.Now()
	if err := cd.committer.MaterializeIaCReachability(ctx, tracer, instruments); err != nil {
		recordPhase(telemetry.BootstrapPhaseIaCReachability, iacStart)
		if logger != nil {
			logger.ErrorContext(
				ctx, "iac reachability materialization failed",
				slog.String("error", err.Error()),
				telemetry.FailureClassAttr("iac_reachability_materialization_failure"),
			)
		}
		return fmt.Errorf("iac reachability materialization fatal: %w", err)
	}
	recordPhase(telemetry.BootstrapPhaseIaCReachability, iacStart)

	// Reopen only the deployment_mapping items that already succeeded with the
	// cross-repo readiness gate closed. Items still pending or claimed will
	// naturally see the gate open when they run (backward_evidence is already
	// committed by BackfillAllRelationshipEvidence above). A small number of
	// in-flight items may succeed between now and the reopen pass — those
	// stragglers are NOT automatically replayed today and require manual admin
	// replay or a future automated straggler-replay mechanism.
	//
	// This step gets its own bounded phase so it is independently identifiable as
	// a potential long pole; without it the reopen time would be an unaccounted
	// gap between iac_reachability and config_state_drift.
	reopenStart := time.Now()
	if err := cd.committer.ReopenDeploymentMappingWorkItems(ctx, tracer, instruments); err != nil {
		recordPhase(telemetry.BootstrapPhaseDeploymentReopen, reopenStart)
		if logger != nil {
			logger.ErrorContext(
				ctx, "reopen deployment_mapping work items failed",
				slog.String("error", err.Error()),
				telemetry.FailureClassAttr("reopen_deployment_mapping_failure"),
			)
		}
		return fmt.Errorf("reopen deployment_mapping fatal: %w", err)
	}
	recordPhase(telemetry.BootstrapPhaseDeploymentReopen, reopenStart)

	// Reopen succeeded code_import_repo_edge work items for the same after-the-fact
	// reason: the code-import projection resolves owners through the cross-scope
	// package-registry owner index, which may have been empty when the projection
	// first ran (e.g. a re-run after package-registry facts land). Replaying it
	// lets cross-repo DEPENDS_ON edges form once that ownership evidence exists.
	codeImportReopenStart := time.Now()
	if err := cd.committer.ReopenCodeImportRepoEdgeWorkItems(ctx, tracer, instruments); err != nil {
		recordPhase(telemetry.BootstrapPhaseCodeImportReopen, codeImportReopenStart)
		if logger != nil {
			logger.ErrorContext(
				ctx, "reopen code_import_repo_edge work items failed",
				slog.String("error", err.Error()),
				telemetry.FailureClassAttr("reopen_code_import_repo_edge_failure"),
			)
		}
		return fmt.Errorf("reopen code_import_repo_edge fatal: %w", err)
	}
	recordPhase("code_import_repo_edge_reopen", codeImportReopenStart)

	// Replay additive correlation domains that consume resolved relationships
	// produced by the deployment_mapping reopen above. deployable_unit_correlation
	// reads resolved DEPLOYS_FROM and has no readiness retry, so on the first
	// maintenance pass (before resolution commits) it correlates nothing; a later
	// maintenance pass — the ingester loops; the gate runs maintenance twice —
	// replays it once resolution exists. Idempotent.
	// kubernetes_correlation_materialization writes RUNS_IMAGE edges by joining a
	// live workload's image digest to the cross-scope active OCI manifest facts.
	// On the first pass the OCI registry scope's generation may not be active yet,
	// so the digest resolves nothing and the edge succeeds with zero edges; a later
	// maintenance pass replays it once the OCI generation is active. Idempotent.
	// The kubernetes_workload_materialization node domain is intentionally NOT
	// reopened here: it consumes only in-scope pod-template facts and commits on
	// the normal drain, so it has no cross-scope readiness dependency to replay.
	correlationReopenStart := time.Now()
	if err := cd.committer.ReopenSucceededReducerWorkItems(ctx, tracer, instruments, []string{
		"deployable_unit_correlation",            // reducer.DomainDeployableUnitCorrelation
		"kubernetes_correlation_materialization", // reducer.DomainKubernetesCorrelationMaterialization
	}); err != nil {
		recordPhase("correlation_reopen", correlationReopenStart)
		if logger != nil {
			logger.ErrorContext(
				ctx, "reopen correlation work items failed",
				slog.String("error", err.Error()),
				telemetry.FailureClassAttr("reopen_correlation_failure"),
			)
		}
		return fmt.Errorf("reopen correlation work items fatal: %w", err)
	}
	recordPhase("correlation_reopen", correlationReopenStart)

	// Phase 3.5: enqueue config_state_drift intents for every state_snapshot
	// scope that has an active generation. The drift handler consumes both
	// config-side parser facts and state-side collector facts, so its work
	// items must land after Phase 3 reopens deployment_mapping (the same
	// facts-first ordering rationale documented in CLAUDE.md). Idempotent.
	driftStart := time.Now()
	if err := cd.committer.EnqueueConfigStateDriftIntents(ctx, tracer, instruments); err != nil {
		recordPhase(telemetry.BootstrapPhaseConfigStateDrift, driftStart)
		if logger != nil {
			logger.ErrorContext(
				ctx, "enqueue config_state_drift intents failed",
				slog.String("error", err.Error()),
				telemetry.FailureClassAttr("enqueue_config_state_drift_failure"),
			)
		}
		return fmt.Errorf("enqueue config_state_drift fatal: %w", err)
	}
	recordPhase(telemetry.BootstrapPhaseConfigStateDrift, driftStart)

	totalDuration := time.Since(pipelineStart).Seconds()
	if logger != nil {
		logger.InfoContext(
			ctx, "bootstrap pipeline complete",
			slog.Float64("total_seconds", totalDuration),
			slog.Float64("overlap_seconds", overlapDuration),
			telemetry.PhaseAttr(telemetry.PhaseProjection),
		)
	}
	if instruments != nil {
		instruments.PipelineOverlapDuration.Record(ctx, overlapDuration)
	}

	return projectorErr
}

// drainProjectorPipelined wraps drainProjector with drain-then-exit behavior.
// While the collector is running, empty queue claims trigger a short poll wait.
// After the collector finishes (collectorDone is closed), the projector enters
// drain mode: maxEmptyPolls consecutive empty claims cause a clean exit.
func drainProjectorPipelined(
	ctx context.Context,
	pd projectorDeps,
	workers int,
	collectorDone <-chan struct{},
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	const maxEmptyPolls = 5
	const pollInterval = 500 * time.Millisecond

	// Use a draining work source wrapper that counts consecutive empty polls
	// and exits cleanly when the collector is done and queue is drained.
	dws := &drainingWorkSource{
		inner:         pd.workSource,
		collectorDone: collectorDone,
		maxEmptyPolls: maxEmptyPolls,
		pollInterval:  pollInterval,
	}

	return drainProjector(ctx, dws, pd.factStore, pd.runner, pd.workSink, pd.heartbeater, pd.heartbeatInterval, workers, tracer, instruments, logger)
}

// drainingWorkSource wraps a ProjectorWorkSource to add drain-then-exit
// behavior for pipelined bootstrap. Before the collector finishes, empty
// claims trigger a poll wait and retry. After the collector finishes,
// consecutive empty claims are counted and the sentinel errProjectorDrained
// triggers exit after maxEmptyPolls.
type drainingWorkSource struct {
	inner         projector.ProjectorWorkSource
	collectorDone <-chan struct{}
	maxEmptyPolls int
	pollInterval  time.Duration
	emptyCount    atomic.Int32
}

func (d *drainingWorkSource) Claim(ctx context.Context) (projector.ScopeGenerationWork, bool, error) {
	for {
		work, ok, err := d.inner.Claim(ctx)
		if err != nil {
			return work, ok, err
		}
		if ok {
			d.emptyCount.Store(0)
			return work, true, nil
		}

		// Queue is empty. Check if collector is done.
		select {
		case <-d.collectorDone:
			// Collector finished — count consecutive empty polls.
			n := int(d.emptyCount.Add(1))
			if n >= d.maxEmptyPolls {
				return projector.ScopeGenerationWork{}, false, nil
			}
		default:
			// Collector still running — wait and retry.
			d.emptyCount.Store(0)
		}

		// Wait before retrying.
		select {
		case <-ctx.Done():
			return projector.ScopeGenerationWork{}, false, ctx.Err()
		case <-time.After(d.pollInterval):
		}
	}
}

// projectionWorkerCount returns the number of concurrent projection workers.
// Reads ESHU_PROJECTION_WORKERS from env; defaults to NumCPU capped at 8.
func projectionWorkerCount(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_PROJECTION_WORKERS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	n := runtime.NumCPU()
	if n > 8 {
		n = 8
	}
	if n < 1 {
		n = 1
	}
	return n
}

// bootstrapProgressInterval is the number of repos between periodic progress
// log lines during collection. Low enough to be useful; high enough to avoid
// log noise on very large corpora.
const bootstrapProgressInterval = 10

// drainCollector runs the collector source until no more work is available.
// Each cycle is wrapped in a collector.observe span with metric and log output
// so operators can trace collection throughput during bootstrap.
//
// Per-repo instrumentation added by #3678:
//   - eshu_dp_content_entity_emitted_total (source_file_kind, collector_kind):
//     incremented per entity by bounded file kind so lockfile/config explosions
//     are visible from the metrics port without manual SQL.
//   - Periodic progress log every bootstrapProgressInterval repos (repos done,
//     elapsed, facts emitted) so a 70-min run produces visible progress in logs.
//   - Per-repo content_entity breakdown in the "bootstrap scope collected" log
//     line (content_entity_count, entity_by_source_file_kind).
func drainCollector(
	ctx context.Context,
	source collector.Source,
	committer collector.Committer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	advisorySinks ...discoveryAdvisorySink,
) error {
	var (
		total           int
		totalFacts      int64
		totalEntities   int64
		collectionStart = time.Now()
	)
	advisorySink := firstDiscoveryAdvisorySink(advisorySinks)
	for {
		cycleStart := time.Now()

		var span trace.Span
		cycleCtx := ctx
		if tracer != nil {
			cycleCtx, span = tracer.Start(
				ctx, telemetry.SpanCollectorObserve,
				trace.WithAttributes(attribute.String("component", "bootstrap-index")),
			)
		}

		collected, ok, err := source.Next(cycleCtx)
		if err != nil {
			if span != nil {
				span.RecordError(err)
				span.End()
			}
			return fmt.Errorf("bootstrap collector: %w", err)
		}
		if !ok {
			if span != nil {
				span.End()
			}
			if logger != nil {
				logger.InfoContext(
					ctx, "bootstrap collection complete",
					slog.Int("scopes_collected", total),
					slog.Int64("total_facts_emitted", totalFacts),
					slog.Int64("total_entities_emitted", totalEntities),
					slog.Float64("collection_duration_seconds", time.Since(collectionStart).Seconds()),
					telemetry.PhaseAttr(telemetry.PhaseEmission),
				)
			}
			return nil
		}

		factCount := collected.FactCount
		if instruments != nil {
			instruments.FactsEmitted.Add(cycleCtx, int64(factCount), metric.WithAttributes(
				telemetry.AttrScopeID(collected.Scope.ScopeID),
				telemetry.AttrCollectorKind("bootstrap-index"),
			))
		}

		if err := committer.CommitScopeGeneration(
			cycleCtx,
			collected.Scope,
			collected.Generation,
			collected.Facts,
		); err != nil {
			if span != nil {
				span.RecordError(err)
				span.End()
			}
			if logger != nil {
				logger.ErrorContext(
					ctx, "bootstrap collector commit failed",
					slog.String("scope_id", collected.Scope.ScopeID),
					slog.Int("fact_count", factCount),
					slog.String("error", err.Error()),
					telemetry.PhaseAttr(telemetry.PhaseEmission),
					telemetry.FailureClassAttr("commit_failure"),
				)
			}
			return fmt.Errorf("bootstrap collector commit: %w", err)
		}

		// Emit per-file-kind content_entity counters from the discovery advisory.
		// The advisory classifies each entity into a bounded source_file_kind
		// (telemetry.ContentEntitySourceFileKind: code, package_manifest, config,
		// other) — package_manifest comes from dependency entity metadata, the same
		// signal the reducer admits. Iterate the bounded constant set (not the map
		// keys) so both the metric label space and the log field space are
		// statically bounded and a stray advisory key can never leak a new
		// dimension. These counters let operators distinguish a lockfile explosion
		// (package_manifest) from normal code growth without querying fact_records.
		var entityCount int
		entityByKind := map[string]int{}
		if collected.DiscoveryAdvisory != nil {
			for _, kind := range telemetry.SourceFileKinds() {
				n := collected.DiscoveryAdvisory.EntityCounts.BySourceFileKind[kind]
				entityByKind[kind] = n
				entityCount += n
				if instruments != nil && n > 0 {
					instruments.ContentEntityEmitted.Add(cycleCtx, int64(n), metric.WithAttributes(
						telemetry.AttrSourceFileKind(kind),
						telemetry.AttrCollectorKind("bootstrap-index"),
					))
				}
			}
		}
		if collected.DiscoveryAdvisory != nil && advisorySink != nil {
			report := *collected.DiscoveryAdvisory
			if report.Run.ScopeID == "" {
				report.Run.ScopeID = collected.Scope.ScopeID
			}
			if report.Run.GenerationID == "" {
				report.Run.GenerationID = collected.Generation.GenerationID
			}
			if err := advisorySink(report); err != nil {
				return fmt.Errorf("record discovery advisory: %w", err)
			}
		}

		duration := time.Since(cycleStart).Seconds()
		if instruments != nil {
			instruments.FactsCommitted.Add(cycleCtx, int64(factCount), metric.WithAttributes(
				telemetry.AttrScopeID(collected.Scope.ScopeID),
			))
			instruments.CollectorObserveDuration.Record(cycleCtx, duration, metric.WithAttributes(
				telemetry.AttrCollectorKind("bootstrap-index"),
			))
		}

		totalFacts += int64(factCount)
		totalEntities += int64(entityCount)
		total++

		if logger != nil {
			// Per-repo log: include content_entity count and per-file-kind breakdown
			// so log grep surfaces the noisy sources without DB queries.
			logAttrs := []any{
				slog.String("scope_id", collected.Scope.ScopeID),
				slog.Int("fact_count", factCount),
				slog.Int("content_entity_count", entityCount),
				slog.Float64("duration_seconds", duration),
				telemetry.PhaseAttr(telemetry.PhaseEmission),
			}
			// Iterate the bounded constant set so the log field set is static and
			// ordered (entity_kind_code, entity_kind_package_manifest, ...).
			for _, kind := range telemetry.SourceFileKinds() {
				logAttrs = append(logAttrs, slog.Int("entity_kind_"+kind, entityByKind[kind]))
			}
			logger.InfoContext(cycleCtx, "bootstrap scope collected", logAttrs...)

			// Periodic progress: every bootstrapProgressInterval repos emit a
			// summary so a 70-min run does not look silent.
			if total%bootstrapProgressInterval == 0 {
				logger.InfoContext(
					ctx, "bootstrap collection progress",
					slog.Int("scopes_done", total),
					slog.Int64("total_facts_emitted", totalFacts),
					slog.Int64("total_entities_emitted", totalEntities),
					slog.Float64("elapsed_seconds", time.Since(collectionStart).Seconds()),
					telemetry.PhaseAttr(telemetry.PhaseEmission),
				)
			}
		}
		if span != nil {
			span.SetAttributes(
				attribute.String("scope_id", collected.Scope.ScopeID),
				attribute.Int("fact_count", factCount),
				attribute.Int("content_entity_count", entityCount),
			)
			span.End()
		}
	}
}

func firstDiscoveryAdvisorySink(sinks []discoveryAdvisorySink) discoveryAdvisorySink {
	for _, sink := range sinks {
		if sink != nil {
			return sink
		}
	}
	return nil
}

func writeDiscoveryAdvisoryReports(path string, reports []collector.DiscoveryAdvisoryReport) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create discovery advisory report directory: %w", err)
	}
	contents, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal discovery advisory report: %w", err)
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		return fmt.Errorf("write discovery advisory report %q: %w", path, err)
	}
	return nil
}

// drainProjector runs projection workers concurrently. Each worker claims
// work items from the queue, loads facts, projects, and acks independently.
//
// Concurrency model:
//   - N goroutine workers compete for work via workSource.Claim (Postgres
//     SELECT ... FOR UPDATE SKIP LOCKED ensures exactly-once delivery).
//   - On first error any worker cancels the shared context so siblings drain
//     promptly; all errors are collected and returned via errors.Join.
//   - An atomic counter tracks completed items for structured log output.
//
// Tuning: set ESHU_PROJECTION_WORKERS to control parallelism. Default is
// min(NumCPU, 8). Monitor eshu_dp_projector_run_duration_seconds and
// eshu_dp_queue_claim_duration_seconds{queue=projector} to identify whether
// the bottleneck is CPU (increase workers) or I/O (tune Postgres connections).
func drainProjector(
	ctx context.Context,
	workSource projector.ProjectorWorkSource,
	factStore projector.FactStore,
	runner projector.ProjectionRunner,
	workSink projector.ProjectorWorkSink,
	heartbeater projector.ProjectorWorkHeartbeater,
	heartbeatInterval time.Duration,
	workers int,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	if workers <= 1 {
		return drainProjectorSequential(ctx, workSource, factStore, runner, workSink, heartbeater, heartbeatInterval, tracer, instruments, logger)
	}

	overallStart := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu        sync.Mutex
		errs      []error
		wg        sync.WaitGroup
		completed atomic.Int64
	)

	for i := 0; i < workers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}

				if err := drainProjectorWorkItem(
					ctx, workSource, factStore, runner, workSink,
					heartbeater, heartbeatInterval,
					workerID, &completed, tracer, instruments, logger,
				); err != nil {
					if errors.Is(err, errProjectorDrained) {
						return
					}
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
					cancel()
					return
				}
			}
		}()
	}

	wg.Wait()

	totalCompleted := completed.Load()
	if logger != nil {
		logger.InfoContext(
			ctx, "bootstrap projection complete",
			slog.Int64("items_projected", totalCompleted),
			slog.Int("workers", workers),
			slog.Float64("total_duration_seconds", time.Since(overallStart).Seconds()),
			telemetry.PhaseAttr(telemetry.PhaseProjection),
		)
	}
	return errors.Join(errs...)
}

// errProjectorDrained is a sentinel indicating the work queue is empty.
var errProjectorDrained = errors.New("projector queue drained")

// drainProjectorWorkItem processes a single projection work item with full
// OTEL tracing, metric recording, and structured logging.
func drainProjectorWorkItem(
	ctx context.Context,
	workSource projector.ProjectorWorkSource,
	factStore projector.FactStore,
	runner projector.ProjectionRunner,
	workSink projector.ProjectorWorkSink,
	heartbeater projector.ProjectorWorkHeartbeater,
	heartbeatInterval time.Duration,
	workerID int,
	completed *atomic.Int64,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	// Claim
	claimStart := time.Now()
	work, ok, err := workSource.Claim(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap projector claim (worker %d): %w", workerID, err)
	}
	if instruments != nil {
		instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
			attribute.String("queue", "projector"),
		))
	}
	if !ok {
		return errProjectorDrained
	}

	// Start span for the full project cycle
	itemStart := time.Now()
	itemCtx := ctx
	var span trace.Span
	if tracer != nil {
		itemCtx, span = tracer.Start(
			ctx, telemetry.SpanProjectorRun,
			trace.WithAttributes(
				attribute.String("scope_id", work.Scope.ScopeID),
				attribute.String("generation_id", work.Generation.GenerationID),
				attribute.Int("worker_id", workerID),
			),
		)
	}

	heartbeatCtx, stopHeartbeat := startBootstrapProjectorHeartbeat(
		itemCtx,
		work,
		heartbeater,
		heartbeatInterval,
		workerID,
		logger,
	)
	defer func() {
		_ = stopHeartbeat()
	}()

	// Load facts
	factsForGeneration, loadErr := factStore.LoadFacts(heartbeatCtx, work)
	if loadErr != nil {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			if errors.Is(heartbeatErr, projector.ErrWorkSuperseded) {
				recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "superseded", 0, nil, span, instruments, logger)
				return nil
			}
			loadErr = errors.Join(loadErr, heartbeatErr)
		}
		recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "failed", 0, loadErr, span, instruments, logger)
		return fmt.Errorf("bootstrap projector load facts (worker %d): %w", workerID, loadErr)
	}

	// Project
	result, projectErr := runner.Project(heartbeatCtx, work.Scope, work.Generation, factsForGeneration)
	if projectErr != nil {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			if errors.Is(heartbeatErr, projector.ErrWorkSuperseded) {
				recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "superseded", len(factsForGeneration), nil, span, instruments, logger)
				return nil
			}
			projectErr = errors.Join(projectErr, heartbeatErr)
		}
		recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "failed", len(factsForGeneration), projectErr, span, instruments, logger)
		return fmt.Errorf("bootstrap projector project (worker %d): %w", workerID, projectErr)
	}
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		if errors.Is(heartbeatErr, projector.ErrWorkSuperseded) {
			recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "superseded", len(factsForGeneration), nil, span, instruments, logger)
			return nil
		}
		recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "failed", len(factsForGeneration), heartbeatErr, span, instruments, logger)
		return fmt.Errorf("bootstrap projector heartbeat (worker %d): %w", workerID, heartbeatErr)
	}

	// Ack
	if ackErr := workSink.Ack(itemCtx, work, result); ackErr != nil {
		recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "failed", len(factsForGeneration), ackErr, span, instruments, logger)
		return fmt.Errorf("bootstrap projector ack (worker %d): %w", workerID, ackErr)
	}

	completed.Add(1)
	recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "succeeded", len(factsForGeneration), nil, span, instruments, logger)
	return nil
}

type bootstrapProjectorHeartbeatStop func() error

// startBootstrapProjectorHeartbeat renews bootstrap projector leases during
// long source-local graph writes so a still-running worker is not re-claimed.
func startBootstrapProjectorHeartbeat(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	heartbeater projector.ProjectorWorkHeartbeater,
	interval time.Duration,
	workerID int,
	logger *slog.Logger,
) (context.Context, bootstrapProjectorHeartbeatStop) {
	if heartbeater == nil || interval <= 0 {
		return ctx, func() error { return nil }
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var heartbeatErr error
		for {
			select {
			case <-heartbeatCtx.Done():
				done <- heartbeatErr
				return
			case <-ticker.C:
				if err := heartbeater.Heartbeat(heartbeatCtx, work); err != nil {
					if heartbeatCtx.Err() != nil && errors.Is(err, heartbeatCtx.Err()) {
						done <- nil
						return
					}
					heartbeatErr = fmt.Errorf("heartbeat bootstrap projector work: %w", err)
					if logger != nil {
						scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
						logAttrs := make([]any, 0, len(scopeAttrs)+5)
						for _, attr := range scopeAttrs {
							logAttrs = append(logAttrs, attr)
						}
						logAttrs = append(
							logAttrs,
							slog.Int("worker_id", workerID),
							slog.Duration("heartbeat_interval", interval),
							telemetry.PhaseAttr(telemetry.PhaseProjection),
							telemetry.FailureClassAttr("lease_heartbeat_failure"),
							slog.String("error", heartbeatErr.Error()),
						)
						logger.ErrorContext(heartbeatCtx, "bootstrap projector lease heartbeat failed", logAttrs...)
					}
					cancel()
				}
			}
		}
	}()

	var once sync.Once
	return heartbeatCtx, func() error {
		var heartbeatErr error
		once.Do(func() {
			cancel()
			heartbeatErr = <-done
		})
		return heartbeatErr
	}
}

// recordBootstrapProjectionResult records metrics and logs for a single
// projection work item, matching the pattern in projector.Service.
func recordBootstrapProjectionResult(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	workerID int,
	start time.Time,
	status string,
	factCount int,
	err error,
	span trace.Span,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) {
	duration := time.Since(start).Seconds()

	if instruments != nil {
		instruments.ProjectorRunDuration.Record(ctx, duration, metric.WithAttributes(
			telemetry.AttrScopeID(work.Scope.ScopeID),
		))
		instruments.ProjectionsCompleted.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrScopeID(work.Scope.ScopeID),
			attribute.String("status", status),
		))
	}

	if span != nil {
		span.SetAttributes(
			attribute.Int("fact_count", factCount),
			attribute.String("status", status),
		)
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}

	if logger != nil {
		scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
		logAttrs := make([]any, 0, len(scopeAttrs)+5)
		for _, a := range scopeAttrs {
			logAttrs = append(logAttrs, a)
		}
		logAttrs = append(
			logAttrs,
			slog.Int("worker_id", workerID),
			slog.String("status", status),
			slog.Int("fact_count", factCount),
			slog.Float64("duration_seconds", duration),
			telemetry.PhaseAttr(telemetry.PhaseProjection),
		)
		if err != nil {
			logAttrs = append(logAttrs, slog.String("error", err.Error()))
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("projection_failure"))
			logger.ErrorContext(ctx, "bootstrap projection failed", logAttrs...)
			return
		}
		message := "bootstrap projection succeeded"
		if status == "superseded" {
			message = "bootstrap projection superseded by newer generation"
		}
		logger.InfoContext(ctx, message, logAttrs...)
	}
}

// drainProjectorSequential is the single-worker fallback. It uses the same
// per-item instrumentation as the concurrent path for consistent telemetry.
func drainProjectorSequential(
	ctx context.Context,
	workSource projector.ProjectorWorkSource,
	factStore projector.FactStore,
	runner projector.ProjectionRunner,
	workSink projector.ProjectorWorkSink,
	heartbeater projector.ProjectorWorkHeartbeater,
	heartbeatInterval time.Duration,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	var completed atomic.Int64
	overallStart := time.Now()
	for {
		err := drainProjectorWorkItem(
			ctx, workSource, factStore, runner, workSink,
			heartbeater, heartbeatInterval,
			0, &completed, tracer, instruments, logger,
		)
		if err != nil {
			if errors.Is(err, errProjectorDrained) {
				if logger != nil {
					logger.InfoContext(
						ctx, "bootstrap projection complete",
						slog.Int64("items_projected", completed.Load()),
						slog.Int("workers", 1),
						slog.Float64("total_duration_seconds", time.Since(overallStart).Seconds()),
						telemetry.PhaseAttr(telemetry.PhaseProjection),
					)
				}
				return nil
			}
			return err
		}
	}
}

// bootstrapSQLDB wraps a *sql.DB so it satisfies both bootstrapDB (Close) and
// postgres.ExecQueryer (QueryContext returns postgres.Rows, not *sql.Rows).
type bootstrapSQLDB struct {
	postgres.SQLDB
	raw *sql.DB
}

func (b *bootstrapSQLDB) Close() error { return b.raw.Close() }

func openBootstrapDB(ctx context.Context, getenv func(string) string) (bootstrapDB, error) {
	db, err := runtimecfg.OpenPostgres(ctx, getenv)
	if err != nil {
		return nil, err
	}
	return &bootstrapSQLDB{SQLDB: postgres.SQLDB{DB: db}, raw: db}, nil
}

func applySchema(ctx context.Context, db bootstrapDB) error {
	return postgres.ApplyBootstrap(ctx, db)
}

func openBootstrapGraph(ctx context.Context, getenv func(string) string, tracer trace.Tracer, instruments *telemetry.Instruments) (graphDeps, error) {
	writer, closer, err := openBootstrapCanonicalWriter(ctx, getenv, tracer, instruments)
	if err != nil {
		return graphDeps{}, err
	}
	return graphDeps{writer: writer, close: closer.Close}, nil
}
