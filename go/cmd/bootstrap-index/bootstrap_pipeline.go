// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/cpubudget"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

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
	// recordPhaseStart logs an explicit phase-start signal before a long
	// sequential post-collection phase begins (#4271). Without this, an
	// operator watching logs sees "bootstrap phase complete" for collection
	// and projection and then nothing until the next phase's (possibly very
	// long) call returns — unable to distinguish active work in that phase
	// from a stuck one-shot lifecycle. This is a log-only signal: the phase's
	// duration is already covered by BootstrapPipelinePhaseDuration recorded
	// via recordPhase/recordPhaseDuration once the phase completes, so no new
	// metric is required to bound this gap.
	recordPhaseStart := func(phase string) {
		if logger != nil {
			logger.InfoContext(
				ctx, "bootstrap phase start",
				slog.String("bootstrap_phase", phase),
				telemetry.PhaseAttr(telemetry.PhaseEmission),
			)
		}
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
		err := drainCollector(ctx, cd.source, cd.committer, tracer, instruments, logger, cd.commitLanes, firstDiscoveryAdvisorySink(advisorySinks))
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
	recordPhaseStart(telemetry.BootstrapPhaseRelationshipBackfill)
	if err := cd.committer.BackfillAllRelationshipEvidence(ctx, tracer, instruments); err != nil {
		recordPhase(telemetry.BootstrapPhaseRelationshipBackfill, backfillStart)
		if logger != nil {
			logger.ErrorContext(
				ctx, "deferred relationship backfill failed",
				log.Err(err),
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
				log.Err(err),
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
				log.Err(err),
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
				log.Err(err),
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
				log.Err(err),
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
				log.Err(err),
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
	n := cpubudget.UsableCPUs()
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
