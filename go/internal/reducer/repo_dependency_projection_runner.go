package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	defaultRepoDependencyLeaseOwner      = "repo-dependency-projection-runner"
	maxRepoDependencyPollInterval        = 5 * time.Second
	maxRepoDependencyAcceptanceScanLimit = 10_000
)

// RepoDependencyProjectionIntentReader reads repo-dependency intents by domain
// and by source-repo-owned acceptance unit.
type RepoDependencyProjectionIntentReader interface {
	ListPendingDomainIntents(ctx context.Context, domain string, limit int) ([]SharedProjectionIntentRow, error)
	ListAcceptanceUnitDomainIntents(ctx context.Context, acceptanceUnitID, domain string, limit int) ([]SharedProjectionIntentRow, error)
	MarkIntentsCompleted(ctx context.Context, intentIDs []string, completedAt time.Time) error
}

// RepoDependencyProjectionRunnerConfig configures the controlled repo-dependency lane.
type RepoDependencyProjectionRunnerConfig struct {
	LeaseOwner   string
	PollInterval time.Duration
	LeaseTTL     time.Duration
	BatchLimit   int
}

func (c RepoDependencyProjectionRunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultSharedPollInterval
	}
	return c.PollInterval
}

func (c RepoDependencyProjectionRunnerConfig) leaseTTL() time.Duration {
	if c.LeaseTTL <= 0 {
		return defaultLeaseTTL
	}
	return c.LeaseTTL
}

func (c RepoDependencyProjectionRunnerConfig) batchLimit() int {
	if c.BatchLimit <= 0 {
		return defaultBatchLimit
	}
	return c.BatchLimit
}

func (c RepoDependencyProjectionRunnerConfig) leaseOwner() string {
	if c.LeaseOwner == "" {
		return defaultRepoDependencyLeaseOwner
	}
	return c.LeaseOwner
}

// RepoDependencyProjectionRunner processes repo-dependency shared intents one
// source repository at a time so repo-wide retractions cannot race with
// partition-sliced snapshots.
type RepoDependencyProjectionRunner struct {
	IntentReader                    RepoDependencyProjectionIntentReader
	LeaseManager                    PartitionLeaseManager
	EdgeWriter                      SharedProjectionEdgeWriter
	WorkloadMaterializationReplayer WorkloadMaterializationReplayer
	AcceptedGen                     AcceptedGenerationLookup
	AcceptedGenPrefetch             AcceptedGenerationPrefetch
	Config                          RepoDependencyProjectionRunnerConfig
	Wait                            func(context.Context, time.Duration) error

	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Run drains repo-dependency work until the context is canceled.
func (r *RepoDependencyProjectionRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}

	consecutiveEmpty := 0
	for {
		if ctx.Err() != nil {
			return nil
		}

		cycleStart := time.Now()
		didWork, err := r.runOneCycle(ctx)
		if err != nil {
			consecutiveEmpty++
			r.recordRepoDependencyCycleFailure(ctx, err, time.Since(cycleStart).Seconds())
			if err := r.wait(ctx, repoDependencyPollBackoff(r.Config.pollInterval(), consecutiveEmpty)); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("wait for repo dependency work: %w", err)
			}
			continue
		}
		if didWork {
			consecutiveEmpty = 0
			continue
		}

		consecutiveEmpty++
		if err := r.wait(ctx, repoDependencyPollBackoff(r.Config.pollInterval(), consecutiveEmpty)); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wait for repo dependency work: %w", err)
		}
	}
}

func (r *RepoDependencyProjectionRunner) runOneCycle(ctx context.Context) (bool, error) {
	result, err := r.processOnce(ctx, time.Now().UTC())
	if err != nil {
		return true, err
	}
	return result.ProcessedIntents > 0, nil
}

