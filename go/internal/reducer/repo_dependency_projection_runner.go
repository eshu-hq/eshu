// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer //nolint:filelength // 524 lines (base 458): the #7853 absent-acceptance accumulation, drain closure, and cap-rescan in selectAcceptanceUnitWork close over the scan loop's locals; extracting them would split one scan's progress proof across files.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/projection"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/platformfam"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
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
	ListPendingDomainIntents(ctx context.Context, domain string, limit int) ([]sharedintent.Row, error)
	ListAcceptanceUnitDomainIntents(ctx context.Context, acceptanceUnitID, domain string, limit int) ([]sharedintent.Row, error)
	// MarkIntentsCompleted marks intents completed and returns the rows
	// matched, so the caller can log a batch whose rows vanished mid-cycle
	// instead of reporting silent success (#7853).
	MarkIntentsCompleted(ctx context.Context, intentIDs []string, completedAt time.Time) (int64, error)
}

// RepoDependencyProjectionRunner processes repo-dependency shared intents one
// source repository at a time so repo-wide retractions cannot race with
// partition-sliced snapshots.
type RepoDependencyProjectionRunner struct {
	IntentReader                    RepoDependencyProjectionIntentReader
	LeaseManager                    sharedintent.PartitionLeaseManager
	AcceptanceUnitGate              RepoDependencyAcceptanceUnitGate
	EdgeWriter                      sharedintent.EdgeWriter
	WorkloadMaterializationReplayer platformfam.WorkloadMaterializationReplayer
	// WorkloadReadinessPrefetch reports whether RUNS_ON endpoint materialization
	// committed before the repo-dependency acceptance unit may write its edges.
	WorkloadReadinessPrefetch gpphase.ReadinessPrefetch
	AcceptedGen               sharedintent.AcceptedGenerationLookup
	AcceptedGenPrefetch       sharedintent.AcceptedGenerationPrefetch
	// GenerationFreshness reports whether a replay request's generation is
	// still its scope's active generation (#7670). A generation retired after
	// its acceptance row was written has nothing left to replay, so the runner
	// skips it instead of failing the cycle. Nil keeps every request on the
	// replayer, which then fails closed on an unscheduled replay, including a
	// superseded stable item it cannot prove retired.
	GenerationFreshness GenerationFreshnessCheck
	// CanonicalQuiescence holds the lane until every code scope's active
	// generation has committed canonical nodes (#6184). The lane's artifact
	// and edge writes MATCH Repository nodes from both the source and
	// target repos, so an intent drained before either side commits is lost
	// silently. Nil preserves the pre-#6184 behavior.
	CanonicalQuiescence CanonicalCodeQuiescenceChecker
	Config              RepoDependencyProjectionRunnerConfig
	Wait                func(context.Context, time.Duration) error

	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Logger      *slog.Logger

	// leaseContended remembers that the last claim was refused because another
	// owner holds this partition, so contention is logged once per episode
	// rather than once per poll. Each fixed shard copies the runner and owns
	// its own flag; only the runner's own goroutine touches it.
	leaseContended bool
}

// Run drains repo-dependency work until the context is canceled.
func (r *RepoDependencyProjectionRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	return runRepoDependencyProjection(ctx, r)
}

