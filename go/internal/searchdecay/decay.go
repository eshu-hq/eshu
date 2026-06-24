// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchdecay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// EvidenceClass identifies the evidence family considered for decay scoring.
type EvidenceClass string

const (
	// EvidenceClassCIRun covers CI run evidence.
	EvidenceClassCIRun EvidenceClass = "ci_run"
	// EvidenceClassVulnerabilityObservation covers vulnerability observation evidence.
	EvidenceClassVulnerabilityObservation EvidenceClass = "vulnerability_observation"
	// EvidenceClassDeploymentEvent covers deployment event evidence.
	EvidenceClassDeploymentEvent EvidenceClass = "deployment_event"
	// EvidenceClassCloudObservation covers live cloud observation evidence.
	EvidenceClassCloudObservation EvidenceClass = "cloud_observation"
	// EvidenceClassRelationshipCandidate covers weak inferred relationship candidates.
	EvidenceClassRelationshipCandidate EvidenceClass = "relationship_candidate"
	// EvidenceClassCanonicalGraph marks canonical graph truth, which must not decay.
	EvidenceClassCanonicalGraph EvidenceClass = "canonical_graph"
	// EvidenceClassDurableRelationship marks admitted durable relationships, which must not decay.
	EvidenceClassDurableRelationship EvidenceClass = "durable_relationship"
)

// Outcome names the decision produced by one decay scoring attempt.
type Outcome string

const (
	// OutcomeApplied means decay scoring adjusted the evidence score.
	OutcomeApplied Outcome = "applied"
	// OutcomeSkippedCanonical means canonical truth or durable relationship evidence was not decayed.
	OutcomeSkippedCanonical Outcome = "skipped_canonical"
	// OutcomeSkippedIneligible means the evidence class is outside the policy scope.
	OutcomeSkippedIneligible Outcome = "skipped_ineligible"
	// OutcomeRejectedInvalid means validation rejected the policy or evidence.
	OutcomeRejectedInvalid Outcome = "rejected_invalid"
)

// Policy configures half-life decay for selected non-canonical evidence.
type Policy struct {
	ID              string
	Now             time.Time
	HalfLife        time.Duration
	MinScore        float64
	EligibleClasses []EvidenceClass
}

// Evidence is one rankable evidence item considered for decay.
type Evidence struct {
	ID         string
	Class      EvidenceClass
	TruthLevel searchdocs.TruthLevel
	ObservedAt time.Time
	Score      float64
}

// Decision records the rank metadata produced for one evidence item.
type Decision struct {
	PolicyID      string
	EvidenceClass EvidenceClass
	Outcome       Outcome
	OriginalScore float64
	Score         float64
	Age           time.Duration
	Reason        string
}

// Observation is the low-cardinality telemetry bridge for one decay decision.
type Observation struct {
	PolicyID      string
	EvidenceClass EvidenceClass
	Outcome       Outcome
}

// Observer records decay scoring decisions for later metrics, spans, or logs.
type Observer interface {
	// ObserveDecay records one decay scoring decision.
	ObserveDecay(context.Context, Observation)
}

// Scorer applies one decay policy to rankable non-canonical evidence.
type Scorer struct {
	Policy   Policy
	Observer Observer
}

// Score applies the scorer policy to one evidence item.
func (scorer Scorer) Score(ctx context.Context, evidence Evidence) (Decision, error) {
	policy := scorer.Policy.normalized()
	evidence = normalizedEvidence(evidence)
	decision := Decision{
		PolicyID:      policy.ID,
		EvidenceClass: evidence.Class,
		OriginalScore: evidence.Score,
		Score:         evidence.Score,
	}
	observe := func(decision Decision, err error) (Decision, error) {
		scorer.observe(ctx, decision)
		return decision, err
	}

	if err := validate(policy, evidence); err != nil {
		decision.Outcome = OutcomeRejectedInvalid
		decision.Reason = err.Error()
		return observe(decision, err)
	}
	decision.Age = policy.Now.Sub(evidence.ObservedAt)
	if decision.Age < 0 {
		decision.Age = 0
	}
	if isCanonicalEvidence(evidence) {
		decision.Outcome = OutcomeSkippedCanonical
		decision.Reason = "canonical evidence is not decay-scored"
		return observe(decision, nil)
	}
	if !policy.eligible(evidence.Class) {
		decision.Outcome = OutcomeSkippedIneligible
		decision.Reason = "evidence class is outside policy scope"
		return observe(decision, nil)
	}

	factor := math.Pow(0.5, float64(decision.Age)/float64(policy.HalfLife))
	decision.Score = clampScore(evidence.Score * factor)
	if decision.Score < policy.MinScore {
		decision.Score = math.Min(policy.MinScore, evidence.Score)
	}
	decision.Outcome = OutcomeApplied
	decision.Reason = "decay policy applied"
	return observe(decision, nil)
}

