// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"fmt"
	"strings"
)

// QueueSurface identifies an Eshu queue or workflow ownership surface.
type QueueSurface string

const (
	// QueueSurfaceProjector covers source-local projector work items.
	QueueSurfaceProjector QueueSurface = "projector"
	// QueueSurfaceReducer covers reducer and graph materialization work items.
	QueueSurfaceReducer QueueSurface = "reducer"
	// QueueSurfaceWorkflow covers hosted collector workflow work items.
	QueueSurfaceWorkflow QueueSurface = "workflow"
	// QueueSurfaceFreshnessTrigger covers webhook and freshness-trigger queues.
	QueueSurfaceFreshnessTrigger QueueSurface = "freshness_trigger"
	// QueueSurfaceRepair covers repair and replay queues.
	QueueSurfaceRepair QueueSurface = "repair"
)

// QueueSubstrate names a candidate durable queue/workflow substrate.
type QueueSubstrate string

const (
	// QueueSubstratePostgres is the current Postgres-backed queue substrate.
	QueueSubstratePostgres QueueSubstrate = "postgres"
	// QueueSubstrateSQS is Amazon SQS.
	QueueSubstrateSQS QueueSubstrate = "sqs"
	// QueueSubstrateNATSJetStream is NATS JetStream.
	QueueSubstrateNATSJetStream QueueSubstrate = "nats_jetstream"
	// QueueSubstrateTemporal is Temporal or a Temporal-compatible workflow engine.
	QueueSubstrateTemporal QueueSubstrate = "temporal"
	// QueueSubstrateOther is another explicitly described substrate.
	QueueSubstrateOther QueueSubstrate = "other"
)

// QueueCapabilityStatus records whether a substrate satisfies one capability.
type QueueCapabilityStatus string

const (
	// QueueCapabilityPass means the candidate satisfies the capability.
	QueueCapabilityPass QueueCapabilityStatus = "pass"
	// QueueCapabilityFail means the candidate does not satisfy the capability.
	QueueCapabilityFail QueueCapabilityStatus = "fail"
	// QueueCapabilityUnknown means the capability needs more proof.
	QueueCapabilityUnknown QueueCapabilityStatus = "unknown"
)

// QueueCapabilityAssessment compares a candidate against required semantics.
type QueueCapabilityAssessment struct {
	ClaimLeaseFencing QueueCapabilityStatus `json:"claim_lease_fencing"`
	VisibilityTimeout QueueCapabilityStatus `json:"visibility_timeout"`
	DelayedRetry      QueueCapabilityStatus `json:"delayed_retry"`
	IdempotentAckFail QueueCapabilityStatus `json:"idempotent_ack_fail"`
	DeadLetter        QueueCapabilityStatus `json:"dead_letter"`
	Backpressure      QueueCapabilityStatus `json:"backpressure"`
	CrashRecovery     QueueCapabilityStatus `json:"crash_recovery"`
	FairScheduling    QueueCapabilityStatus `json:"fair_scheduling"`
}

// QueueProofScenario names a required failure or concurrency proof scenario.
type QueueProofScenario string

const (
	// QueueProofDuplicateDelivery covers duplicate delivery after success or retry.
	QueueProofDuplicateDelivery QueueProofScenario = "duplicate_delivery"
	// QueueProofPartialFailure covers a worker crash after partial side effects.
	QueueProofPartialFailure QueueProofScenario = "partial_failure"
	// QueueProofStaleLease covers expired ownership and safe reclamation.
	QueueProofStaleLease QueueProofScenario = "stale_lease"
	// QueueProofConcurrentClaimSameConflictDomain covers conflict-domain contention.
	QueueProofConcurrentClaimSameConflictDomain QueueProofScenario = "concurrent_claim_same_conflict_domain"
	// QueueProofRetry covers delayed retry and retry accounting.
	QueueProofRetry QueueProofScenario = "retry"
	// QueueProofDeadLetterReplay covers replay from terminal dead-letter state.
	QueueProofDeadLetterReplay QueueProofScenario = "dead_letter_replay"
	// QueueProofEmptyQueue covers empty or already-drained queue state.
	QueueProofEmptyQueue QueueProofScenario = "empty_queue"
)

// QueueProofStatus records whether a proof scenario is covered or pending.
type QueueProofStatus string

