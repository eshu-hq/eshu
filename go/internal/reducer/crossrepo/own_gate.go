// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"fmt"
	"slices"
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
// defer forever. When the producer later activates the relationship
// generation, the activation transaction reopens the consumer
// (RelationshipStore in internal/storage/postgres).
const DeployableUnitCorrelationProducerDeadLetteredFailureClass = "deployable_unit_correlation_producer_dead_lettered"

// WorkloadMaterializationProducerDeadLetteredFailureClass is the
// workload_materialization twin of
// DeployableUnitCorrelationProducerDeadLetteredFailureClass (#7597).
const WorkloadMaterializationProducerDeadLetteredFailureClass = "workload_materialization_producer_dead_lettered"

// DeployableUnitCorrelationProducerMissingFailureClass classifies a
// deployable_unit_correlation pass whose scope generation has had neither a
// deployment_mapping work item nor a relationship_generations row for longer
// than crossscope.ProducerReadinessMaxWait (#7597). Counting, like the
// dead-lettered class, and distinct from retry_exhausted in a dead letter.
const DeployableUnitCorrelationProducerMissingFailureClass = "deployable_unit_correlation_producer_missing"

// WorkloadMaterializationProducerMissingFailureClass is the
// workload_materialization twin of
// DeployableUnitCorrelationProducerMissingFailureClass (#7597).
const WorkloadMaterializationProducerMissingFailureClass = "workload_materialization_producer_missing"

// ownGateClasses names the two counting classes of one own-gate consumer.
type ownGateClasses struct {
	deadLettered string
	missing      string
}

// ownGateClassesByDomain is the single source of the own-gate consumers and
// their counting classes. A domain outside this map never counts.
var ownGateClassesByDomain = map[reducercontract.Domain]ownGateClasses{
	reducercontract.DomainDeployableUnitCorrelation: {
		deadLettered: DeployableUnitCorrelationProducerDeadLetteredFailureClass,
		missing:      DeployableUnitCorrelationProducerMissingFailureClass,
	},
	reducercontract.DomainWorkloadMaterialization: {
		deadLettered: WorkloadMaterializationProducerDeadLetteredFailureClass,
		missing:      WorkloadMaterializationProducerMissingFailureClass,
	},
}

// ProducerDeadLetteredFailureClasses returns every producer-dead-lettered
// class, sorted, derived from ownGateClassesByDomain. The Postgres activation
// wake selects consumer rows by exactly this set, and the queue's enrollment
// tests pin that none of them is non-counting.
func ProducerDeadLetteredFailureClasses() []string {
	return sortedClasses(func(c ownGateClasses) string { return c.deadLettered })
}

// ProducerMissingFailureClasses returns every producer-missing class, sorted,
// derived from ownGateClassesByDomain.
func ProducerMissingFailureClasses() []string {
	return sortedClasses(func(c ownGateClasses) string { return c.missing })
}

// OwnGateConsumerDomains returns the own-gate consumer domains, sorted.
func OwnGateConsumerDomains() []reducercontract.Domain {
	domains := make([]reducercontract.Domain, 0, len(ownGateClassesByDomain))
	for domain := range ownGateClassesByDomain {
		domains = append(domains, domain)
	}
	slices.Sort(domains)
	return domains
}

func sortedClasses(pick func(ownGateClasses) string) []string {
	classes := make([]string, 0, len(ownGateClassesByDomain))
	for _, c := range ownGateClassesByDomain {
		classes = append(classes, pick(c))
	}
	slices.Sort(classes)
	return classes
}