func (r *RepoDependencyProjectionRunner) processOnce(ctx context.Context, now time.Time) (PartitionProcessResult, error) {
	cycleStart := time.Now()
	claimStart := time.Now()
	result := PartitionProcessResult{}
	claimed, err := r.LeaseManager.ClaimPartitionLease(
		ctx,
		DomainRepoDependency,
		0,
		1,
		r.Config.leaseOwner(),
		r.Config.leaseTTL(),
	)
	if r.Instruments != nil {
		r.Instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
			attribute.String("queue", "repo_dependency"),
		))
	}
	result.LeaseClaimDurationSeconds = time.Since(claimStart).Seconds()
	if err != nil {
		return PartitionProcessResult{}, fmt.Errorf("claim repo dependency lease: %w", err)
	}
	if !claimed {
		return PartitionProcessResult{LeaseAcquired: false}, nil
	}
	defer func() {
		_ = r.LeaseManager.ReleasePartitionLease(ctx, DomainRepoDependency, 0, 1, r.Config.leaseOwner())
	}()
	workCtx, stopHeartbeat := r.startLeaseHeartbeat(ctx)
	defer stopHeartbeat()

	selectionStart := time.Now()
	acceptanceUnitID, err := r.selectAcceptanceUnitWork(workCtx)
	result.SelectionDurationSeconds = time.Since(selectionStart).Seconds()
	if err != nil {
		result.LeaseAcquired = true
		return result, err
	}
	if acceptanceUnitID == "" {
		result.LeaseAcquired = true
		return result, nil
	}

	loadAllStart := time.Now()
	rows, err := r.loadAllAcceptanceUnitIntents(workCtx, acceptanceUnitID)
	result.LoadAllDurationSeconds = time.Since(loadAllStart).Seconds()
	if err != nil {
		result.LeaseAcquired = true
		return result, err
	}
	result.AcceptanceUnitRows = len(rows)

	lookup := r.AcceptedGen
	if r.AcceptedGenPrefetch != nil {
		prefetchStart := time.Now()
		resolvedLookup, err := r.AcceptedGenPrefetch(workCtx, rows)
		result.AcceptancePrefetchDurationSeconds = time.Since(prefetchStart).Seconds()
		if err != nil {
			result.LeaseAcquired = true
			return result, fmt.Errorf("prefetch accepted generations: %w", err)
		}
		lookup = resolvedLookup
	}

	active, staleIDs := FilterAuthoritativeIntents(rows, lookup)
	result.StaleIntents = len(staleIDs)
	result.ActiveIntents = len(active)
	if len(active) == 0 && len(staleIDs) == 0 {
		result.LeaseAcquired = true
		return result, nil
	}

	result.LeaseAcquired = true
	writtenRows := 0
	writtenGroups := 0
	if len(active) > 0 {
		if repoDependencyNeedsRetract(rows, staleIDs) {
			retractStart := time.Now()
			retractedRows, err := r.retractRepo(workCtx, active)
			result.RetractDurationSeconds = time.Since(retractStart).Seconds()
			if err != nil {
				return result, err
			}
			result.RetractedRows = retractedRows
		}
		writeStart := time.Now()
		writtenRows, writtenGroups, err = r.writeActiveRows(workCtx, active)
		result.WriteDurationSeconds = time.Since(writeStart).Seconds()
		if err != nil {
			return result, err
		}
		result.UpsertedRows = writtenRows
		if r.WorkloadMaterializationReplayer != nil {
			replayStart := time.Now()
			replayRequests, err := r.replayWorkloadMaterialization(workCtx, active)
			result.ReplayDurationSeconds = time.Since(replayStart).Seconds()
			result.ReplayRequests = replayRequests
			if err != nil {
				return result, fmt.Errorf("replay workload materialization after repo dependency projection: %w", err)
			}
		}
	}

	processedIDs := make([]string, 0, len(staleIDs)+len(active))
	processedIDs = append(processedIDs, staleIDs...)
	for _, row := range active {
		processedIDs = append(processedIDs, row.IntentID)
	}
	if len(processedIDs) > 0 {
		markCompletedStart := time.Now()
		if err := r.IntentReader.MarkIntentsCompleted(workCtx, processedIDs, now); err != nil {
			return result, fmt.Errorf("mark repo dependency intents completed: %w", err)
		}
		result.MarkCompletedDurationSeconds = time.Since(markCompletedStart).Seconds()
	}
	result.ProcessedIntents = len(processedIDs)
	result.ProcessingDurationSeconds = time.Since(cycleStart).Seconds()
	if len(active) > 0 {
		r.recordRepoDependencyCycle(ctx, acceptanceUnitID, active, writtenRows, writtenGroups, cycleStart, result)
	}
	return result, nil
}

