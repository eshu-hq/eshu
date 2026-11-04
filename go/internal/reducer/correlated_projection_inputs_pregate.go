// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/accepted"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// deferUnlessReadyForBulkLoad evaluates the cheap readiness probes before
// LoadWorkloadProjectionScopeInputs pays for its bulk scope-local fact load
// (#9697): under a resolution storm almost every intent deferred after
// loading, so the load was pure allocation churn feeding the engine OOM
// livelock. A deferral here returns the same non-counting
// workloadMaterializationResolutionNotReadyError (or the same counting
// producer error) the post-load gates return, only earlier. The post-load
// gates still run after the load, so graph truth is unchanged: a ready intent
// loads exactly once, and a deferred intent retries on the same fence.
//
// The pre-gate fires only when the loader's candidate-precondition probe
// explicitly reports the scope generation may hold candidates. A missing
// probe, a probe error, or a false answer all proceed to the normal
// load-then-gate path, so candidate-free scopes keep their pinned vacuous
// success (no bulk load is shed for them, but none is wasted either: the
// normal path succeeds without consulting the fence). Because a probe error
// fails open, every probe call is counted and every error class is logged
// (#9986): a probe that always errors would otherwise disable the shed
// silently, which is how the #9700 syntax error went unseen.
func (l CorrelatedWorkloadProjectionInputLoader) deferUnlessReadyForBulkLoad(
	ctx context.Context,
	intent reducercontract.Intent,
) error {
	prober, ok := l.FactLoader.(factload.WorkloadCandidateProbe)
	if !ok {
		return nil
	}
	mayHold, err := l.observeCandidateProbe(ctx, prober, intent)
	if err != nil || !mayHold {
		return nil
	}
	// Own-generation probe: a single-key activation read, no bulk load. The
	// full own-gate verdict runs with assumed candidates (the probe just
	// established they may exist) so a dead-lettered or long-missing
	// producer still counts instead of deferring forever (#7597).
	if l.ResolutionActiveLookup != nil {
		active, err := l.ResolutionActiveLookup(intent.GenerationID)
		if err != nil {
			return workloadMaterializationResolutionNotReadyError{
				scopeID: intent.ScopeID, generationID: intent.GenerationID,
			}
		}
		if !active {
			gate := crossrepo.EvaluateOwnGate(ctx, l.ResolutionActiveLookup, l.ResolutionProducerLookup,
				intent, true, time.Now().UTC())
			if gate.Err != nil {
				return gate.Err
			}
			return workloadMaterializationResolutionNotReadyError{
				scopeID: intent.ScopeID, generationID: intent.GenerationID, producerDetail: gate.ProducerDetail,
			}
		}
	}
	// Corpus-wide probe: one aggregate read, no bulk load. Skipped for fused
	// loaders, whose verdict comes from the by-repos read's own statement
	// snapshot (#6740) and for which ResolutionsCompleteLookup stays
	// unconsumed by contract.
	if _, fused := l.ResolvedLoader.(CorpusFencedResolvedRelationshipLoader); !fused &&
		l.ResolutionsCompleteLookup != nil {
		complete, err := l.ResolutionsCompleteLookup(ctx)
		if err != nil {
			return workloadMaterializationResolutionNotReadyError{
				scopeID: intent.ScopeID, generationID: intent.GenerationID, cause: err,
			}
		}
		if !complete {
			return workloadMaterializationResolutionNotReadyError{
				scopeID:         intent.ScopeID,
				generationID:    intent.GenerationID,
				holdingScopeIDs: accepted.IncompleteScopeIDs(ctx, l.IncompleteScopesLookup),
			}
		}
	}
	return nil
}