// ProducerState is the newest deployment_mapping work item of one scope
// generation, plus the scope's current active generation and the
// generation's relationship_generations status. WorkItemID and Status are
// empty when the generation has no deployment_mapping row.
type ProducerState struct {
	// ScopeActiveGenerationID is ingestion_scopes.active_generation_id, empty
	// when the scope row is missing or has no active generation.
	ScopeActiveGenerationID string
	WorkItemID              string
	Status                  string
	FailureClass            string
	// RelationshipGenerationStatus is relationship_generations.status for the
	// intent's generation, empty when no row exists. A row that exists and is
	// not active means re-resolution is in progress (a refinalize retired it).
	RelationshipGenerationStatus string
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
//   - no producer row but a relationship_generations row that is not active
//     (re-resolution in progress, e.g. after a refinalize): plain deferral,
//     no time bound;
//   - neither a producer row nor a relationship generation row: deferral
//     bounded by crossscope.ProducerReadinessMaxWait from
//     crossscope.ReadinessCycleAnchor, then ProducerMissingError;
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
			"scope active generation is %q, not this generation; if this generation is older, the claim path retires it",
			state.ScopeActiveGenerationID,
		)}
	}
	classes, counted := ownGateClassesByDomain[intent.Domain]
	switch {
	case state.Status == "dead_letter" || state.Status == "failed":
		if !counted {
			return OwnGateVerdict{ProducerDetail: state.detail()}
		}
		return OwnGateVerdict{Err: ProducerDeadLetteredError{
			failureClass: classes.deadLettered, consumer: intent, producer: state,
		}}
	case state.Status != "":
		return OwnGateVerdict{ProducerDetail: state.detail()}
	case state.RelationshipGenerationStatus != "":
		return OwnGateVerdict{ProducerDetail: fmt.Sprintf(
			"no deployment_mapping work item yet and relationship generation status=%s; re-resolution in progress",
			state.RelationshipGenerationStatus,
		)}
	case !counted:
		return OwnGateVerdict{ProducerDetail: "no deployment_mapping work item exists for this generation yet"}
	default:
		return missingProducerVerdict(intent, classes.missing, now)
	}
}

// missingProducerVerdict defers while the repair cycle is younger than the
// bound and counts once it is not. A zero anchor means elapsed time is
// unknown, not infinite, so it keeps deferring (see
// crossscope.ReadinessCycleAnchor).
func missingProducerVerdict(intent reducercontract.Intent, class string, now time.Time) OwnGateVerdict {
	anchor := crossscope.ReadinessCycleAnchor(intent)
	if anchor.IsZero() || now.Sub(anchor) < crossscope.ProducerReadinessMaxWait {
		return OwnGateVerdict{ProducerDetail: "no deployment_mapping work item or relationship generation exists for this generation yet"}
	}
	return OwnGateVerdict{Err: ProducerMissingError{failureClass: class, consumer: intent, elapsed: now.Sub(anchor)}}
}

// detail renders the producer evidence appended to a deferral message.
func (s ProducerState) detail() string {
	return fmt.Sprintf("deployment_mapping producer %s status=%s failure_class=%s",
		s.WorkItemID, s.Status, s.FailureClass)
}

// ProducerDeadLetteredError fails an own-gate pass whose deployment_mapping
// producer is dead-lettered (or legacy failed). Retryable() keeps it on the ordinary counted
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
		"own relationship generation for scope %s generation %s cannot activate while deployment_mapping producer %s is %s (failure_class=%s); counting this %s failure toward the retry budget; replay the producer, and its activation reopens this item",
		e.consumer.ScopeID, e.consumer.GenerationID, e.producer.WorkItemID, e.producer.Status,
		e.producer.FailureClass, e.consumer.Domain,
	)
}

// Retryable opts the failure into the normal counted queue retries.
func (ProducerDeadLetteredError) Retryable() bool { return true }

// FailureClass returns the consumer domain's producer-terminal class.
func (e ProducerDeadLetteredError) FailureClass() string { return e.failureClass }

// ProducerMissingError fails an own-gate pass whose scope generation has had
// neither a deployment_mapping row nor a relationship_generations row for
// longer than crossscope.ProducerReadinessMaxWait. Retryable with an
// unenrolled *_producer_missing class, so it counts toward MaxAttempts and its
// dead letter is distinguishable from retry_exhausted.
type ProducerMissingError struct {
	failureClass string
	consumer     reducercontract.Intent
	elapsed      time.Duration
}

// Error reports the elapsed wait against the bound.
func (e ProducerMissingError) Error() string {
	return fmt.Sprintf(
		"own relationship generation for scope %s generation %s still inactive and neither a deployment_mapping work item nor a relationship generation exists after %s (bound %s); counting this %s failure toward the retry budget",
		e.consumer.ScopeID, e.consumer.GenerationID, e.elapsed.Round(time.Second),
		crossscope.ProducerReadinessMaxWait, e.consumer.Domain,
	)
}

// Retryable opts the bounded failure into the normal counted queue retries.
func (ProducerMissingError) Retryable() bool { return true }

// FailureClass returns the consumer domain's producer-missing class.
func (e ProducerMissingError) FailureClass() string { return e.failureClass }