// DefaultEligibleClasses returns the non-canonical evidence classes decay can score.
func DefaultEligibleClasses() []EvidenceClass {
	return []EvidenceClass{
		EvidenceClassCIRun,
		EvidenceClassVulnerabilityObservation,
		EvidenceClassDeploymentEvent,
		EvidenceClassCloudObservation,
		EvidenceClassRelationshipCandidate,
	}
}

func (policy Policy) normalized() Policy {
	policy.ID = strings.TrimSpace(policy.ID)
	if policy.Now.IsZero() {
		policy.Now = time.Now().UTC()
	}
	if len(policy.EligibleClasses) == 0 {
		policy.EligibleClasses = DefaultEligibleClasses()
	}
	return policy
}

func (policy Policy) eligible(class EvidenceClass) bool {
	for _, eligible := range policy.EligibleClasses {
		if class == eligible {
			return true
		}
	}
	return false
}

func normalizedEvidence(evidence Evidence) Evidence {
	evidence.ID = strings.TrimSpace(evidence.ID)
	evidence.Class = EvidenceClass(strings.TrimSpace(string(evidence.Class)))
	evidence.TruthLevel = searchdocs.TruthLevel(strings.TrimSpace(string(evidence.TruthLevel)))
	return evidence
}

func validate(policy Policy, evidence Evidence) error {
	var problems []string
	if policy.ID == "" {
		problems = append(problems, "policy.id is required")
	}
	if policy.HalfLife <= 0 {
		problems = append(problems, "policy.half_life is required")
	}
	if policy.MinScore < 0 || policy.MinScore > 1 {
		problems = append(problems, "policy.min_score must be between 0 and 1")
	}
	if evidence.ID == "" {
		problems = append(problems, "evidence.id is required")
	}
	if evidence.Class == "" {
		problems = append(problems, "evidence.class is required")
	}
	if evidence.TruthLevel == "" {
		problems = append(problems, "evidence.truth_level is required")
	}
	if evidence.ObservedAt.IsZero() {
		problems = append(problems, "evidence.observed_at is required")
	}
	if evidence.Score < 0 || evidence.Score > 1 {
		problems = append(problems, "evidence.score must be between 0 and 1")
	}
	return joinedValidationError(problems)
}

func isCanonicalEvidence(evidence Evidence) bool {
	return evidence.Class == EvidenceClassCanonicalGraph ||
		evidence.Class == EvidenceClassDurableRelationship ||
		evidence.TruthLevel != searchdocs.TruthLevelDerived
}

func clampScore(score float64) float64 {
	switch {
	case score < 0:
		return 0
	case score > 1:
		return 1
	default:
		return score
	}
}

func (scorer Scorer) observe(ctx context.Context, decision Decision) {
	if scorer.Observer == nil {
		return
	}
	scorer.Observer.ObserveDecay(ctx, Observation{
		PolicyID:      decision.PolicyID,
		EvidenceClass: decision.EvidenceClass,
		Outcome:       decision.Outcome,
	})
}

func joinedValidationError(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	errs := make([]error, 0, len(problems))
	for _, problem := range problems {
		errs = append(errs, errors.New(problem))
	}
	return fmt.Errorf("decay scoring validation: %w", errors.Join(errs...))
}
