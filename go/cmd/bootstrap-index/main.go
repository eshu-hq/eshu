package main

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

type openBootstrapDBFn func(context.Context, func(string) string) (bootstrapDB, error)
type applyBootstrapFn func(context.Context, bootstrapDB) error
type openGraphFn func(context.Context, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error)
type buildCollectorFn func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (collectorDeps, error)
type buildProjectorFn func(context.Context, bootstrapDB, projector.CanonicalWriter, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (projectorDeps, error)
type discoveryAdvisorySink func(collector.DiscoveryAdvisoryReport) error

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
	logger.Info("starting pipelined bootstrap",
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

	// Start collector goroutine
	go func() {
		defer close(collectorDone)
		err := drainCollector(ctx, cd.source, cd.committer, tracer, instruments, logger, firstDiscoveryAdvisorySink(advisorySinks))
		errc <- err
	}()

	// Start projector goroutine — polls for work, projects concurrently.
	// After collector signals done, drains remaining queue then exits.
	go func() {
		err := drainProjectorPipelined(ctx, pd, workers, collectorDone, tracer, instruments, logger)
		errc <- err
	}()

	// Wait for collector to finish first.
	collectorErr := <-errc

	overlapDuration := time.Since(pipelineStart).Seconds()

	if collectorErr != nil {
		// Collector failed — cancel projector and drain.
		cancel()
		projectorErr := <-errc
		return errors.Join(collectorErr, projectorErr)
	}

	if err := cd.committer.BackfillAllRelationshipEvidence(ctx, tracer, instruments); err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, "deferred relationship backfill failed",
				slog.String("error", err.Error()),
				telemetry.FailureClassAttr("backfill_deferred_failure"),
			)
		}
		cancel()
		projectorErr := <-errc
		return fmt.Errorf("deferred backfill fatal: %w", errors.Join(err, projectorErr))
	}

	// Wait for the source-local projector to drain before reopening reducer work.
	// Otherwise deployment_mapping items emitted after the reopen pass starts
	// could miss reopening and remain soft-gated.
	projectorErr := <-errc
	if projectorErr != nil {
		return projectorErr
	}

	if err := cd.committer.MaterializeIaCReachability(ctx, tracer, instruments); err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, "iac reachability materialization failed",
				slog.String("error", err.Error()),
				telemetry.FailureClassAttr("iac_reachability_materialization_failure"),
			)
		}
		return fmt.Errorf("iac reachability materialization fatal: %w", err)
	}

	// Reopen only the deployment_mapping items that already succeeded with the
	// cross-repo readiness gate closed. Items still pending or claimed will
	// naturally see the gate open when they run (backward_evidence is already
	// committed by BackfillAllRelationshipEvidence above). A small number of
	// in-flight items may succeed between now and the reopen pass — those
	// stragglers are NOT automatically replayed today and require manual admin
	// replay or a future automated straggler-replay mechanism.
	if err := cd.committer.ReopenDeploymentMappingWorkItems(ctx, tracer, instruments); err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, "reopen deployment_mapping work items failed",
				slog.String("error", err.Error()),
				telemetry.FailureClassAttr("reopen_deployment_mapping_failure"),
			)
		}
		return fmt.Errorf("reopen deployment_mapping fatal: %w", err)
	}

	// Phase 3.5: enqueue config_state_drift intents for every state_snapshot
	// scope that has an active generation. The drift handler consumes both
	// config-side parser facts and state-side collector facts, so its work
	// items must land after Phase 3 reopens deployment_mapping (the same
	// facts-first ordering rationale documented in CLAUDE.md). Idempotent.
	if err := cd.committer.EnqueueConfigStateDriftIntents(ctx, tracer, instruments); err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, "enqueue config_state_drift intents failed",
				slog.String("error", err.Error()),
				telemetry.FailureClassAttr("enqueue_config_state_drift_failure"),
			)
		}
		return fmt.Errorf("enqueue config_state_drift fatal: %w", err)
	}

	totalDuration := time.Since(pipelineStart).Seconds()
	if logger != nil {
		logger.InfoContext(ctx, "bootstrap pipeline complete",
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

// drainCollector runs the collector source until no more work is available.
// Each cycle is wrapped in a collector.observe span with metric and log output
// so operators can trace collection throughput during bootstrap.
func drainCollector(
	ctx context.Context,
	source collector.Source,
	committer collector.Committer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	advisorySinks ...discoveryAdvisorySink,
) error {
	var total int
	advisorySink := firstDiscoveryAdvisorySink(advisorySinks)
	for {
		cycleStart := time.Now()

		var span trace.Span
		cycleCtx := ctx
		if tracer != nil {
			cycleCtx, span = tracer.Start(ctx, telemetry.SpanCollectorObserve,
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
				logger.InfoContext(ctx, "bootstrap collection complete",
					slog.Int("scopes_collected", total),
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
				logger.ErrorContext(ctx, "bootstrap collector commit failed",
					slog.String("scope_id", collected.Scope.ScopeID),
					slog.Int("fact_count", factCount),
					slog.String("error", err.Error()),
					telemetry.PhaseAttr(telemetry.PhaseEmission),
					telemetry.FailureClassAttr("commit_failure"),
				)
			}
			return fmt.Errorf("bootstrap collector commit: %w", err)
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
		if logger != nil {
			logger.InfoContext(cycleCtx, "bootstrap scope collected",
				slog.String("scope_id", collected.Scope.ScopeID),
				slog.Int("fact_count", factCount),
				slog.Float64("duration_seconds", duration),
				telemetry.PhaseAttr(telemetry.PhaseEmission),
			)
		}
		if span != nil {
			span.SetAttributes(
				attribute.String("scope_id", collected.Scope.ScopeID),
				attribute.Int("fact_count", factCount),
			)
			span.End()
		}
		total++
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create discovery advisory report directory: %w", err)
	}
	contents, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal discovery advisory report: %w", err)
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(path, contents, 0o644); err != nil {
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
		logger.InfoContext(ctx, "bootstrap projection complete",
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
		itemCtx, span = tracer.Start(ctx, telemetry.SpanProjectorRun,
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
						logAttrs = append(logAttrs,
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
		logAttrs = append(logAttrs,
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
					logger.InfoContext(ctx, "bootstrap projection complete",
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
