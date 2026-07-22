// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	defaultPartitionCount     = 8
	defaultSharedPollInterval = 500 * time.Millisecond
	defaultLeaseTTL           = 60 * time.Second
	defaultBatchLimit         = 100
	defaultEvidenceSource     = "finalization/workloads"
	maxSharedPollInterval     = 5 * time.Second
)

// sharedProjectionDomains lists the shared projection domains processed
// by the partition worker.
var sharedProjectionDomains = []string{
	DomainWorkloadDependency,
	DomainInheritanceEdges,
	DomainDocumentationEdges,
	DomainRationaleEdges,
	DomainSQLRelationships,
	DomainShellExec,
	DomainHandlesRoute,
	DomainRunsIn,
	DomainInvokesCloudAction,
	DomainCodeownersOwnershipEdges,
	DomainSubmodulePinEdges,
}

// SharedProjectionRunnerConfig holds configuration for the shared projection
// partition worker.
type SharedProjectionRunnerConfig struct {
	PartitionCount int
	PollInterval   time.Duration
	LeaseTTL       time.Duration
	LeaseOwner     string
	BatchLimit     int
	EvidenceSource string
	Workers        int // concurrent partition workers; 0 or 1 means sequential
}

func (c SharedProjectionRunnerConfig) partitionCount() int {
	if c.PartitionCount <= 0 {
		return defaultPartitionCount
	}
	return c.PartitionCount
}

func (c SharedProjectionRunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultSharedPollInterval
	}
	return c.PollInterval
}

func (c SharedProjectionRunnerConfig) leaseTTL() time.Duration {
	if c.LeaseTTL <= 0 {
		return defaultLeaseTTL
	}
	return c.LeaseTTL
}

func (c SharedProjectionRunnerConfig) batchLimit() int {
	if c.BatchLimit <= 0 {
		return defaultBatchLimit
	}
	return c.BatchLimit
}

func (c SharedProjectionRunnerConfig) evidenceSource() string {
	if c.EvidenceSource == "" {
		return defaultEvidenceSource
	}
	return c.EvidenceSource
}

func (c SharedProjectionRunnerConfig) leaseOwner() string {
	if c.LeaseOwner == "" {
		return "shared-projection-runner"
	}
	return c.LeaseOwner
}

// sharedProjectionDomainEvidenceSource returns the evidence source the worker must
// stamp on a domain's edges, falling back to the runner's global source. Domains
// promoted onto the shared-projection runner from a dedicated materialization
// handler keep the handler's original evidence source so that, across an upgrade,
// the refresh/delta retract still matches the edges the old handler wrote (a
// mismatched source would leave stale edges un-retracted and change edge
// provenance). inheritance_edges keeps reducer/inheritance (#2867); the symbol→runtime
// domains have no pre-existing edges and stay on the runner's global source.
func sharedProjectionDomainEvidenceSource(domain, fallback string) string {
	switch domain {
	case DomainInheritanceEdges:
		return inheritanceEvidenceSource
	default:
		return fallback
	}
}

