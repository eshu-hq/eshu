// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"fmt"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// DeployableUnitCorrelationProducerDeadLetteredFailureClass classifies a
// deployable_unit_correlation pass whose own relationship generation can never
// activate, because the deployment_mapping item that would activate it is
// dead-lettered (#7597).
//
// It is deliberately NOT a non-counting class: it counts toward MaxAttempts so
// the ordinary budget dead-letters the consumer loudly instead of letting it
// defer forever. Replaying the dead-lettered producer reopens the consumer in
// the same statement (ReplayFailedWorkItems in internal/storage/postgres).
const DeployableUnitCorrelationProducerDeadLetteredFailureClass = "deployable_unit_correlation_producer_dead_lettered"

// WorkloadMaterializationProducerDeadLetteredFailureClass is the
// workload_materialization twin of
// DeployableUnitCorrelationProducerDeadLetteredFailureClass (#7597).
const WorkloadMaterializationProducerDeadLetteredFailureClass = "workload_materialization_producer_dead_lettered"

// producerDeadLetteredClassByDomain maps each own-gate consumer to its
// producer-terminal class. A domain outside this map never counts.
var producerDeadLetteredClassByDomain = map[reducercontract.Domain]string{
	reducercontract.DomainDeployableUnitCorrelation: DeployableUnitCorrelationProducerDeadLetteredFailureClass,
	reducercontract.DomainWorkloadMaterialization:   WorkloadMaterializationProducerDeadLetteredFailureClass,
}

// ProducerDeadLetteredFailureClasses returns every producer-terminal class in
// a stable order. The Postgres replay wake selects consumer rows by exactly
// this set, and the queue's enrollment tests pin that none of them is
// non-counting.
func ProducerDeadLetteredFailureClasses() []string {
	return []string{
		DeployableUnitCorrelationProducerDeadLetteredFailureClass,
		WorkloadMaterializationProducerDeadLetteredFailureClass,
	}
}

// ProducerState is the newest deployment_mapping work item of one scope
// generation, plus the scope's current active generation. WorkItemID and
// Status are empty when the generation has no deployment_mapping row.
type ProducerState struct {
	// ScopeActiveGenerationID is ingestion_scopes.active_generation_id, empty
	// when the scope row is missing or has no active generation.
	ScopeActiveGenerationID string
	WorkItemID              string
	Status                  string
	FailureClass            string
}

// ProducerLookup reads the ProducerState of one scope generation. The
// production implementation is postgres.NewResolutionProducerLookup. An error
// means the question could not be answered; it is never a readiness miss.
type ProducerLookup func(ctx context.Context, scopeID, generationID string) (ProducerState, error)

// OwnGateVerdict is the answer of EvaluateOwnGate. Ready means the pass may
// read resolved relationships. Otherwise a non-nil Err must be returned as is
// (a counting failure or a classified lookup failure), and a nil Err means the
// caller returns its unchanged non-counting deferral with ProducerDetail
// appended to the message.
type OwnGateVerdict struct {
	Ready          bool
	ProducerDetail string
	Err            error
}

// EvaluateOwnGate is the own-generation readiness gate of
// deployable_unit_correlation and workload_materialization (#6184), plus the
// #7597 producer check that bounds it.
//
// The gate is vacuous without candidates and open when activeLookup is nil.
// An activeLookup error keeps today's plain deferral and does not read the
// producer. Only when the own generation reads inactive does it read the
// newest deployment_mapping row of the same scope generation, so the extra
// read runs only in the branch that already defers:
//
//   - scope moved to another generation, or producer pending, claimed,
//     running, retrying, superseded, succeeded: plain deferral, no time bound
//     (a live producer's own retry budget already bounds it);
//   - producer dead_letter (or legacy failed): ProducerDeadLetteredError,
//     which counts toward the retry budget;
//   - no producer row: deferral bounded by crossscope.ProducerReadinessMaxWait
//     from crossscope.ReadinessCycleAnchor, then ProducerMissingError;
//   - lookup error: factload.ClassifyFactLoadError, never a deferral.
//
// now must be the caller's clock reading for this pass.
func EvaluateOwnGate(
	ctx context.Context,
	activeLookup func(generationID string) (bool, error),
	producerLookup ProducerLookup,
	intent reducercontract.Intent,
	hasCandidates bool,
	now time.Time,
) OwnGateVerdict {
	if !hasCandidates || activeLookup == nil {
		return OwnGateVerdict{Ready: true}
	}
	active, err := activeLookup(intent.GenerationID)
	if err == nil && active {
		return OwnGateVerdict{Ready: true}
	}
	if err != nil || producerLookup == nil {
		return OwnGateVerdict{}
	}
	state, err := producerLookup(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return OwnGateVerdict{Err: factload.ClassifyFactLoadError(fmt.Errorf(
			"look up deployment_mapping producer for scope %s generation %s: %w",
			intent.ScopeID, intent.GenerationID, err,
		))}
	}
	return producerVerdict(intent, state, now)
}