// startLeaseHeartbeat renews the source-repo lane lease while graph writes are
// in flight so slow backend calls cannot make active work appear abandoned.
func (r *RepoDependencyProjectionRunner) startLeaseHeartbeat(ctx context.Context) (context.Context, func()) {
	interval := repoDependencyLeaseHeartbeatInterval(r.Config.leaseTTL())
	if interval <= 0 {
		return ctx, func() {}
	}
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				claimed, err := r.LeaseManager.ClaimPartitionLease(
					heartbeatCtx,
					DomainRepoDependency,
					0,
					1,
					r.Config.leaseOwner(),
					r.Config.leaseTTL(),
				)
				if err != nil || !claimed {
					if r.Logger != nil {
						attrs := []any{
							slog.String(telemetry.LogKeyDomain, DomainRepoDependency),
							telemetry.PhaseAttr(telemetry.PhaseReduction),
						}
						if err != nil {
							attrs = append(attrs, slog.String("error", err.Error()))
						}
						r.Logger.WarnContext(heartbeatCtx, "repo dependency lease heartbeat failed", attrs...)
					}
					cancel()
					return
				}
			}
		}
	}()
	return heartbeatCtx, func() {
		once.Do(func() {
			close(done)
			cancel()
		})
	}
}

// repoDependencyLeaseHeartbeatInterval renews before the lease reaches its
// deadline while capping idle wakeups for unusually long lease settings.
func repoDependencyLeaseHeartbeatInterval(leaseTTL time.Duration) time.Duration {
	if leaseTTL <= 0 {
		return 0
	}
	interval := leaseTTL / 3
	if interval <= 0 {
		return leaseTTL
	}
	if interval > time.Minute {
		return time.Minute
	}
	return interval
}

func repoDependencyNeedsRetract(rows []SharedProjectionIntentRow, staleIDs []string) bool {
	if len(staleIDs) > 0 {
		return true
	}
	for _, row := range rows {
		action := strings.TrimSpace(repoDependencyPayloadString(row, "action"))
		if action == "delete" || action == "retract" {
			return true
		}
	}
	return false
}