func (r *RepoDependencyProjectionRunner) runSerial(ctx context.Context) error {
	consecutiveEmpty := 0
	for {
		if ctx.Err() != nil {
			return nil
		}

		cycleStart := time.Now()
		result, err := r.runOneCycle(ctx)
		if err != nil {
			if errors.Is(err, errRepoDependencyShutdown) {
				// Shutdown before any mutation (#6747): the cycle already
				// released its lease and there is nothing to quarantine. A
				// shutdown inside the acceptance-unit gate arrives as a
				// quarantine instead and is recorded below like any other.
				return nil
			}
			consecutiveEmpty++
			r.recordRepoDependencyCycleFailure(ctx, err, time.Since(cycleStart).Seconds())
			delay := repoDependencyQuarantineDelay(
				err,
				repoDependencyPollBackoff(r.Config.pollInterval(), consecutiveEmpty),
			)
			if err := r.wait(ctx, delay); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("wait for repo dependency work: %w", err)
			}
			continue
		}
		if result.ProcessedIntents > 0 {
			consecutiveEmpty = 0
			continue
		}
		// A quiescence-blocked cycle is neither work nor idleness: the lane
		// is held shut by the canonical-code gate, so reset the empty
		// backoff and re-poll at the base interval instead of backing off
		// into a sleep that delays post-quiescence recovery. Mirrors the
		// code-call lane's BlockedReadiness branch.
		if result.BlockedReadiness > 0 {
			consecutiveEmpty = 0
			if err := r.wait(ctx, r.Config.pollInterval()); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("wait for repo dependency readiness: %w", err)
			}
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

func (r *RepoDependencyProjectionRunner) runOneCycle(ctx context.Context) (worker.PartitionProcessResult, error) {
	result, err := r.processOnce(ctx, time.Now().UTC())
	if err != nil {
		return worker.PartitionProcessResult{}, err
	}
	return result, nil
}

func (r *RepoDependencyProjectionRunner) processOnce(ctx context.Context, now time.Time) (worker.PartitionProcessResult, error) {
	cycleStart := time.Now()
	claimStart := time.Now()
	result := worker.PartitionProcessResult{}
	// #6184: hold the whole lane while any code scope's canonical nodes are
	// uncommitted, before claiming the lease. See CanonicalCodeQuiescenceChecker.
	if r.CanonicalQuiescence != nil {
		gateStart := time.Now()
		uncommitted, err := r.CanonicalQuiescence.HasUncommittedCanonicalCodeScopes(ctx)
		if err != nil {
			return worker.PartitionProcessResult{}, fmt.Errorf("check canonical code quiescence: %w", err)
		}
		if r.Instruments != nil && r.Instruments.SharedProjectionLaneGateDuration != nil {
			r.Instruments.SharedProjectionLaneGateDuration.Record(ctx, time.Since(gateStart).Seconds(), metric.WithAttributes(
				telemetry.AttrDomain(reducercontract.DomainRepoDependency),
				telemetry.AttrReason(projection.BlockedReasonCanonicalCodeQuiescence),
			))
		}
		if uncommitted {
			r.recordRepoDependencyQuiescenceBlocked(ctx, cycleStart)
			return worker.PartitionProcessResult{BlockedReadiness: 1}, nil
		}
	}
	claimed, err := r.LeaseManager.ClaimPartitionLease(
		ctx,
		reducercontract.DomainRepoDependency,
		r.Config.partitionID(),
		r.Config.partitionCount(),
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
		if ctx.Err() != nil {
			// The claim statement may have committed before the cancellation
			// reached the client; release through a context that survives
			// shutdown so the lease cannot outlive this process (#6747).
			r.releasePartitionLease(ctx)
			return worker.PartitionProcessResult{}, repoDependencyShutdownError(ctx)
		}
		return worker.PartitionProcessResult{}, fmt.Errorf("claim repo dependency lease: %w", err)
	}
	if !claimed {
		r.recordRepoDependencyLeaseContended(ctx)
		return worker.PartitionProcessResult{LeaseAcquired: false}, nil
	}
	r.recordRepoDependencyLeaseAcquired(ctx)
	// releaseLease stays false on a quarantined error on purpose: the
	// fail-closed pause holds the partition for the lease TTL. failCycle
	// flips it for shutdown, and releasePartitionLease runs on a context
	// that survives the cancellation.
	releaseLease := false
	defer func() {
		if releaseLease {
			r.releasePartitionLease(ctx)
		}
	}()
	cycleCtx, cancelCycle := context.WithTimeout(ctx, r.Config.cycleTimeout())
	defer cancelCycle()
	workCtx, stopHeartbeat := r.startLeaseHeartbeat(cycleCtx)
	defer func() { _ = stopHeartbeat() }()

	selectionStart := time.Now()
	acceptanceUnitID, selectionDrained, selectionTornIDs, err := r.selectAcceptanceUnitWork(workCtx, now)
	result.SelectionDurationSeconds = time.Since(selectionStart).Seconds()
	if err != nil {
		result.LeaseAcquired = true
		return result, r.failCycle(ctx, err, &releaseLease)
	}
	// The selection drain (#7853) already marked these completed and recorded
	// its metric; the counts ride the result while processAcceptanceUnit adds
	// the selected unit's own drain counts. The selection's torn ids ride
	// the exclusion so each torn row WARNs and counts once per cycle (#8509).
	result.AbsentGenerationGoneIntents = selectionDrained
	result.AbsentAcceptanceTornWrites = len(selectionTornIDs)
	reportedTorn := make(map[string]struct{}, len(selectionTornIDs))
	for _, id := range selectionTornIDs {
		reportedTorn[id] = struct{}{}
	}
	if acceptanceUnitID == "" {
		result.LeaseAcquired = true
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			return result, r.failCycle(ctx, heartbeatErr, &releaseLease)
		}
		releaseLease = true
		return result, nil
	}
	if r.AcceptanceUnitGate == nil {
		result.LeaseAcquired = true
		return result, r.failCycle(ctx, errors.New("repo dependency projection runner: acceptance unit gate is required"), &releaseLease)
	}

	var (
		processed     worker.PartitionProcessResult
		active        []sharedintent.Row
		writtenGroups int
	)
	ran, gateErr := r.AcceptanceUnitGate.WithAcceptanceUnit(
		workCtx,
		RepoDependencyAcceptanceUnitGateKey{
			Domain:           reducercontract.DomainRepoDependency,
			AcceptanceUnitID: acceptanceUnitID,
			PartitionID:      r.Config.partitionID(),
			PartitionCount:   r.Config.partitionCount(),
			LeaseOwner:       r.Config.leaseOwner(),
		},
		func(gateCtx context.Context, reader RepoDependencyProjectionIntentReader) error {
			var processErr error
			processed, active, writtenGroups, processErr = r.processAcceptanceUnit(
				gateCtx, now, acceptanceUnitID, reader, result, cycleStart, reportedTorn,
			)
			return processErr
		},
	)
	// From here on the gate has opened: a cancelled graph write or an
	// ambiguous Postgres commit may still be settling, so even a shutdown
	// keeps the quarantine and its lease (evidence-5122 safety proof); the
	// prompt release above is only for the phases before this point.
	if gateErr != nil {
		return processed, r.quarantineLease(gateErr)
	}
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		return processed, r.quarantineLease(heartbeatErr)
	}
	if !ran {
		result.LeaseAcquired = true
		releaseLease = true
		return result, nil
	}
	if len(active) > 0 {
		r.recordRepoDependencyCycle(ctx, acceptanceUnitID, active, processed.UpsertedRows, writtenGroups, cycleStart, processed)
	}
	releaseLease = true
	return processed, nil
}