// WorkloadMaterializationResolutionNotReadyFailureClass classifies a deferral
// of workload projection inputs whose own relationship generation has not
// activated yet.
//
// Registered as a non-counting reducer retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go): the load is
// waiting on upstream resolution, not failing on its own merits. Without this
// gate the loader merges the generation-pinned own-scope read with an empty
// by-repos read while the generation is retired-or-pending and workload
// materialization succeeds on that partial input, which nothing reopens
// (#6184).
const WorkloadMaterializationResolutionNotReadyFailureClass = "workload_materialization_resolution_not_ready"

// workloadMaterializationResolutionNotReadyError defers the input load until
// the scope's own relationship generation activates.
type workloadMaterializationResolutionNotReadyError struct {
	scopeID      string
	generationID string
	// cause carries the fence lookup failure when the deferral is outage
	// driven rather than backpressure driven; nil for a genuinely
	// incomplete corpus fence (#6730: a swallowed lookup error reads
	// exactly like healthy backpressure at 3 AM).
	cause error
	// holdingScopeIDs names the scopes holding the corpus fence, best
	// effort; empty when the holder lookup is unwired or failed.
	holdingScopeIDs []string
	// producerDetail names the own generation's deployment_mapping producer
	// state on an own-gate deferral (#7597); empty otherwise.
	producerDetail string
}

func (e workloadMaterializationResolutionNotReadyError) Error() string {
	msg := fmt.Sprintf(
		"cross-repo resolution not active for scope %s generation %s; deferring workload projection inputs rather than materializing against a partial resolved set",
		e.scopeID,
		e.generationID,
	)
	if e.cause != nil {
		msg += fmt.Sprintf("; corpus fence lookup failed: %v", e.cause)
	}
	if len(e.holdingScopeIDs) > 0 {
		msg += fmt.Sprintf("; holding scopes: %s", strings.Join(e.holdingScopeIDs, ","))
	}
	if e.producerDetail != "" {
		msg += "; " + e.producerDetail
	}
	return msg
}

// Unwrap exposes the fence lookup failure to errors.Is/As without changing
// the retryable failure class: the cause travels for logs, the class travels
// for the queue.
func (e workloadMaterializationResolutionNotReadyError) Unwrap() error {
	return e.cause
}

func (workloadMaterializationResolutionNotReadyError) Retryable() bool { return true }

func (workloadMaterializationResolutionNotReadyError) FailureClass() string {
	return WorkloadMaterializationResolutionNotReadyFailureClass
}

// defaultProbeErrorLog is the process-wide limiter behind the probe error
// log; the loader value is rebuilt per handler call, so the state lives here.
var defaultProbeErrorLog = newProbeErrorLogLimiter(10*time.Minute, time.Now)

// probeErrorLogLimiter admits one error log per cause class per interval and
// counts what it suppressed in between, so a probe that fails on every intent
// cannot flood the reducer log. The class set is closed (see
// telemetry.WorkloadCandidateProbeCause*), so the map stays bounded.
type probeErrorLogLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	now      func() time.Time
	classes  map[string]*probeErrorLogState
}

type probeErrorLogState struct {
	last       time.Time
	suppressed int
}

func newProbeErrorLogLimiter(interval time.Duration, now func() time.Time) *probeErrorLogLimiter {
	return &probeErrorLogLimiter{interval: interval, now: now, classes: map[string]*probeErrorLogState{}}
}

// admit reports whether this occurrence of class should be logged, and how
// many occurrences were suppressed since the previous admitted log.
func (l *probeErrorLogLimiter) admit(class string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	state, seen := l.classes[class]
	if !seen {
		l.classes[class] = &probeErrorLogState{last: now}
		return true, 0
	}
	if now.Sub(state.last) < l.interval {
		state.suppressed++
		return false, 0
	}
	suppressed := state.suppressed
	state.last, state.suppressed = now, 0
	return true, suppressed
}