const (
	// QueueProofPassed means the proof scenario has executable evidence.
	QueueProofPassed QueueProofStatus = "passed"
	// QueueProofPlanned means the proof plan names the scenario for future execution.
	QueueProofPlanned QueueProofStatus = "planned"
	// QueueProofFail means the candidate failed the proof scenario.
	QueueProofFail QueueProofStatus = "failed"
)

// QueueProof records one required proof scenario for a queue candidate.
type QueueProof struct {
	Scenario QueueProofScenario `json:"scenario"`
	Status   QueueProofStatus   `json:"status"`
	Evidence string             `json:"evidence,omitempty"`
}

// QueueObservabilityAssessment records required operator-facing signals.
type QueueObservabilityAssessment struct {
	Backlog            bool `json:"backlog"`
	OldestAge          bool `json:"oldest_age"`
	RetryCount         bool `json:"retry_count"`
	OverdueClaims      bool `json:"overdue_claims"`
	DeadLetters        bool `json:"dead_letters"`
	ClaimDuration      bool `json:"claim_duration"`
	ProcessingDuration bool `json:"processing_duration"`
}

// QueueCandidateEvaluation is one substrate candidate in the decision record.
type QueueCandidateEvaluation struct {
	CandidateID   string                       `json:"candidate_id"`
	Substrate     QueueSubstrate               `json:"substrate"`
	Capabilities  QueueCapabilityAssessment    `json:"capabilities"`
	Proofs        []QueueProof                 `json:"proofs"`
	Observability QueueObservabilityAssessment `json:"observability"`
}

// QueueSubstrateDecision records the #1289 queue/workflow substrate evaluation.
type QueueSubstrateDecision struct {
	DecisionID                          string                     `json:"decision_id"`
	QueueSurface                        QueueSurface               `json:"queue_surface"`
	ConflictDomain                      string                     `json:"conflict_domain"`
	TransactionScope                    string                     `json:"transaction_scope"`
	RetryScope                          string                     `json:"retry_scope"`
	IdempotencyKey                      string                     `json:"idempotency_key"`
	ChosenCandidateID                   string                     `json:"chosen_candidate_id"`
	Candidates                          []QueueCandidateEvaluation `json:"candidates"`
	StorageSuccessTreatedAsQueueSuccess bool                       `json:"storage_success_treated_as_queue_success"`
	WorkerCountReductionAsFix           bool                       `json:"worker_count_reduction_as_fix"`
}

// ValidateQueueSubstrateDecision verifies one queue-substrate evaluation record.
func ValidateQueueSubstrateDecision(decision QueueSubstrateDecision) error {
	if strings.TrimSpace(decision.DecisionID) == "" {
		return fmt.Errorf("decision id is required")
	}
	if !supportedQueueSurface(decision.QueueSurface) {
		return fmt.Errorf("unsupported queue surface %q", decision.QueueSurface)
	}
	if strings.TrimSpace(decision.ConflictDomain) == "" {
		return fmt.Errorf("conflict domain is required")
	}
	if strings.TrimSpace(decision.TransactionScope) == "" {
		return fmt.Errorf("transaction scope is required")
	}
	if strings.TrimSpace(decision.RetryScope) == "" {
		return fmt.Errorf("retry scope is required")
	}
	if strings.TrimSpace(decision.IdempotencyKey) == "" {
		return fmt.Errorf("idempotency key is required")
	}
	if strings.TrimSpace(decision.ChosenCandidateID) == "" {
		return fmt.Errorf("chosen candidate id is required")
	}
	if decision.StorageSuccessTreatedAsQueueSuccess {
		return fmt.Errorf("storage success must not be treated as queue success")
	}
	if decision.WorkerCountReductionAsFix {
		return fmt.Errorf("worker-count reduction is not a queue-substrate fix")
	}
	if len(decision.Candidates) == 0 {
		return fmt.Errorf("at least one queue substrate candidate is required")
	}

	var chosen *QueueCandidateEvaluation
	seen := make(map[string]struct{}, len(decision.Candidates))
	for i := range decision.Candidates {
		candidate := &decision.Candidates[i]
		if err := validateQueueCandidate(*candidate); err != nil {
			return err
		}
		if _, ok := seen[candidate.CandidateID]; ok {
			return fmt.Errorf("duplicate candidate id %q", candidate.CandidateID)
		}
		seen[candidate.CandidateID] = struct{}{}
		if candidate.CandidateID == decision.ChosenCandidateID {
			chosen = candidate
		}
	}
	if chosen == nil {
		return fmt.Errorf("chosen candidate %q is not in candidates", decision.ChosenCandidateID)
	}
	if err := validateChosenQueueCandidate(*chosen); err != nil {
		return err
	}
	return nil
}

