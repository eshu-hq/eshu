// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RuntimeNotReadyFailureClass classifies a deferral of a service catalog
// correlation intent whose runtime evidence would be read while a correlated
// repository's WorkloadInstance producer is still in flight (#7816).
//
// Registered as a non-counting reducer retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go): the intent
// waits on upstream materialization, not on its own merits. Without the fence
// the handler read zero WorkloadInstance rows before the producer's MERGE and
// committed a service generation with an empty runtime family that nothing
// later repairs.
//
// The wait is bounded by crossscope.ProducerReadinessMaxWait, measured per
// in-flight repository from the newest in-flight producer row's last-workable
// time, GREATEST(COALESCE(reopened_at, created_at), COALESCE(last_attempt_at,
// created_at), COALESCE(visible_at, created_at), COALESCE(claim_until,
// created_at)): the latest of its cycle start, last claim, scheduled
// visibility, and lease expiry. claim_until is non-NULL only on claimed and
// running rows: a claim sets it, a heartbeat extends it, and retry, ack,
// fail, and dead-letter set it NULL. The handler proceeds only when every
// in-flight repository is past the bound; it then commits the runtime
// evidence it reads, which can be an empty family. The catalog intent's own
// cycle anchor is log-only: it also counts queue wait and corpus-fence wait.
// A producer row that is polling a non-counting readiness class, or whose
// worker heartbeats, refreshes its anchor and never counts as exceeded; only
// a starved row or a dead worker's row whose lease expired 30 minutes ago
// does. A handler that heartbeats forever without finishing therefore holds
// the catalog: that is the reducer lease policy's gap, tracked in #8211. A
// zero producer anchor means elapsed time is unknown and keeps deferring.
const RuntimeNotReadyFailureClass = "service_catalog_correlation_runtime_not_ready"

// Runtime readiness outcomes recorded on
// eshu_dp_service_catalog_runtime_readiness_total. The set is closed.
const (
	// RuntimeReadinessReady means every correlated repository has producer
	// work at its active generation and none of it is in flight.
	RuntimeReadinessReady = "ready"
	// RuntimeReadinessNoProducer means no correlated repository has a
	// producer work item at its active generation (a repository without
	// workloads). Absent means ready, so such repositories never deadlock.
	RuntimeReadinessNoProducer = "no_producer"
	// RuntimeReadinessDeferred means a producer is in flight inside the bound
	// and the intent deferred with nothing written.
	RuntimeReadinessDeferred = "deferred"
	// RuntimeReadinessBoundExceeded means every in-flight producer row had
	// gone at least crossscope.ProducerReadinessMaxWait with no claim,
	// heartbeat, scheduled visibility, or cycle start, measured from its
	// last-workable time, and the handler
	// proceeded with the runtime evidence it could read, possibly an empty
	// family.
	RuntimeReadinessBoundExceeded = "bound_exceeded"
)

// runtimeProducerDomains is the enumerated producer set of the runtime
// evidence family: every reducer queue domain whose fact_work_items rows write
// the WorkloadInstance nodes and RUNS_ON edges GraphServiceRuntimeInstanceLoader
// reads. TestRuntimeProducerSetCoversEveryRuntimeWriter pins it against the
// writers found in source; a new writer fails that guard until it is either
// added here or classified there with a reason.
var runtimeProducerDomains = []reducercontract.Domain{
	reducercontract.DomainWorkloadMaterialization,
}

// RuntimeProducerDomains returns a copy of the producer domains the runtime
// fence waits on, in declaration order.
func RuntimeProducerDomains() []reducercontract.Domain {
	return slices.Clone(runtimeProducerDomains)
}

// RuntimeProducerState counts one repository's producer work items at its
// active generation, by queue status. Total counts every status, terminal
// ones included, so a zero Total means the repository has no producer work at
// its active generation.
type RuntimeProducerState struct {
	Pending  int
	Retrying int
	Claimed  int
	Running  int
	Total    int
	// NewestInFlightWorkableAt is the latest last-workable time across the
	// repository's in-flight producer rows. A row's last-workable time is
	// GREATEST(COALESCE(reopened_at, created_at), COALESCE(last_attempt_at,
	// created_at), COALESCE(visible_at, created_at), COALESCE(claim_until,
	// created_at)): the latest of its cycle start, last claim, scheduled
	// visibility, and lease expiry. It can lie in the future while a retry
	// backoff sleeps or a lease is live. It is zero when no producer row is in
	// flight.
	NewestInFlightWorkableAt time.Time
	// NewestInFlightClaimUntil is the latest lease end (claim_until) across
	// the repository's in-flight producer rows. claim_until is set only on
	// claimed and running rows, so it is zero when none holds a lease.
	NewestInFlightClaimUntil time.Time
}

// InFlight reports how many producer work items are not yet terminal.
func (s RuntimeProducerState) InFlight() int {
	return s.Pending + s.Retrying + s.Claimed + s.Running
}