func (r *RepoDependencyProjectionRunner) quarantineLease(err error) error {
	if err == nil {
		return nil
	}
	return &repoDependencyLeaseQuarantineError{delay: r.Config.leaseTTL(), cause: err}
}

func repoDependencyNeedsRetract(rows []sharedintent.Row, staleIDs []string) bool {
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

func (r *RepoDependencyProjectionRunner) selectAcceptanceUnitWork(ctx context.Context, now time.Time) (string, int, []string, error) {
	scanLimit := r.Config.batchLimit()
	if scanLimit > maxRepoDependencyAcceptanceScanLimit {
		scanLimit = maxRepoDependencyAcceptanceScanLimit
	}

	// absentRows accumulates the pending rows skipped for absent acceptance
	// across widen rounds, keyed by intent id. The #7853 drain below marks
	// the generation-gone subset completed and deletes them from this map,
	// so a rescan after a cap-continue never re-marks them.
	absentRows := make(map[string]sharedintent.Row)
	absentDrained := 0
	// reportedTorn accumulates the torn-write ids already WARNed and
	// counted this selection, and tornIDs carries them to the exit paths
	// and on to the unit drain so each torn row WARNs and counts once per
	// cycle (#8509). Kept rows stay in absentRows across cap-continue
	// rescans, so without the exclusion each rescan would re-report them.
	reportedTorn := make(map[string]struct{})
	var tornIDs []string
	acceptanceTelemetry := worker.AcceptanceTelemetry{Instruments: r.Instruments, Logger: r.Logger}
	// drainAbsent marks the accumulated absent-acceptance rows whose scope
	// generation is gone (#7853). It runs at most once per exit path, so a
	// call costs one generation probe per window that reaches an exit plus
	// one per cap-continue, each making permanent progress; a call with no
	// absent rows costs no query.
	drainAbsent := func() ([]string, []string, error) {
		drained, kept, err := worker.DrainAbsentAcceptance(
			ctx, now, acceptanceTelemetry,
			"repo_dependency_projection", reducercontract.DomainRepoDependency, r.IntentReader, absentRows, reportedTorn,
		)
		if err != nil {
			return nil, nil, err
		}
		absentDrained += len(drained)
		for _, id := range kept {
			if _, ok := reportedTorn[id]; !ok {
				reportedTorn[id] = struct{}{}
				tornIDs = append(tornIDs, id)
			}
		}
		sort.Strings(tornIDs)
		return drained, tornIDs, nil
	}

	for {
		pending, err := r.IntentReader.ListPendingDomainIntents(ctx, reducercontract.DomainRepoDependency, scanLimit)
		if err != nil {
			return "", 0, nil, fmt.Errorf("list pending repo dependency intents: %w", err)
		}
		if len(pending) == 0 {
			// An empty rescan after a cap-continue drain means the drain
			// took the whole window: report what it drained. Cumulative
			// torn ids ride along so the unit drain skips them.
			return "", absentDrained, tornIDs, nil
		}

		lookup := r.AcceptedGen
		if r.AcceptedGenPrefetch != nil {
			resolvedLookup, err := r.AcceptedGenPrefetch(ctx, pending)
			if err != nil {
				return "", 0, nil, fmt.Errorf("prefetch accepted generations: %w", err)
			}
			lookup = resolvedLookup
		}

		acceptedByUnit := make(map[string]bool, len(pending))
		order := make([]string, 0, len(pending))
		for _, row := range pending {
			unitID, ok := repoDependencyAcceptanceUnitID(row)
			if !ok {
				return "", 0, nil, fmt.Errorf("pending repo dependency intent %q is missing acceptance unit", row.IntentID)
			}
			if _, seen := acceptedByUnit[unitID]; !seen {
				order = append(order, unitID)
				acceptedByUnit[unitID] = false
			}
			key, ok := row.AcceptanceKey()
			if !ok {
				return "", 0, nil, fmt.Errorf(
					"pending repo dependency intent %q is missing scope, acceptance unit, or source run",
					row.IntentID,
				)
			}
			acceptedGeneration, ok := lookup(key)
			if !ok {
				// Absent acceptance: accumulate for the #7853 drain at the
				// exit paths. Keyless rows already errored above, so every
				// row here carries a key.
				absentRows[row.IntentID] = row
				continue
			}
			if strings.TrimSpace(row.GenerationID) == strings.TrimSpace(acceptedGeneration) {
				acceptedByUnit[unitID] = true
			}
		}

		for _, unitID := range order {
			if acceptedByUnit[unitID] {
				// Drain the generation-gone orphans scanned on the way to
				// this unit before returning it; a drain error fails the
				// cycle rather than dropping or keeping rows.
				_, kept, err := drainAbsent()
				if err != nil {
					return "", 0, nil, err
				}
				return unitID, absentDrained, kept, nil
			}
		}

		if len(pending) < scanLimit {
			// Drain the generation-gone orphans in this final window before
			// reporting the miss, so the next cycle scans past them.
			_, kept, err := drainAbsent()
			if err != nil {
				return "", 0, nil, err
			}
			return "", absentDrained, kept, nil
		}
		if scanLimit >= maxRepoDependencyAcceptanceScanLimit {
			// At the cap with absent rows, drain the generation-gone subset
			// and rescan instead of erroring: the drained window advancing
			// is progress, and the healthy unit past it becomes selectable.
			// Only a cap with nothing newly drainable still errors; the
			// per-call (not cumulative) count is what proves progress and
			// keeps a torn-write backlog from rescanning forever.
			newlyDrained, _, err := drainAbsent()
			if err != nil {
				return "", 0, nil, err
			}
			if len(newlyDrained) > 0 {
				continue
			}
			return "", 0, nil, fmt.Errorf(
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

func (r *RepoDependencyProjectionRunner) loadAllAcceptanceUnitIntents(
	ctx context.Context,
	reader RepoDependencyProjectionIntentReader,
	acceptanceUnitID string,
) ([]sharedintent.Row, error) {
	limit := r.Config.batchLimit()
	if limit > maxRepoDependencyAcceptanceScanLimit {
		limit = maxRepoDependencyAcceptanceScanLimit
	}
	for {
		rows, err := reader.ListAcceptanceUnitDomainIntents(ctx, acceptanceUnitID, reducercontract.DomainRepoDependency, limit)
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

func (r *RepoDependencyProjectionRunner) retractRepo(ctx context.Context, rows []sharedintent.Row) (int, error) {
	retractRows := buildRepoDependencyRetractRows(sharedintent.UniqueRepositoryIDs(rows))
	if len(retractRows) == 0 {
		return 0, nil
	}
	sources := repoDependencyEvidenceSources(rows)
	for _, source := range sources {
		if err := r.EdgeWriter.RetractEdges(ctx, reducercontract.DomainRepoDependency, retractRows, source); err != nil {
			return 0, fmt.Errorf("retract repo dependency edges for %s: %w", source, err)
		}
	}
	return len(retractRows) * len(sources), nil
}

func (r *RepoDependencyProjectionRunner) writeActiveRows(ctx context.Context, rows []sharedintent.Row) (int, int, error) {
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
		// #5984: this handler has no unroutable-intent sink wired, so the write
		// report is deliberately discarded here and the rejected rows remain
		// visible only through the writer's WARN and
		// eshu_dp_shared_edge_unroutable_rows_total counter. That is unchanged
		// behaviour for this path, not a new loss -- but it IS a remaining gap,
		// and the compile-forced report is what makes it visible instead of
		// implicit. Wiring the sink here needs this handler's own proof.
		if _, err := r.EdgeWriter.WriteEdges(ctx, reducercontract.DomainRepoDependency, group, source); err != nil {
			return 0, 0, fmt.Errorf("write repo dependency edges for %s: %w", source, err)
		}
		writtenRows += len(group)
	}
	return writtenRows, len(sources), nil
}