func validateQueueCandidate(candidate QueueCandidateEvaluation) error {
	if strings.TrimSpace(candidate.CandidateID) == "" {
		return fmt.Errorf("candidate id is required")
	}
	if !supportedQueueSubstrate(candidate.Substrate) {
		return fmt.Errorf("candidate %s unsupported substrate %q", candidate.CandidateID, candidate.Substrate)
	}
	if err := validateQueueCapabilityAssessment(candidate.CandidateID, candidate.Capabilities); err != nil {
		return err
	}
	return validateQueueProofCoverage(candidate.CandidateID, candidate.Proofs)
}

func validateChosenQueueCandidate(candidate QueueCandidateEvaluation) error {
	if err := requireQueueCapabilityPass(candidate.CandidateID, "claim/lease/fencing", candidate.Capabilities.ClaimLeaseFencing); err != nil {
		return err
	}
	if err := requireQueueCapabilityPass(candidate.CandidateID, "visibility-timeout", candidate.Capabilities.VisibilityTimeout); err != nil {
		return err
	}
	if err := requireQueueCapabilityPass(candidate.CandidateID, "delayed-retry", candidate.Capabilities.DelayedRetry); err != nil {
		return err
	}
	if err := requireQueueCapabilityPass(candidate.CandidateID, "idempotent-ack/fail", candidate.Capabilities.IdempotentAckFail); err != nil {
		return err
	}
	if err := requireQueueCapabilityPass(candidate.CandidateID, "dead-letter", candidate.Capabilities.DeadLetter); err != nil {
		return err
	}
	if err := requireQueueCapabilityPass(candidate.CandidateID, "backpressure", candidate.Capabilities.Backpressure); err != nil {
		return err
	}
	if err := requireQueueCapabilityPass(candidate.CandidateID, "crash-recovery", candidate.Capabilities.CrashRecovery); err != nil {
		return err
	}
	if err := requireQueueCapabilityPass(candidate.CandidateID, "fair-scheduling", candidate.Capabilities.FairScheduling); err != nil {
		return err
	}
	if err := requireChosenQueueProofCoverage(candidate.CandidateID, candidate.Proofs); err != nil {
		return err
	}
	return requireQueueObservability(candidate.CandidateID, candidate.Observability)
}

func validateQueueCapabilityAssessment(candidateID string, capabilities QueueCapabilityAssessment) error {
	checks := []struct {
		label  string
		status QueueCapabilityStatus
	}{
		{"claim/lease/fencing", capabilities.ClaimLeaseFencing},
		{"visibility-timeout", capabilities.VisibilityTimeout},
		{"delayed-retry", capabilities.DelayedRetry},
		{"idempotent-ack/fail", capabilities.IdempotentAckFail},
		{"dead-letter", capabilities.DeadLetter},
		{"backpressure", capabilities.Backpressure},
		{"crash-recovery", capabilities.CrashRecovery},
		{"fair-scheduling", capabilities.FairScheduling},
	}
	for _, check := range checks {
		if check.status == "" {
			return fmt.Errorf("candidate %s %s status is required", candidateID, check.label)
		}
		if !supportedQueueCapabilityStatus(check.status) {
			return fmt.Errorf("candidate %s %s status %q is unsupported", candidateID, check.label, check.status)
		}
	}
	return nil
}

func requireQueueCapabilityPass(candidateID string, label string, status QueueCapabilityStatus) error {
	if status != QueueCapabilityPass {
		return fmt.Errorf("chosen candidate %s must pass %s", candidateID, label)
	}
	return nil
}