func (r *RepoDependencyProjectionRunner) selectAcceptanceUnitWork(ctx context.Context) (string, error) {
	scanLimit := r.Config.batchLimit()
	if scanLimit > maxRepoDependencyAcceptanceScanLimit {
		scanLimit = maxRepoDependencyAcceptanceScanLimit
	}

	for {
		pending, err := r.IntentReader.ListPendingDomainIntents(ctx, DomainRepoDependency, scanLimit)
		if err != nil {
			return "", fmt.Errorf("list pending repo dependency intents: %w", err)
		}
		if len(pending) == 0 {
			return "", nil
		}

		lookup := r.AcceptedGen
		if r.AcceptedGenPrefetch != nil {
			resolvedLookup, err := r.AcceptedGenPrefetch(ctx, pending)
			if err != nil {
				return "", fmt.Errorf("prefetch accepted generations: %w", err)
			}
			lookup = resolvedLookup
		}

		acceptedByUnit := make(map[string]bool, len(pending))
		order := make([]string, 0, len(pending))
		for _, row := range pending {
			unitID, ok := repoDependencyAcceptanceUnitID(row)
			if !ok {
				return "", fmt.Errorf("pending repo dependency intent %q is missing acceptance unit", row.IntentID)
			}
			if _, seen := acceptedByUnit[unitID]; !seen {
				order = append(order, unitID)
				acceptedByUnit[unitID] = false
			}
			key, ok := row.AcceptanceKey()
			if !ok {
				return "", fmt.Errorf(
					"pending repo dependency intent %q is missing scope, acceptance unit, or source run",
					row.IntentID,
				)
			}
			acceptedGeneration, ok := lookup(key)
			if !ok {
				continue
			}
			if strings.TrimSpace(row.GenerationID) == strings.TrimSpace(acceptedGeneration) {
				acceptedByUnit[unitID] = true
			}
		}

		for _, unitID := range order {
			if acceptedByUnit[unitID] {
				return unitID, nil
			}
		}

		if len(pending) < scanLimit {
			return "", nil
		}
		if scanLimit >= maxRepoDependencyAcceptanceScanLimit {
			return "", fmt.Errorf(
				"repo dependency acceptance scan reached cap (%d) before locating accepted work",
				maxRepoDependencyAcceptanceScanLimit,
			)
		}

		nextLimit := scanLimit * 2
		if nextLimit > maxRepoDependencyAcceptanceScanLimit {
			nextLimit = maxRepoDependencyAcceptanceScanLimit
		}
		scanLimit = nextLimit
	}
}

func (r *RepoDependencyProjectionRunner) loadAllAcceptanceUnitIntents(ctx context.Context, acceptanceUnitID string) ([]SharedProjectionIntentRow, error) {
	limit := r.Config.batchLimit()
	if limit > maxRepoDependencyAcceptanceScanLimit {
		limit = maxRepoDependencyAcceptanceScanLimit
	}
	for {
		rows, err := r.IntentReader.ListAcceptanceUnitDomainIntents(ctx, acceptanceUnitID, DomainRepoDependency, limit)
		if err != nil {
			return nil, fmt.Errorf("list repo dependency acceptance intents: %w", err)
		}
		if len(rows) < limit {
			return rows, nil
		}
		if limit >= maxRepoDependencyAcceptanceScanLimit {
			return nil, fmt.Errorf(
				"repo dependency acceptance intent scan reached cap (%d) for unit %q",
				maxRepoDependencyAcceptanceScanLimit,
				acceptanceUnitID,
			)
		}
		nextLimit := limit * 2
		if nextLimit > maxRepoDependencyAcceptanceScanLimit {
			nextLimit = maxRepoDependencyAcceptanceScanLimit
		}
		limit = nextLimit
	}
}

func (r *RepoDependencyProjectionRunner) retractRepo(ctx context.Context, rows []SharedProjectionIntentRow) (int, error) {
	retractRows := buildRepoDependencyRetractRows(uniqueRepositoryIDs(rows))
	if len(retractRows) == 0 {
		return 0, nil
	}
	sources := repoDependencyEvidenceSources(rows)
	for _, source := range sources {
		if err := r.EdgeWriter.RetractEdges(ctx, DomainRepoDependency, retractRows, source); err != nil {
			return 0, fmt.Errorf("retract repo dependency edges for %s: %w", source, err)
		}
	}
	return len(retractRows) * len(sources), nil
}