// observeCandidateProbe runs the candidate-precondition probe and makes its
// outcome visible: one eshu_dp_reducer_workload_candidate_probe_total point
// per call (may_hold, no_candidates, error plus a closed cause_class) and, for
// an error, a Warn log rate limited per cause class. It returns the probe's
// own answer unchanged; the fail-open decision stays with the caller.
func (l CorrelatedWorkloadProjectionInputLoader) observeCandidateProbe(
	ctx context.Context,
	prober factload.WorkloadCandidateProbe,
	intent reducercontract.Intent,
) (bool, error) {
	mayHold, err := prober.ScopeMayHoldWorkloadCandidates(ctx, intent.ScopeID, intent.GenerationID)
	outcome, cause := telemetry.WorkloadCandidateProbeOutcomeNoCandidates, telemetry.WorkloadCandidateProbeCauseNone
	switch {
	case err != nil:
		var sqlState string
		cause, sqlState = classifyWorkloadCandidateProbeError(err)
		outcome = telemetry.WorkloadCandidateProbeOutcomeError
		l.logCandidateProbeError(ctx, intent, err, cause, sqlState)
	case mayHold:
		outcome = telemetry.WorkloadCandidateProbeOutcomeMayHold
	}
	if l.Instruments != nil && l.Instruments.ReducerWorkloadCandidateProbe != nil {
		l.Instruments.ReducerWorkloadCandidateProbe.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionOutcome, outcome),
			attribute.String("cause_class", cause),
		))
	}
	return mayHold, err
}

// logCandidateProbeError logs a probe failure once per cause class per
// interval. The log names the scope generation, the closed cause class (as
// failure_class), the SQLSTATE when the driver supplied one, and how many
// same-class failures were suppressed since the previous line.
func (l CorrelatedWorkloadProjectionInputLoader) logCandidateProbeError(
	ctx context.Context,
	intent reducercontract.Intent,
	err error,
	cause string,
	sqlState string,
) {
	limiter := l.probeErrorLog
	if limiter == nil {
		limiter = defaultProbeErrorLog
	}
	admitted, suppressed := limiter.admit(cause)
	if !admitted {
		return
	}
	logger := l.Logger
	if logger == nil {
		logger = slog.Default()
	}
	attrs := []slog.Attr{
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		telemetry.FailureClassAttr(cause),
		slog.Int("suppressed", suppressed),
		slog.String("error", err.Error()),
	}
	if sqlState != "" {
		attrs = append(attrs, slog.String("sqlstate", sqlState))
	}
	logger.LogAttrs(ctx, slog.LevelWarn,
		"workload candidate probe failed; the bulk-load shed is skipped and the normal load runs", attrs...)
}

// classifyWorkloadCandidateProbeError maps a probe failure to the closed
// telemetry.WorkloadCandidateProbeCause* set and returns the SQLSTATE when
// the error carries one (pgconn.PgError exposes SQLState()). Class 42 is a
// statement the server rejected, the #9986 shape that never self-heals.
func classifyWorkloadCandidateProbeError(err error) (cause, sqlState string) {
	var stater interface{ SQLState() string }
	if errors.As(err, &stater) {
		sqlState = stater.SQLState()
		switch {
		case strings.HasPrefix(sqlState, "42"):
			return telemetry.WorkloadCandidateProbeCauseSQLInvalid, sqlState
		case strings.HasPrefix(sqlState, "08"):
			return telemetry.WorkloadCandidateProbeCauseConnection, sqlState
		case strings.HasPrefix(sqlState, "53"), strings.HasPrefix(sqlState, "57"):
			return telemetry.WorkloadCandidateProbeCauseResource, sqlState
		}
		return telemetry.WorkloadCandidateProbeCauseOther, sqlState
	}
	switch {
	case errors.Is(err, context.Canceled):
		return telemetry.WorkloadCandidateProbeCauseCanceled, ""
	case errors.Is(err, context.DeadlineExceeded):
		return telemetry.WorkloadCandidateProbeCauseDeadline, ""
	}
	return telemetry.WorkloadCandidateProbeCauseOther, ""
}