func validateQueueProofCoverage(candidateID string, proofs []QueueProof) error {
	if len(proofs) == 0 {
		return fmt.Errorf("candidate %s proof scenarios are required", candidateID)
	}
	seen := make(map[QueueProofScenario]QueueProofStatus, len(proofs))
	for _, proof := range proofs {
		if !supportedQueueProofScenario(proof.Scenario) {
			return fmt.Errorf("candidate %s proof scenario %q is unsupported", candidateID, proof.Scenario)
		}
		if !supportedQueueProofStatus(proof.Status) {
			return fmt.Errorf("candidate %s proof scenario %s status %q is unsupported", candidateID, proof.Scenario, proof.Status)
		}
		if _, ok := seen[proof.Scenario]; ok {
			return fmt.Errorf("candidate %s proof scenario %s is duplicated", candidateID, proof.Scenario)
		}
		seen[proof.Scenario] = proof.Status
	}
	for _, scenario := range requiredQueueProofScenarios() {
		if _, ok := seen[scenario]; !ok {
			return fmt.Errorf("candidate %s proof scenario %s is required", candidateID, scenario)
		}
	}
	return nil
}

func requireChosenQueueProofCoverage(candidateID string, proofs []QueueProof) error {
	statuses := make(map[QueueProofScenario]QueueProofStatus, len(proofs))
	for _, proof := range proofs {
		statuses[proof.Scenario] = proof.Status
	}
	for _, scenario := range requiredQueueProofScenarios() {
		status := statuses[scenario]
		if status != QueueProofPassed && status != QueueProofPlanned {
			return fmt.Errorf("chosen candidate %s proof scenario %s must be covered", candidateID, scenario)
		}
	}
	return nil
}

func requireQueueObservability(candidateID string, observability QueueObservabilityAssessment) error {
	checks := []struct {
		label string
		ok    bool
	}{
		{"backlog", observability.Backlog},
		{"oldest-age", observability.OldestAge},
		{"retry-count", observability.RetryCount},
		{"overdue-claims", observability.OverdueClaims},
		{"dead-letters", observability.DeadLetters},
		{"claim-duration", observability.ClaimDuration},
		{"processing-duration", observability.ProcessingDuration},
	}
	for _, check := range checks {
		if !check.ok {
			return fmt.Errorf("chosen candidate %s missing %s observability", candidateID, check.label)
		}
	}
	return nil
}

func supportedQueueSurface(surface QueueSurface) bool {
	switch surface {
	case QueueSurfaceProjector, QueueSurfaceReducer, QueueSurfaceWorkflow,
		QueueSurfaceFreshnessTrigger, QueueSurfaceRepair:
		return true
	default:
		return false
	}
}

func supportedQueueSubstrate(substrate QueueSubstrate) bool {
	switch substrate {
	case QueueSubstratePostgres, QueueSubstrateSQS, QueueSubstrateNATSJetStream,
		QueueSubstrateTemporal, QueueSubstrateOther:
		return true
	default:
		return false
	}
}

func supportedQueueCapabilityStatus(status QueueCapabilityStatus) bool {
	switch status {
	case QueueCapabilityPass, QueueCapabilityFail, QueueCapabilityUnknown:
		return true
	default:
		return false
	}
}

func supportedQueueProofScenario(scenario QueueProofScenario) bool {
	switch scenario {
	case QueueProofDuplicateDelivery, QueueProofPartialFailure, QueueProofStaleLease,
		QueueProofConcurrentClaimSameConflictDomain, QueueProofRetry,
		QueueProofDeadLetterReplay, QueueProofEmptyQueue:
		return true
	default:
		return false
	}
}

func supportedQueueProofStatus(status QueueProofStatus) bool {
	switch status {
	case QueueProofPassed, QueueProofPlanned, QueueProofFail:
		return true
	default:
		return false
	}
}

func requiredQueueProofs(status QueueProofStatus) []QueueProof {
	scenarios := requiredQueueProofScenarios()
	proofs := make([]QueueProof, 0, len(scenarios))
	for _, scenario := range scenarios {
		proofs = append(proofs, QueueProof{Scenario: scenario, Status: status})
	}
	return proofs
}

func requiredQueueProofScenarios() []QueueProofScenario {
	return []QueueProofScenario{
		QueueProofDuplicateDelivery,
		QueueProofPartialFailure,
		QueueProofStaleLease,
		QueueProofConcurrentClaimSameConflictDomain,
		QueueProofRetry,
		QueueProofDeadLetterReplay,
		QueueProofEmptyQueue,
	}
}