// producerVerdict maps a read producer state to the gate's answer.
func producerVerdict(intent reducercontract.Intent, state ProducerState, now time.Time) OwnGateVerdict {
	if state.ScopeActiveGenerationID != intent.GenerationID {
		return OwnGateVerdict{ProducerDetail: fmt.Sprintf(
			"scope active generation is %q, not this generation; the claim path retires it",
			state.ScopeActiveGenerationID,
		)}
	}
	switch state.Status {
	case "":
		return missingProducerVerdict(intent, now)
	case "dead_letter", "failed":
		class, ok := producerDeadLetteredClassByDomain[intent.Domain]
		if !ok {
			return OwnGateVerdict{ProducerDetail: state.detail()}
		}
		return OwnGateVerdict{Err: ProducerDeadLetteredError{
			failureClass: class, consumer: intent, producer: state,
		}}
	default:
		return OwnGateVerdict{ProducerDetail: state.detail()}
	}
}

// missingProducerVerdict defers while the repair cycle is younger than the
// bound and counts once it is not. A zero anchor means elapsed time is
// unknown, not infinite, so it keeps deferring (see
// crossscope.ReadinessCycleAnchor).
func missingProducerVerdict(intent reducercontract.Intent, now time.Time) OwnGateVerdict {
	anchor := crossscope.ReadinessCycleAnchor(intent)
	if anchor.IsZero() || now.Sub(anchor) < crossscope.ProducerReadinessMaxWait {
		return OwnGateVerdict{ProducerDetail: "no deployment_mapping work item exists for this generation yet"}
	}
	return OwnGateVerdict{Err: ProducerMissingError{consumer: intent, elapsed: now.Sub(anchor)}}
}

// detail renders the producer evidence appended to a deferral message.
func (s ProducerState) detail() string {
	return fmt.Sprintf("deployment_mapping producer %s status=%s failure_class=%s",
		s.WorkItemID, s.Status, s.FailureClass)
}

// ProducerDeadLetteredError fails an own-gate pass whose deployment_mapping
// producer is dead-lettered. Retryable() keeps it on the ordinary counted
// retry path and FailureClass() returns an unenrolled class, so attempt_count
// advances and MaxAttempts dead-letters the consumer.
//
// It must not wrap or unwrap to the own-gate deferral: errors.As would find
// that error's non-counting class and the failure would silently never count
// (the #6759 rule, see deploymentSourceTargetWaitExceededError in the reducer
// root).
type ProducerDeadLetteredError struct {
	failureClass string
	consumer     reducercontract.Intent
	producer     ProducerState
}

// Error names the producer work item and its failure class.
func (e ProducerDeadLetteredError) Error() string {
	return fmt.Sprintf(
		"own relationship generation for scope %s generation %s can never activate: deployment_mapping producer %s is %s (failure_class=%s); counting this %s failure toward the retry budget, replay the producer to reopen it",
		e.consumer.ScopeID, e.consumer.GenerationID, e.producer.WorkItemID, e.producer.Status,
		e.producer.FailureClass, e.consumer.Domain,
	)
}

// Retryable opts the failure into the normal counted queue retries.
func (ProducerDeadLetteredError) Retryable() bool { return true }

// FailureClass returns the consumer domain's producer-terminal class.
func (e ProducerDeadLetteredError) FailureClass() string { return e.failureClass }

// ProducerMissingError fails an own-gate pass whose scope generation has had
// no deployment_mapping row for longer than crossscope.ProducerReadinessMaxWait.
// Retryable with no FailureClass, so it counts like the other elapsed-time
// bounds (#6759, #7268) and dead-letters under the generic triage class.
type ProducerMissingError struct {
	consumer reducercontract.Intent
	elapsed  time.Duration
}

// Error reports the elapsed wait against the bound.
func (e ProducerMissingError) Error() string {
	return fmt.Sprintf(
		"own relationship generation for scope %s generation %s still inactive and no deployment_mapping work item exists after %s (bound %s); counting this %s failure toward the retry budget",
		e.consumer.ScopeID, e.consumer.GenerationID, e.elapsed.Round(time.Second),
		crossscope.ProducerReadinessMaxWait, e.consumer.Domain,
	)
}

// Retryable opts the bounded failure into the normal counted queue retries.
func (ProducerMissingError) Retryable() bool { return true }
