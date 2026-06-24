// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"strings"
	"testing"
)

func TestValidateQueueSubstrateDecisionAcceptsCoveredCandidate(t *testing.T) {
	decision := validQueueSubstrateDecision()

	if err := ValidateQueueSubstrateDecision(decision); err != nil {
		t.Fatalf("ValidateQueueSubstrateDecision() error = %v, want nil", err)
	}
}

func TestValidateQueueSubstrateDecisionRejectsInvalidEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*QueueSubstrateDecision)
		want   string
	}{
		{
			name: "missing conflict domain",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.ConflictDomain = ""
			},
			want: "conflict domain is required",
		},
		{
			name: "missing transaction scope",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.TransactionScope = ""
			},
			want: "transaction scope is required",
		},
		{
			name: "missing retry scope",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.RetryScope = ""
			},
			want: "retry scope is required",
		},
		{
			name: "missing idempotency key",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.IdempotencyKey = ""
			},
			want: "idempotency key is required",
		},
		{
			name: "storage success conflated with queue success",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.StorageSuccessTreatedAsQueueSuccess = true
			},
			want: "storage success must not be treated as queue success",
		},
		{
			name: "worker count reduction as fix",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.WorkerCountReductionAsFix = true
			},
			want: "worker-count reduction is not a queue-substrate fix",
		},
		{
			name: "missing candidates",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.Candidates = nil
			},
			want: "at least one queue substrate candidate is required",
		},
		{
			name: "missing capability status",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.Candidates[0].Capabilities.ClaimLeaseFencing = ""
			},
			want: "candidate retained_postgres claim/lease/fencing status is required",
		},
		{
			name: "chosen candidate fails dead letter",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.Candidates[0].Capabilities.DeadLetter = QueueCapabilityFail
			},
			want: "chosen candidate retained_postgres must pass dead-letter",
		},
		{
			name: "missing proof scenario",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.Candidates[0].Proofs = decision.Candidates[0].Proofs[1:]
			},
			want: "candidate retained_postgres proof scenario duplicate_delivery is required",
		},
		{
			name: "duplicate proof scenario",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.Candidates[0].Proofs = append(decision.Candidates[0].Proofs, decision.Candidates[0].Proofs[0])
			},
			want: "candidate retained_postgres proof scenario duplicate_delivery is duplicated",
		},
		{
			name: "failed proof scenario",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.Candidates[0].Proofs[3].Status = QueueProofFail
			},
			want: "chosen candidate retained_postgres proof scenario concurrent_claim_same_conflict_domain must be covered",
		},
		{
			name: "missing backlog observability",
			mutate: func(decision *QueueSubstrateDecision) {
				decision.Candidates[0].Observability.Backlog = false
			},
			want: "chosen candidate retained_postgres missing backlog observability",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := validQueueSubstrateDecision()
			test.mutate(&decision)

			err := ValidateQueueSubstrateDecision(decision)
			if err == nil {
				t.Fatalf("ValidateQueueSubstrateDecision() error = nil, want %q", test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateQueueSubstrateDecision() error = %q, want substring %q", err.Error(), test.want)
			}
		})
	}
}

func validQueueSubstrateDecision() QueueSubstrateDecision {
	postgres := QueueCandidateEvaluation{
		CandidateID: "retained_postgres",
		Substrate:   QueueSubstratePostgres,
		Capabilities: QueueCapabilityAssessment{
			ClaimLeaseFencing: QueueCapabilityPass,
			VisibilityTimeout: QueueCapabilityPass,
			DelayedRetry:      QueueCapabilityPass,
			IdempotentAckFail: QueueCapabilityPass,
			DeadLetter:        QueueCapabilityPass,
			Backpressure:      QueueCapabilityPass,
			CrashRecovery:     QueueCapabilityPass,
			FairScheduling:    QueueCapabilityPass,
		},
		Proofs: requiredQueueProofs(QueueProofPlanned),
		Observability: QueueObservabilityAssessment{
			Backlog:            true,
			OldestAge:          true,
			RetryCount:         true,
			OverdueClaims:      true,
			DeadLetters:        true,
			ClaimDuration:      true,
			ProcessingDuration: true,
		},
	}
	temporal := postgres
	temporal.CandidateID = "temporal_future"
	temporal.Substrate = QueueSubstrateTemporal
	temporal.Capabilities.FairScheduling = QueueCapabilityUnknown

	return QueueSubstrateDecision{
		DecisionID:                          "queue-substrate-eval-1289",
		QueueSurface:                        QueueSurfaceReducer,
		ConflictDomain:                      "stage:conflict_domain:conflict_key",
		TransactionScope:                    "claim mutation and ack/fail mutation are fenced separately",
		RetryScope:                          "one work item attempt, replay resets terminal dead-letter state",
		IdempotencyKey:                      "work_item_id:fencing_token",
		ChosenCandidateID:                   "retained_postgres",
		Candidates:                          []QueueCandidateEvaluation{postgres, temporal},
		StorageSuccessTreatedAsQueueSuccess: false,
		WorkerCountReductionAsFix:           false,
	}
}