// RuntimeProducerReadiness reports, per repository, the state of the producer
// work that writes the repository's WorkloadInstance nodes. Only work items at
// the repository scope's ACTIVE generation count: a newer pending generation
// is a later cycle and an older one is superseded. A repository with no
// producer row there is absent from the result. An error means the question
// could not be answered and must never be read as ready or not ready.
type RuntimeProducerReadiness interface {
	RuntimeProducerStates(
		ctx context.Context,
		repoIDs []string,
		producerDomains []reducercontract.Domain,
	) (map[string]RuntimeProducerState, error)
}

// runtimeNotReadyError defers the whole intent, before any write, until the
// runtime producers of its correlated repositories drain.
type runtimeNotReadyError struct {
	scopeID      string
	generationID string
	repoIDs      []string
}

// Error names the total and at most the first runtimeReadinessLogRepoSample
// repository ids. The queue persists this message on the work item on every
// non-counting deferral cycle, so a wide catalog scope must not grow it; the
// struct keeps the full list.
func (e runtimeNotReadyError) Error() string {
	sample, more := e.repoIDs, ""
	if extra := len(sample) - runtimeReadinessLogRepoSample; extra > 0 {
		sample = sample[:runtimeReadinessLogRepoSample]
		more = fmt.Sprintf(" and %d more", extra)
	}
	return fmt.Sprintf(
		"workload materialization in flight for %d correlated repositor(ies) %v%s of service catalog scope %s generation %s; deferring service catalog correlation rather than committing runtime evidence read before the WorkloadInstance write",
		len(e.repoIDs), sample, more, e.scopeID, e.generationID,
	)
}

// Retryable reports that the deferral must be retried once the producers drain.
func (runtimeNotReadyError) Retryable() bool { return true }

// FailureClass returns the non-counting readiness class for the queue.
func (runtimeNotReadyError) FailureClass() string { return RuntimeNotReadyFailureClass }

// runtimeReadinessLogRepoSample bounds the repository ids one log line carries.
const runtimeReadinessLogRepoSample = 10

// checkServiceRuntimeReadiness is the #7816 fence. It runs before the runtime
// read and before any write. It is a no-op when no runtime loader or readiness
// port is wired, or no service carries a repository, because then there is no
// runtime read to fence. When a correlated repository's producer is in flight
// it returns runtimeNotReadyError, unless every in-flight repository's
// producer is past the bound from its own last-workable time; then it
// proceeds. It records the outcome either way.
func (h ServiceCatalogCorrelationHandler) checkServiceRuntimeReadiness(
	ctx context.Context,
	intent reducercontract.Intent,
	writes []ServiceMaterializationWrite,
	decisions []ServiceCatalogCorrelationDecision,
) error {
	if h.RuntimeInstanceLoader == nil || h.RuntimeProducerReadiness == nil || len(writes) == 0 {
		return nil
	}
	repoIDs := distinctServiceRepositoryIDs(writes, serviceRepositoryIndex(decisions))
	if len(repoIDs) == 0 {
		return nil
	}
	states, err := h.RuntimeProducerReadiness.RuntimeProducerStates(ctx, repoIDs, RuntimeProducerDomains())
	if err != nil {
		// A torn connection is the retryable fact_load_transient class, which
		// still counts against the retry budget; every other error is returned
		// as is. Neither is ever converted into the non-counting class.
		return fmt.Errorf("check service runtime producer readiness: %w", factload.ClassifyFactLoadError(err))
	}
	inFlight, totals := runtimeReadinessInFlight(repoIDs, states)
	if len(inFlight) == 0 {
		outcome := RuntimeReadinessReady
		if totals.Total == 0 {
			outcome = RuntimeReadinessNoProducer
		}
		h.recordRuntimeReadiness(ctx, outcome)
		return nil
	}
	now := h.now()
	allExceeded, oldestWorkable := runtimeProducerBound(inFlight, states, now)
	if allExceeded {
		h.recordRuntimeReadiness(ctx, RuntimeReadinessBoundExceeded)
		logRuntimeReadiness(ctx, h.logger(), slog.LevelWarn,
			"service catalog runtime readiness bound exceeded; committing the runtime evidence read now",
			intent, now, oldestWorkable, RuntimeReadinessBoundExceeded, inFlight, totals)
		return nil
	}
	h.recordRuntimeReadiness(ctx, RuntimeReadinessDeferred)
	logRuntimeReadiness(ctx, h.logger(), slog.LevelInfo,
		"service catalog correlation deferred: workload materialization in flight for a correlated repository",
		intent, now, oldestWorkable, RuntimeReadinessDeferred, inFlight, totals)
	return runtimeNotReadyError{scopeID: intent.ScopeID, generationID: intent.GenerationID, repoIDs: inFlight}
}

