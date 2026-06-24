// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

func TestAdmissionDecisionStoreRejectsUnknownStateBeforeWrite(t *testing.T) {
	t.Parallel()

	db := newAdmissionDecisionTestDB()
	store := NewAdmissionDecisionStore(db)
	now := time.Now().UTC()

	err := store.UpsertDecision(context.Background(), AdmissionDecision{
		DecisionID:       "admission:invalid",
		Domain:           "deployable_unit_correlation",
		State:            AdmissionDecisionState("maybe"),
		DomainState:      "maybe",
		ScopeID:          "scope:repo-a",
		GenerationID:     "gen-a",
		AnchorKind:       "repository",
		AnchorID:         "repository:repo-a",
		CandidateKind:    "service",
		CandidateID:      "service:payments",
		ConfidenceScore:  0.2,
		ConfidenceBucket: "low",
		ConfidenceBasis:  "invalid state test",
		FreshnessState:   "current",
		FreshnessCause:   "active_generation",
		RedactionState:   "safe",
		PayloadVersion:   "correlation.admission.v1",
		DecidedAt:        now,
		UpdatedAt:        now,
	})
	if err == nil {
		t.Fatal("UpsertDecision() error = nil, want invalid state error")
	}
	if len(db.decisions) != 0 {
		t.Fatalf("stored decisions = %d, want 0", len(db.decisions))
	}
}
