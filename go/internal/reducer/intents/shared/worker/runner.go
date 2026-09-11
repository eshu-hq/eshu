// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	defaultPartitionCount = 8
	// DefaultSharedPollInterval is the root spelling of the shared projection
	// runner's default poll interval.
	DefaultSharedPollInterval = 500 * time.Millisecond
	// DefaultLeaseTTL is the shared projection runner's default partition
	// lease TTL.
	DefaultLeaseTTL = 60 * time.Second
	// DefaultBatchLimit is the shared projection runner's default batch size.
	DefaultBatchLimit = 100
	// DefaultEvidenceSource is the shared projection runner's default
	// evidence_source, used when a domain has no dedicated one.
	DefaultEvidenceSource = "finalization/workloads"
	maxSharedPollInterval = 5 * time.Second
)

// DefaultSharedProjectionLeaseOwnerPrefix is the default human-readable label
// prepended to the production reducer's process-unique shared projection owner.
const DefaultSharedProjectionLeaseOwnerPrefix = "shared-projection-runner"

// sharedProjectionDomains lists the shared projection domains processed
// by the partition worker.
var sharedProjectionDomains = []string{
	reducercontract.DomainWorkloadDependency,
	reducercontract.DomainInheritanceEdges,
	reducercontract.DomainDocumentationEdges,
	reducercontract.DomainRationaleEdges,
	reducercontract.DomainSQLRelationships,
	reducercontract.DomainShellExec,
	reducercontract.DomainHandlesRoute,
	reducercontract.DomainRunsIn,
	reducercontract.DomainInvokesCloudAction,
	reducercontract.DomainCodeownersOwnershipEdges,
	reducercontract.DomainSubmodulePinEdges,
}

// SharedProjectionDomains returns the shared projection domains the generic
// partition worker drains (moved here from the reducer root's unexported
// sharedProjectionDomains, issue #6061). The root keeps a var initialized
// from this at package init, since Go has no var alias across packages.
func SharedProjectionDomains() []string {
	return sharedProjectionDomains
}

// SharedProjectionRunner processes shared projection intents across all
// domains and partitions. It runs as a long-lived goroutine alongside the
// main reducer claim/execute/ack loop.
type SharedProjectionRunner struct {
	IntentReader        SharedIntentReader
	LeaseManager        sharedintent.PartitionLeaseManager
	EdgeWriter          sharedintent.EdgeWriter
	AcceptedGen         sharedintent.AcceptedGenerationLookup
	AcceptedGenPrefetch sharedintent.AcceptedGenerationPrefetch
	ReadinessLookup     gpphase.ReadinessLookup
	ReadinessPrefetch   gpphase.ReadinessPrefetch
	// EndpointPresenceLookup answers property-keyed (repo_id, path) :Endpoint
	// presence for the DomainHandlesRoute readiness gate (#2809). A nil lookup
	// disables the gate, leaving handles_route — and every other domain —
	// byte-identical to its pre-#2809 behavior.
	EndpointPresenceLookup gpphase.EndpointPresenceLookup
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

	// UnroutableWriter persists a durable row for every intent a canonical
	// edge write could not route (#5984). Nil is accepted so existing callers
	// and tests keep working, but a nil sink means a rejected row is visible
	// only in the writer's WARN and counter -- deployments that care about
	// edge-loss truth must wire it. The worker fails its cycle when this
	// write fails, because the intent is completed immediately afterwards and
	// the durable row is then the only record.
	UnroutableWriter UnroutableWriter
	Config           SharedProjectionRunnerConfig
	Wait             func(context.Context, time.Duration) error

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
			MergePartitionProcessResult(&cycleResult, result)
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
				MergePartitionProcessResult(&cycleResult, result)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return cycleResult
}

// MergePartitionProcessResult preserves the cycle-level signals that drive
// polling behavior without coupling the runner to any one partition result.
func MergePartitionProcessResult(total *PartitionProcessResult, result PartitionProcessResult) {
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
		r.UnroutableWriter,
	)

	duration := time.Since(start).Seconds()
	acceptanceTelemetry := SharedAcceptanceTelemetry{
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

	if err != nil && r.Logger != nil {
		// Durable observability for a shared-projection partition failure
		// (e.g. a graph-write MERGE error): the caller (runOneCycleSequential
		// / runOneCycleConcurrent) drops this error with `continue` and
		// retries the same (domain, partition) on the next poll cycle --
		// shared_projection_intents rows carry no attempt_count column, so
		// without this log line a failed-then-recovered write leaves zero
		// durable trace anywhere (unlike a fact_work_items domain, where
		// WorkSink.Fail durably increments attempt_count). This is the
		// fired-fault, non-vacuity signal
		// scripts/verify-ifa-fault-injection.sh's SQL-targeted
		// fail-graph-write-once-then-succeed-sql cell polls for (issue #5555).
		r.Logger.ErrorContext(
			ctx,
			"shared projection partition processing failed; retrying on next poll cycle",
			log.Domain(domain),
			slog.Int("partition_id", partitionID),
			slog.Int("partition_count", partitionCount),
			slog.String("error", err.Error()),
			telemetry.PhaseAttr(telemetry.PhaseShared),
		)
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
