// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestAdmissionDecisionStoreCapsEvidenceLimit(t *testing.T) {
	t.Parallel()

	db := newAdmissionDecisionTestDB()
	store := NewAdmissionDecisionStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	decision := AdmissionDecision{
		DecisionID:       "admission:bounded-evidence",
		Domain:           "deployable_unit_correlation",
		State:            AdmissionDecisionStateAdmitted,
		DomainState:      "exact",
		ScopeID:          "scope:repo-a",
		GenerationID:     "gen-a",
		AnchorKind:       "repository",
		AnchorID:         "repository:repo-a",
		CandidateKind:    "service",
		CandidateID:      "service:payments",
		ConfidenceScore:  0.98,
		ConfidenceBucket: "high",
		ConfidenceBasis:  "exact owner evidence",
		FreshnessState:   "current",
		FreshnessCause:   "active_generation",
		CanonicalWrite: AdmissionDecisionCanonicalWrite{
			Eligible:   true,
			Written:    true,
			TargetKind: "relationship",
			TargetID:   "DEPLOYS_FROM:repository:repo-a:service:payments",
		},
		RecommendedAction: AdmissionDecisionNextAction{Action: "none"},
		PayloadVersion:    "correlation.admission.v1",
		DecidedAt:         now,
		UpdatedAt:         now,
	}
	if err := store.UpsertDecision(ctx, decision); err != nil {
		t.Fatalf("UpsertDecision: %v", err)
	}

	evidence := make([]AdmissionDecisionEvidence, 0, 3)
	for i := range 3 {
		evidence = append(evidence, AdmissionDecisionEvidence{
			EvidenceID:   fmt.Sprintf("admission-evidence:%d", i),
			DecisionID:   decision.DecisionID,
			SourceHandle: fmt.Sprintf("fact:repo-a:%d", i),
			EvidenceKind: "input_fact",
			Detail:       map[string]any{"ordinal": i},
			CreatedAt:    now.Add(time.Duration(i) * time.Minute),
		})
	}
	if err := store.InsertEvidence(ctx, evidence); err != nil {
		t.Fatalf("InsertEvidence: %v", err)
	}

	got, err := store.ListEvidence(ctx, decision.DecisionID, 2)
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(evidence) = %d, want 2", len(got))
	}
	if got[0].SourceHandle != "fact:repo-a:0" || got[1].SourceHandle != "fact:repo-a:1" {
		t.Fatalf("source handles = %#v, want first two created rows", got)
	}
}
