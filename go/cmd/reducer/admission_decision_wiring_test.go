// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestPostgresAdmissionDecisionMappingPreservesSharedPayload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 3, 30, 0, 0, time.UTC)
	got := postgresAdmissionDecision(reducer.AdmissionDecision{
		DecisionID:       "admission:one",
		Domain:           string(reducer.DomainDeployableUnitCorrelation),
		State:            reducer.AdmissionStateAdmitted,
		DomainState:      "admitted",
		ScopeID:          "repository:api",
		GenerationID:     "generation-1",
		AnchorKind:       "repository",
		AnchorID:         "repo-api",
		CandidateKind:    "deployable_unit",
		CandidateID:      "candidate-one",
		ConfidenceScore:  0.94,
		ConfidenceBucket: "high",
		ConfidenceBasis:  "argocd",
		SourceHandles: []reducer.AdmissionDecisionSourceHandle{{
			Kind:    "repository_identity",
			ID:      "fact-one",
			ScopeID: "repository:api",
		}},
		CanonicalWrite: reducer.AdmissionCanonicalWrite{
			Eligible:   true,
			Written:    true,
			TargetKind: reducer.DomainDeployableUnitEdges,
			TargetID:   "edge-one",
		},
		RecommendedAction: reducer.AdmissionNextAction{Action: "none"},
		PayloadVersion:    "v1",
		DecidedAt:         now,
		UpdatedAt:         now,
	})

	if got.State != postgres.AdmissionDecisionStateAdmitted {
		t.Fatalf("State = %q, want %q", got.State, postgres.AdmissionDecisionStateAdmitted)
	}
	if got.CanonicalWrite.TargetKind != reducer.DomainDeployableUnitEdges {
		t.Fatalf("TargetKind = %q, want %q", got.CanonicalWrite.TargetKind, reducer.DomainDeployableUnitEdges)
	}
	if len(got.SourceHandles) != 1 || got.SourceHandles[0].ID != "fact-one" {
		t.Fatalf("SourceHandles = %+v, want fact-one handle", got.SourceHandles)
	}
}

func TestPostgresAdmissionDecisionWriterRequiresStore(t *testing.T) {
	t.Parallel()

	err := (postgresAdmissionDecisionWriter{}).WriteAdmissionDecisions(
		context.Background(),
		[]reducer.AdmissionDecisionWrite{{}},
	)
	if err == nil {
		t.Fatal("WriteAdmissionDecisions() error = nil, want missing store error")
	}
}

func TestPostgresAdmissionEvidenceMappingPreservesDetails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 3, 45, 0, 0, time.UTC)
	got := postgresAdmissionEvidence([]reducer.AdmissionDecisionEvidence{{
		EvidenceID:   "evidence-one",
		DecisionID:   "admission:one",
		SourceHandle: "fact-one",
		EvidenceKind: "repository_identity",
		Detail:       map[string]any{"key": "repo_id"},
		CreatedAt:    now,
	}})

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].EvidenceID != "evidence-one" || got[0].Detail["key"] != "repo_id" {
		t.Fatalf("mapped evidence = %+v, want preserved id and detail", got[0])
	}
}