// runtimeProducerBound reports whether every in-flight repository's producer
// is at least crossscope.ProducerReadinessMaxWait past its own
// NewestInFlightWorkableAt. A zero anchor is unknown elapsed time, not
// infinite, so it is never exceeded. It also returns the oldest known
// workable anchor across the in-flight repositories, for the log line; that
// is zero when no anchor is known.
func runtimeProducerBound(
	inFlight []string,
	states map[string]RuntimeProducerState,
	now time.Time,
) (bool, time.Time) {
	allExceeded := len(inFlight) > 0
	var oldest time.Time
	for _, repoID := range inFlight {
		workable := states[repoID].NewestInFlightWorkableAt
		if workable.IsZero() {
			allExceeded = false
			continue
		}
		if oldest.IsZero() || workable.Before(oldest) {
			oldest = workable
		}
		if now.Sub(workable) < crossscope.ProducerReadinessMaxWait {
			allExceeded = false
		}
	}
	return allExceeded, oldest
}

// runtimeReadinessInFlight returns the repositories with in-flight producer
// work, in repoIDs order, and the summed state across every repository. The
// summed state's NewestInFlightClaimUntil is the latest lease end of any
// repository, for the log line; its NewestInFlightWorkableAt stays zero.
func runtimeReadinessInFlight(
	repoIDs []string,
	states map[string]RuntimeProducerState,
) ([]string, RuntimeProducerState) {
	var inFlight []string
	var totals RuntimeProducerState
	for _, repoID := range repoIDs {
		state := states[repoID]
		totals.Pending += state.Pending
		totals.Retrying += state.Retrying
		totals.Claimed += state.Claimed
		totals.Running += state.Running
		totals.Total += state.Total
		if state.NewestInFlightClaimUntil.After(totals.NewestInFlightClaimUntil) {
			totals.NewestInFlightClaimUntil = state.NewestInFlightClaimUntil
		}
		if state.InFlight() > 0 {
			inFlight = append(inFlight, repoID)
		}
	}
	return inFlight, totals
}

func (h ServiceCatalogCorrelationHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h ServiceCatalogCorrelationHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func (h ServiceCatalogCorrelationHandler) recordRuntimeReadiness(ctx context.Context, outcome string) {
	if h.Instruments == nil || h.Instruments.ServiceCatalogRuntimeReadiness == nil {
		return
	}
	h.Instruments.ServiceCatalogRuntimeReadiness.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainServiceCatalogCorrelation)),
		telemetry.AttrOutcome(outcome),
	))
}

// logRuntimeReadiness writes the defer or bound-exceeded line. It carries
// producer_elapsed (since the oldest in-flight repository's workable anchor;
// negative when that anchor lies in the future, as during a retry backoff or
// a live lease) against producer_max_wait, which is the pair the decision
// uses, producer_newest_claim_until (the latest in-flight lease end, when any
// producer holds a lease), and elapsed_since_cycle_start of the catalog
// intent for context only. It never
// carries attempt_count, which the non-counting class freezes. Producer states
// are summed counts; the in-flight repository ids are bounded to the first
// runtimeReadinessLogRepoSample.
func logRuntimeReadiness(
	ctx context.Context,
	logger *slog.Logger,
	level slog.Level,
	message string,
	intent reducercontract.Intent,
	now time.Time,
	oldestProducerWorkable time.Time,
	outcome string,
	inFlight []string,
	totals RuntimeProducerState,
) {
	sample := inFlight
	if len(sample) > runtimeReadinessLogRepoSample {
		sample = sample[:runtimeReadinessLogRepoSample]
	}
	attrs := []slog.Attr{
		slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.String(telemetry.LogKeyFailureClass, RuntimeNotReadyFailureClass),
		slog.String("runtime_readiness_outcome", outcome),
		slog.Int("in_flight_repo_count", len(inFlight)),
		slog.Any("in_flight_repo_ids", sample),
		slog.Int("producer_pending", totals.Pending),
		slog.Int("producer_retrying", totals.Retrying),
		slog.Int("producer_claimed", totals.Claimed),
		slog.Int("producer_running", totals.Running),
		slog.Duration("producer_max_wait", crossscope.ProducerReadinessMaxWait),
	}
	if !oldestProducerWorkable.IsZero() {
		attrs = append(attrs, slog.Duration("producer_elapsed", now.Sub(oldestProducerWorkable)))
	}
	if !totals.NewestInFlightClaimUntil.IsZero() {
		attrs = append(attrs, slog.Time("producer_newest_claim_until", totals.NewestInFlightClaimUntil))
	}
	if anchor := crossscope.ReadinessCycleAnchor(intent); !anchor.IsZero() {
		attrs = append(attrs, slog.Duration("elapsed_since_cycle_start", now.Sub(anchor)))
	}
	logger.LogAttrs(ctx, level, message, attrs...)
}
