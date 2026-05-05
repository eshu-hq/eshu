package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
	DomainPlatformInfra,
	DomainWorkloadDependency,
	DomainInheritanceEdges,
	DomainSQLRelationships,
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
	Config              SharedProjectionRunnerConfig
	Wait                func(context.Context, time.Duration) error

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
			EvidenceSource: r.Config.evidenceSource(),
		},
		r.LeaseManager,
		r.IntentReader,
		r.EdgeWriter,
		r.AcceptedGen,
		r.AcceptedGenPrefetch,
		r.ReadinessLookup,
		r.ReadinessPrefetch,
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
			slog.String("domain", domain),
			slog.Int("partition_id", partitionID),
			slog.Int("partition_count", partitionCount),
			slog.Int("blocked_count", result.BlockedReadiness),
			slog.Float64("blocked_intent_wait_seconds", result.MaxBlockedIntentWaitSeconds),
			telemetry.PhaseAttr(telemetry.PhaseShared),
		)
	}

	if err == nil {
		r.recordSharedProjectionTiming(ctx, domain, result)
	}

	if err == nil && result.ProcessedIntents > 0 {
		r.recordSharedProjectionCycle(ctx, domain, duration, result)
	}

	return result, err
}

func (r *SharedProjectionRunner) recordSharedProjectionTiming(
	ctx context.Context,
	domain string,
	result PartitionProcessResult,
) {
	if r.Instruments == nil {
		return
	}
	if result.MaxIntentWaitSeconds > 0 {
		r.Instruments.SharedProjectionIntentWaitDuration.Record(
			ctx,
			result.MaxIntentWaitSeconds,
			metric.WithAttributes(
				telemetry.AttrDomain(domain),
				telemetry.AttrOutcome("processed"),
			),
		)
	}
	if result.MaxBlockedIntentWaitSeconds > 0 {
		r.Instruments.SharedProjectionIntentWaitDuration.Record(
			ctx,
			result.MaxBlockedIntentWaitSeconds,
			metric.WithAttributes(
				telemetry.AttrDomain(domain),
				telemetry.AttrOutcome("readiness_blocked"),
			),
		)
	}
	if result.ProcessingDurationSeconds > 0 {
		r.Instruments.SharedProjectionProcessingDuration.Record(
			ctx,
			result.ProcessingDurationSeconds,
			metric.WithAttributes(
				telemetry.AttrDomain(domain),
				telemetry.AttrOutcome("completed"),
			),
		)
	}
	recordSharedProjectionStepDurations(ctx, r.Instruments, domain, result)
}

func (r *SharedProjectionRunner) recordSharedProjectionCycle(
	ctx context.Context,
	domain string,
	duration float64,
	result PartitionProcessResult,
) {
	if r.Instruments != nil {
		attrs := metric.WithAttributes(
			telemetry.AttrDomain(domain),
		)
		r.Instruments.SharedProjectionCycles.Add(ctx, 1, attrs)
		r.Instruments.CanonicalWriteDuration.Record(ctx, duration, attrs)
	}

	if r.Logger != nil {
		domainAttrs := telemetry.DomainAttrs(domain, "")
		logAttrs := make([]any, 0, len(domainAttrs)+1)
		for _, a := range domainAttrs {
			logAttrs = append(logAttrs, a)
		}
		logAttrs = append(logAttrs, slog.Float64("duration_seconds", duration))
		logAttrs = append(logAttrs, slog.Float64("intent_wait_seconds", result.MaxIntentWaitSeconds))
		logAttrs = append(logAttrs, slog.Float64("processing_duration_seconds", result.ProcessingDurationSeconds))
		logAttrs = append(logAttrs, slog.Float64("retract_duration_seconds", result.RetractDurationSeconds))
		logAttrs = append(logAttrs, slog.Float64("write_duration_seconds", result.WriteDurationSeconds))
		logAttrs = append(logAttrs, slog.Float64("mark_completed_duration_seconds", result.MarkCompletedDurationSeconds))
		logAttrs = append(logAttrs, slog.Float64("selection_duration_seconds", result.SelectionDurationSeconds))
		logAttrs = append(logAttrs, slog.Float64("lease_claim_duration_seconds", result.LeaseClaimDurationSeconds))
		logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseShared))
		r.Logger.InfoContext(ctx, "shared projection cycle completed", logAttrs...)
	}
}

func recordSharedProjectionStepDurations(
	ctx context.Context,
	instruments *telemetry.Instruments,
	domain string,
	result PartitionProcessResult,
) {
	if instruments == nil {
		return
	}
	steps := []struct {
		phase    string
		duration float64
	}{
		{phase: "retract", duration: result.RetractDurationSeconds},
		{phase: "write", duration: result.WriteDurationSeconds},
		{phase: "mark_completed", duration: result.MarkCompletedDurationSeconds},
	}
	for _, step := range steps {
		if step.duration <= 0 {
			continue
		}
		instruments.SharedProjectionStepDuration.Record(
			ctx,
			step.duration,
			metric.WithAttributes(
				telemetry.AttrDomain(domain),
				telemetry.AttrWritePhase(step.phase),
				telemetry.AttrOutcome("completed"),
			),
		)
	}
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
func LoadSharedProjectionConfig(getenv func(string) string) SharedProjectionRunnerConfig {
	return SharedProjectionRunnerConfig{
		PartitionCount: intFromEnvDefault(getenv, "ESHU_SHARED_PROJECTION_PARTITION_COUNT", defaultPartitionCount),
		PollInterval:   durationFromEnv(getenv, "ESHU_SHARED_PROJECTION_POLL_INTERVAL", defaultSharedPollInterval),
		LeaseTTL:       durationFromEnv(getenv, "ESHU_SHARED_PROJECTION_LEASE_TTL", defaultLeaseTTL),
		BatchLimit:     intFromEnvDefault(getenv, "ESHU_SHARED_PROJECTION_BATCH_LIMIT", defaultBatchLimit),
		Workers:        intFromEnvDefault(getenv, "ESHU_SHARED_PROJECTION_WORKERS", defaultSharedProjectionWorkers()),
	}
}

func defaultSharedProjectionWorkers() int {
	n := runtime.NumCPU()
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return n
}

func intFromEnvDefault(getenv func(string) string, key string, defaultValue int) int {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultValue
	}
	return value
}

func durationFromEnv(getenv func(string) string, key string, defaultValue time.Duration) time.Duration {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaultValue
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return defaultValue
	}
	return value
}