// SharedProjectionRunner processes shared projection intents across all
// domains and partitions. It runs as a long-lived goroutine alongside the
// main reducer claim/execute/ack loop.
type SharedProjectionRunner struct {
	IntentReader        SharedIntentReader
	LeaseManager        PartitionLeaseManager
	EdgeWriter          SharedProjectionEdgeWriter
	AcceptedGen         AcceptedGenerationLookup
	AcceptedGenPrefetch AcceptedGenerationPrefetch
	ReadinessLookup     GraphProjectionReadinessLookup
	ReadinessPrefetch   GraphProjectionReadinessPrefetch
	// EndpointPresenceLookup answers property-keyed (repo_id, path) :Endpoint
	// presence for the DomainHandlesRoute readiness gate (#2809). A nil lookup
	// disables the gate, leaving handles_route — and every other domain —
	// byte-identical to its pre-#2809 behavior.
	EndpointPresenceLookup EndpointPresenceLookup
	// RefreshFenceLookup gates the repo-wide-retract domains (handles_route,
	// runs_in, invokes_cloud_action) so each repo's single repo-wide retract runs
	// once via its refresh intent and per-edge writes are held until it completes
	// (#2898/#2910). A nil lookup leaves those domains byte-identical to their
	// pre-fix per-partition retract behavior.
	RefreshFenceLookup SharedProjectionRefreshFenceLookup
	// FirstProjectionLookup lets a repo-wide-retract domain's refresh row skip its
	// whole-scope retract when the scope has no generation other than the current
	// one (#3624): with zero prior edges the retract is a guaranteed no-op, and on
	// NornicDB that retract is a full-scan the cold-corpus long pole pays once per
	// repo per domain. A nil lookup disables the skip, leaving the retract
	// byte-identical to pre-#3624 behavior.
	FirstProjectionLookup FirstProjectionLookup
	Config                SharedProjectionRunnerConfig
	Wait                  func(context.Context, time.Duration) error

	// Telemetry fields (optional)
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Run processes shared projection intents until the context is cancelled.
// Each cycle iterates over all domains and partitions, calling
// ProcessPartitionOnce for each combination. When no work is found, the
// poll interval doubles on each consecutive empty cycle (up to 5s) to
// avoid sustained high-frequency polling during idle periods.
func (r *SharedProjectionRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}

	consecutiveEmpty := 0

	for {
		if ctx.Err() != nil {
			return nil
		}

		result := r.runOneCycle(ctx)

		if result.ProcessedIntents > 0 {
			consecutiveEmpty = 0
			continue // immediately re-poll
		}
		if result.BlockedReadiness > 0 {
			consecutiveEmpty = 0
			if err := r.wait(ctx, r.Config.pollInterval()); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("wait for shared projection readiness: %w", err)
			}
			continue
		}

		consecutiveEmpty++
		backoff := r.Config.pollInterval()
		for i := 1; i < consecutiveEmpty && i < 4; i++ {
			backoff *= 2
		}
		if backoff > maxSharedPollInterval {
			backoff = maxSharedPollInterval
		}

		if err := r.wait(ctx, backoff); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wait for shared projection work: %w", err)
		}
	}
}

// runOneCycle iterates all domains and partitions, returning the aggregate
// progress and readiness-blocking signal for the cycle.
func (r *SharedProjectionRunner) runOneCycle(ctx context.Context) PartitionProcessResult {
	if r.Config.Workers <= 1 {
		return r.runOneCycleSequential(ctx)
	}
	return r.runOneCycleConcurrent(ctx)
}

// runOneCycleSequential processes partitions one at a time.
func (r *SharedProjectionRunner) runOneCycleSequential(ctx context.Context) PartitionProcessResult {
	now := time.Now().UTC()
	partitionCount := r.Config.partitionCount()
	var cycleResult PartitionProcessResult

	for _, domain := range sharedProjectionDomains {
		for partitionID := 0; partitionID < partitionCount; partitionID++ {
			if ctx.Err() != nil {
				return cycleResult
			}

			result, err := r.processPartitionWithTelemetry(
				ctx,
				now,
				domain,
				partitionID,
				partitionCount,
			)
			if err != nil {
				continue
			}
			mergePartitionProcessResult(&cycleResult, result)
		}
	}

	return cycleResult
}

// partitionWork represents a single domain/partition combination to process.
type partitionWork struct {
	domain      string
	partitionID int
}