func (r *RepoDependencyProjectionRunner) writeActiveRows(ctx context.Context, rows []SharedProjectionIntentRow) (int, int, error) {
	groups := groupRepoDependencyUpsertRows(rows)
	if len(groups) == 0 {
		return 0, 0, nil
	}

	sources := make([]string, 0, len(groups))
	for source := range groups {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	writtenRows := 0
	for _, source := range sources {
		group := groups[source]
		if len(group) == 0 {
			continue
		}
		if err := r.EdgeWriter.WriteEdges(ctx, DomainRepoDependency, group, source); err != nil {
			return 0, 0, fmt.Errorf("write repo dependency edges for %s: %w", source, err)
		}
		writtenRows += len(group)
	}
	return writtenRows, len(sources), nil
}

func (r *RepoDependencyProjectionRunner) recordRepoDependencyCycle(
	ctx context.Context,
	acceptanceUnitID string,
	rows []SharedProjectionIntentRow,
	writtenRows int,
	writtenGroups int,
	startedAt time.Time,
	timing PartitionProcessResult,
) {
	duration := time.Since(startedAt).Seconds()
	if r.Instruments != nil {
		attrs := metric.WithAttributes(telemetry.AttrDomain(DomainRepoDependency))
		r.Instruments.CanonicalWriteDuration.Record(ctx, duration, attrs)
		r.Instruments.CanonicalWrites.Add(ctx, int64(writtenRows), attrs)
		recordRepoDependencyStepDurations(ctx, r.Instruments, timing)
	}
	if r.Logger != nil {
		r.Logger.InfoContext(
			ctx,
			"repo dependency projection cycle completed",
			slog.String(telemetry.LogKeyAcceptanceUnitID, acceptanceUnitID),
			slog.Int("written_rows", writtenRows),
			slog.Int("written_groups", writtenGroups),
			slog.Int("processed_intents", timing.ProcessedIntents),
			slog.Int("active_intents", timing.ActiveIntents),
			slog.Int("stale_intents", timing.StaleIntents),
			slog.Int("acceptance_unit_rows", timing.AcceptanceUnitRows),
			slog.Int("replay_requests", timing.ReplayRequests),
			slog.Int("active_generations", len(uniqueGenerationIDs(rows))),
			slog.Float64("duration_seconds", duration),
			slog.Float64("processing_duration_seconds", timing.ProcessingDurationSeconds),
			slog.Float64("selection_duration_seconds", timing.SelectionDurationSeconds),
			slog.Float64("load_all_duration_seconds", timing.LoadAllDurationSeconds),
			slog.Float64("acceptance_prefetch_duration_seconds", timing.AcceptancePrefetchDurationSeconds),
			slog.Float64("retract_duration_seconds", timing.RetractDurationSeconds),
			slog.Float64("write_duration_seconds", timing.WriteDurationSeconds),
			slog.Float64("replay_duration_seconds", timing.ReplayDurationSeconds),
			slog.Float64("mark_completed_duration_seconds", timing.MarkCompletedDurationSeconds),
			slog.Float64("lease_claim_duration_seconds", timing.LeaseClaimDurationSeconds),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

func recordRepoDependencyStepDurations(
	ctx context.Context,
	instruments *telemetry.Instruments,
	timing PartitionProcessResult,
) {
	steps := []struct {
		phase    string
		duration float64
	}{
		{phase: "selection", duration: timing.SelectionDurationSeconds},
		{phase: "load_all", duration: timing.LoadAllDurationSeconds},
		{phase: "acceptance_prefetch", duration: timing.AcceptancePrefetchDurationSeconds},
		{phase: "retract", duration: timing.RetractDurationSeconds},
		{phase: "write", duration: timing.WriteDurationSeconds},
		{phase: "replay", duration: timing.ReplayDurationSeconds},
		{phase: "mark_completed", duration: timing.MarkCompletedDurationSeconds},
	}
	for _, step := range steps {
		if step.duration <= 0 {
			continue
		}
		instruments.SharedProjectionStepDuration.Record(
			ctx,
			step.duration,
			metric.WithAttributes(
				telemetry.AttrDomain(DomainRepoDependency),
				telemetry.AttrWritePhase(step.phase),
				telemetry.AttrOutcome("completed"),
			),
		)
	}
}

func (r *RepoDependencyProjectionRunner) recordRepoDependencyCycleFailure(ctx context.Context, err error, duration float64) {
	if r.Logger == nil {
		return
	}
	failureClass := "repo_dependency_projection_cycle_error"
	if IsRetryable(err) {
		failureClass = "repo_dependency_projection_retryable"
	}
	logAttrs := make([]any, 0, 6)
	for _, attr := range telemetry.DomainAttrs(string(DomainRepoDependency), "") {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(logAttrs,
		slog.Float64("duration_seconds", duration),
		slog.Bool("retryable", IsRetryable(err)),
		slog.String("error", err.Error()),
		telemetry.FailureClassAttr(failureClass),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
	r.Logger.ErrorContext(ctx, "repo dependency projection cycle failed", logAttrs...)
}

func (r *RepoDependencyProjectionRunner) validate() error {
	if r.IntentReader == nil {
		return errors.New("repo dependency projection runner: intent reader is required")
	}
	if r.LeaseManager == nil {
		return errors.New("repo dependency projection runner: lease manager is required")
	}
	if r.EdgeWriter == nil {
		return errors.New("repo dependency projection runner: edge writer is required")
	}
	if r.AcceptedGen == nil {
		return errors.New("repo dependency projection runner: accepted generation lookup is required")
	}
	return nil
}

func (r *RepoDependencyProjectionRunner) wait(ctx context.Context, interval time.Duration) error {
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

func repoDependencyPollBackoff(base time.Duration, consecutiveEmpty int) time.Duration {
	backoff := base
	for i := 1; i < consecutiveEmpty && i < 4; i++ {
		backoff *= 2
	}
	if backoff > maxRepoDependencyPollInterval {
		backoff = maxRepoDependencyPollInterval
	}
	return backoff
}

type workloadMaterializationReplayRequest struct {
	scopeID      string
	generationID string
	entityKey    string
}

func (r *RepoDependencyProjectionRunner) replayWorkloadMaterialization(
	ctx context.Context,
	rows []SharedProjectionIntentRow,
) (int, error) {
	requestCount := 0
	for _, request := range repoDependencyReplayRequests(rows) {
		requestCount++
		if _, err := r.WorkloadMaterializationReplayer.ReplayWorkloadMaterialization(
			ctx,
			request.scopeID,
			request.generationID,
			request.entityKey,
		); err != nil {
			return requestCount, err
		}
	}
	return requestCount, nil
}

func repoDependencyAcceptanceUnitID(row SharedProjectionIntentRow) (string, bool) {
	if value := strings.TrimSpace(row.AcceptanceUnitID); value != "" {
		return value, true
	}
	if key, ok := row.AcceptanceKey(); ok && strings.TrimSpace(key.AcceptanceUnitID) != "" {
		return strings.TrimSpace(key.AcceptanceUnitID), true
	}
	if value := strings.TrimSpace(row.RepositoryID); value != "" {
		return value, true
	}
	return "", false
}

func repoDependencyReplayRequests(rows []SharedProjectionIntentRow) []workloadMaterializationReplayRequest {
	seen := make(map[string]struct{}, len(rows))
	requests := make([]workloadMaterializationReplayRequest, 0, len(rows))
	for _, row := range rows {
		scopeID := strings.TrimSpace(row.ScopeID)
		generationID := strings.TrimSpace(row.GenerationID)
		entityKey := repoDependencyReplayEntityKey(row)
		if scopeID == "" || generationID == "" || entityKey == "" {
			continue
		}
		key := scopeID + "|" + generationID + "|" + entityKey
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		requests = append(requests, workloadMaterializationReplayRequest{
			scopeID:      scopeID,
			generationID: generationID,
			entityKey:    entityKey,
		})
	}
	sort.Slice(requests, func(i, j int) bool {
		if requests[i].scopeID != requests[j].scopeID {
			return requests[i].scopeID < requests[j].scopeID
		}
		if requests[i].generationID != requests[j].generationID {
			return requests[i].generationID < requests[j].generationID
		}
		return requests[i].entityKey < requests[j].entityKey
	})
	return requests
}

func repoDependencyReplayEntityKey(row SharedProjectionIntentRow) string {
	if strings.EqualFold(
		strings.TrimSpace(repoDependencyPayloadString(row, "relationship_type")),
		"PROVISIONS_DEPENDENCY_FOR",
	) {
		if targetRepoID := strings.TrimSpace(repoDependencyPayloadString(row, "target_repo_id")); targetRepoID != "" {
			return repoDependencyReplayRepoKey(targetRepoID)
		}
	}

	repoID := strings.TrimSpace(row.RepositoryID)
	if repoID == "" {
		repoID = strings.TrimSpace(repoDependencyPayloadString(row, "repo_id"))
	}
	if repoID == "" {
		repoID = strings.TrimSpace(row.AcceptanceUnitID)
	}
	if repoID == "" {
		return ""
	}
	return repoDependencyReplayRepoKey(repoID)
}

// repoDependencyReplayRepoKey normalizes repository identifiers to the
// workload-materialization entity-key form used by reducer intents.
func repoDependencyReplayRepoKey(repoID string) string {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(repoID), "repo:") {
		return repoID
	}
	if alias := normalizedEntityKey(repoID); alias != "" {
		return "repo:" + alias
	}
	return "repo:" + repoID
}

func buildRepoDependencyRetractRows(repositoryIDs []string) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{
			RepositoryID: repositoryID,
			Payload:      map[string]any{"repo_id": repositoryID},
		})
	}
	return rows
}

func groupRepoDependencyUpsertRows(rows []SharedProjectionIntentRow) map[string][]SharedProjectionIntentRow {
	groups := make(map[string][]SharedProjectionIntentRow)
	for _, row := range rows {
		if !isRepoDependencyUpsertRow(row) {
			continue
		}
		source := repoDependencyRowEvidenceSource(row)
		groups[source] = append(groups[source], row)
	}
	return groups
}

func isRepoDependencyUpsertRow(row SharedProjectionIntentRow) bool {
	if row.Payload == nil {
		return false
	}
	action := strings.TrimSpace(repoDependencyPayloadString(row, "action"))
	if action == "delete" || action == "retract" {
		return false
	}

	repoID := strings.TrimSpace(repoDependencyPayloadString(row, "repo_id"))
	if repoID == "" {
		repoID = strings.TrimSpace(row.RepositoryID)
	}
	if repoID == "" {
		return false
	}
	if relationshipType := strings.TrimSpace(repoDependencyPayloadString(row, "relationship_type")); relationshipType == "RUNS_ON" {
		return strings.TrimSpace(repoDependencyPayloadString(row, "platform_id")) != ""
	}
	return strings.TrimSpace(repoDependencyPayloadString(row, "target_repo_id")) != ""
}

func repoDependencyEvidenceSources(rows []SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	sources := make([]string, 0, len(rows))
	for _, row := range rows {
		source := repoDependencyRowEvidenceSource(row)
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}

func repoDependencyRowEvidenceSource(row SharedProjectionIntentRow) string {
	if source := strings.TrimSpace(repoDependencyPayloadString(row, "evidence_source")); source != "" {
		return source
	}
	return defaultEvidenceSource
}

func repoDependencyPayloadString(row SharedProjectionIntentRow, key string) string {
	if row.Payload == nil {
		return ""
	}
	value, ok := row.Payload[key]
	if !ok {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(str)
}

func uniqueGenerationIDs(rows []SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		generationID := strings.TrimSpace(row.GenerationID)
		if generationID == "" {
			continue
		}
		if _, ok := seen[generationID]; ok {
			continue
		}
		seen[generationID] = struct{}{}
		ids = append(ids, generationID)
	}
	sort.Strings(ids)
	return ids
}