// runOneCycleConcurrent processes partitions across N concurrent workers.
func (r *SharedProjectionRunner) runOneCycleConcurrent(ctx context.Context) PartitionProcessResult {
	now := time.Now().UTC()
	partitionCount := r.Config.partitionCount()

	// Build work queue
	var work []partitionWork
	for _, domain := range sharedProjectionDomains {
		for partitionID := 0; partitionID < partitionCount; partitionID++ {
			work = append(work, partitionWork{domain: domain, partitionID: partitionID})
		}
	}

	workChan := make(chan partitionWork, len(work))
	for _, w := range work {
		workChan <- w
	}
	close(workChan)

	var (
		wg          sync.WaitGroup
		cycleResult PartitionProcessResult
		mu          sync.Mutex
	)

	for i := 0; i < r.Config.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range workChan {
				if ctx.Err() != nil {
					return
				}

				result, err := r.processPartitionWithTelemetry(
					ctx,
					now,
					w.domain,
					w.partitionID,
					partitionCount,
				)
				if err != nil {
					continue
				}
				mu.Lock()
				mergePartitionProcessResult(&cycleResult, result)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return cycleResult
}

// mergePartitionProcessResult preserves the cycle-level signals that drive
// polling behavior without coupling the runner to any one partition result.
func mergePartitionProcessResult(total *PartitionProcessResult, result PartitionProcessResult) {
	total.ProcessedIntents += result.ProcessedIntents
	total.BlockedReadiness += result.BlockedReadiness
	if result.MaxBlockedIntentWaitSeconds > total.MaxBlockedIntentWaitSeconds {
		total.MaxBlockedIntentWaitSeconds = result.MaxBlockedIntentWaitSeconds
	}
}

func (r *SharedProjectionRunner) processPartitionWithTelemetry(
	ctx context.Context,
	now time.Time,
	domain string,
	partitionID int,
	partitionCount int,
) (PartitionProcessResult, error) {
	start := time.Now()

	if r.Tracer != nil {
		var span trace.Span
		ctx, span = r.Tracer.Start(ctx, telemetry.SpanCanonicalWrite)
		defer span.End()
	}

	result, err := ProcessPartitionOnce(
		ctx,
		now,
		PartitionProcessorConfig{
			Domain:         domain,
			PartitionID:    partitionID,
			PartitionCount: partitionCount,
			LeaseOwner:     r.Config.leaseOwner(),
			LeaseTTL:       r.Config.leaseTTL(),
			BatchLimit:     r.Config.batchLimit(),
			EvidenceSource: sharedProjectionDomainEvidenceSource(domain, r.Config.evidenceSource()),
			Instruments:    r.Instruments,
			Logger:         r.Logger,
		},
		r.LeaseManager,
		r.IntentReader,
		r.EdgeWriter,
		r.AcceptedGen,
		r.AcceptedGenPrefetch,
		r.ReadinessLookup,
		r.ReadinessPrefetch,
		r.EndpointPresenceLookup,
		r.RefreshFenceLookup,
		r.FirstProjectionLookup,
	)

	duration := time.Since(start).Seconds()
	acceptanceTelemetry := sharedAcceptanceTelemetry{
		Instruments: r.Instruments,
		Logger:      r.Logger,
	}
	acceptanceTelemetry.RecordStaleIntents(ctx, "shared_projection", domain, result.StaleIntents)
	if result.BlockedReadiness > 0 && r.Logger != nil {
		r.Logger.InfoContext(
			ctx,
			"shared projection skipped intents until semantic readiness is committed",
			log.Domain(domain),
			slog.Int("partition_id", partitionID),
			slog.Int("partition_count", partitionCount),
			slog.Int("blocked_count", result.BlockedReadiness),
			slog.Float64("blocked_intent_wait_seconds", result.MaxBlockedIntentWaitSeconds),
			telemetry.PhaseAttr(telemetry.PhaseShared),
		)
	}
	if result.TerminalNoEndpoint > 0 && r.Logger != nil {
		// Operator signal (#2809 handles_route, #2855 runs_in): symbol→runtime rows
		// drained with no edge because their runtime target will never commit (a
		// route-only repo with no endpoint, or a repo with no Workload). The
		// `domain` attribute says which gate. Distinct from readiness-blocked —
		// these rows are complete, not waiting — so a non-zero count is expected,
		// not a stall.
		r.Logger.InfoContext(
			ctx,
			"shared projection drained intents with no runtime target",
			log.Domain(domain),
			slog.Int("partition_id", partitionID),
			slog.Int("partition_count", partitionCount),
			slog.Int("terminal_no_endpoint_count", result.TerminalNoEndpoint),
			telemetry.PhaseAttr(telemetry.PhaseShared),
		)
	}
	if result.RefreshFenceDeferred > 0 && r.Logger != nil {
		// Operator signal (#2898): per-edge rows held until their repo's single
		// repo-wide retract (refresh intent) completes. Expected briefly each cycle
		// while a repo's refresh partition is still pending; a value that stays
		// non-zero for a repo means its refresh intent is not completing.
		r.Logger.InfoContext(
			ctx,
			"shared projection deferred per-edge rows behind repo refresh fence",
			log.Domain(domain),
			slog.Int("partition_id", partitionID),
			slog.Int("partition_count", partitionCount),
			slog.Int("refresh_fence_deferred_count", result.RefreshFenceDeferred),
			telemetry.PhaseAttr(telemetry.PhaseShared),
		)
	}

	if err == nil {
		r.recordSharedProjectionTiming(ctx, domain, result)
		r.recordSharedProjectionPartitionMetrics(ctx, domain, partitionID, duration, result)
	}

	if err == nil && result.ProcessedIntents > 0 {
		r.recordSharedProjectionCycle(ctx, domain, duration, result)
	}

	return result, err
}

func (r *SharedProjectionRunner) validate() error {
	if r.IntentReader == nil {
		return errors.New("shared projection runner: intent reader is required")
	}
	if r.LeaseManager == nil {
		return errors.New("shared projection runner: lease manager is required")
	}
	if r.EdgeWriter == nil {
		return errors.New("shared projection runner: edge writer is required")
	}
	return nil
}

func (r *SharedProjectionRunner) wait(ctx context.Context, interval time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, interval)
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// LoadSharedProjectionConfig parses shared projection env vars.
