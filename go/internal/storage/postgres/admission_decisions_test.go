// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestAdmissionDecisionSchemaSQL(t *testing.T) {
	t.Parallel()

	sqlStr := AdmissionDecisionSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS admission_decisions",
		"CREATE TABLE IF NOT EXISTS admission_decision_evidence",
		"admission_decisions_scope_generation_domain_idx",
		"admission_decisions_anchor_idx",
		"admission_decision_evidence_decision_idx",
		"source_handles JSONB NOT NULL",
		"canonical_write JSONB NOT NULL",
		"recommended_action JSONB NOT NULL",
		"CHECK (state IN ('admitted', 'rejected', 'ambiguous', 'stale', 'missing_evidence', 'permission_hidden', 'unsupported', 'unsafe'))",
	} {
		if !strings.Contains(sqlStr, want) {
			t.Fatalf("AdmissionDecisionSchemaSQL() missing %q", want)
		}
	}
}

func TestAdmissionDecisionStatesCoverClosedVocabulary(t *testing.T) {
	t.Parallel()

	got := make([]string, 0)
	for _, state := range AdmissionDecisionStateValues() {
		got = append(got, string(state))
	}
	sort.Strings(got)
	want := []string{
		"admitted",
		"ambiguous",
		"missing_evidence",
		"permission_hidden",
		"rejected",
		"stale",
		"unsafe",
		"unsupported",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
}

func TestAdmissionDecisionStoreUpsertListAndEvidence(t *testing.T) {
	t.Parallel()

	db := newAdmissionDecisionTestDB()
	store := NewAdmissionDecisionStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	decision := AdmissionDecision{
		DecisionID:       "admission:deployable-unit:repo-a",
		Domain:           "deployable_unit_correlation",
		State:            AdmissionDecisionStateAmbiguous,
		DomainState:      "ambiguous",
		ScopeID:          "scope:repo-a",
		GenerationID:     "gen-a",
		AnchorKind:       "repository",
		AnchorID:         "repository:repo-a",
		CandidateKind:    "service",
		CandidateID:      "service:payments",
		ConfidenceScore:  0.42,
		ConfidenceBucket: "medium",
		ConfidenceBasis:  "multiple service candidates share the repository",
		FreshnessState:   "current",
		FreshnessCause:   "active_generation",
		SourceHandles: []AdmissionDecisionSourceHandle{
			{Kind: "fact", ID: "fact:repo-a", ScopeID: "scope:repo-a"},
			{Kind: "projection_decision", ID: "projection:repo-a"},
		},
		RedactionState: "safe",
		CanonicalWrite: AdmissionDecisionCanonicalWrite{
			Eligible:      false,
			Written:       false,
			TargetKind:    "relationship",
			SkippedReason: "ambiguous",
		},
		RecommendedAction: AdmissionDecisionNextAction{
			Action: "provide_catalog_owner",
			Reason: "candidate service ownership is unresolved",
		},
		PayloadVersion: "correlation.admission.v1",
		DecidedAt:      now,
		UpdatedAt:      now,
	}

	if err := store.UpsertDecision(ctx, decision); err != nil {
		t.Fatalf("UpsertDecision: %v", err)
	}

	decision.State = AdmissionDecisionStateAdmitted
	decision.DomainState = "exact"
	decision.ConfidenceScore = 0.98
	decision.CanonicalWrite.Eligible = true
	decision.CanonicalWrite.Written = true
	decision.CanonicalWrite.SkippedReason = ""
	if err := store.UpsertDecision(ctx, decision); err != nil {
		t.Fatalf("second UpsertDecision: %v", err)
	}

	rows, err := store.ListDecisions(ctx, AdmissionDecisionFilter{
		Domain:       "deployable_unit_correlation",
		ScopeID:      "scope:repo-a",
		GenerationID: "gen-a",
		Limit:        100,
	})
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.State != AdmissionDecisionStateAdmitted {
		t.Fatalf("state = %q, want admitted after overwrite", got.State)
	}
	if got.ConfidenceScore != 0.98 {
		t.Fatalf("confidence = %f, want 0.98", got.ConfidenceScore)
	}
	if !got.CanonicalWrite.Written {
		t.Fatal("canonical write posture was not preserved")
	}
	if len(got.SourceHandles) != 2 {
		t.Fatalf("source handles = %d, want 2", len(got.SourceHandles))
	}

	evidence := []AdmissionDecisionEvidence{
		{
			EvidenceID:   "admission-evidence:1",
			DecisionID:   decision.DecisionID,
			SourceHandle: "fact:repo-a",
			EvidenceKind: "input_fact",
			Detail:       map[string]any{"fact_kind": "reducer_service_catalog_correlation"},
			CreatedAt:    now,
		},
	}
	if err := store.InsertEvidence(ctx, evidence); err != nil {
		t.Fatalf("InsertEvidence: %v", err)
	}
	gotEvidence, err := store.ListEvidence(ctx, decision.DecisionID, 100)
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if len(gotEvidence) != 1 {
		t.Fatalf("len(evidence) = %d, want 1", len(gotEvidence))
	}
	if gotEvidence[0].SourceHandle != "fact:repo-a" {
		t.Fatalf("source handle = %q, want fact:repo-a", gotEvidence[0].SourceHandle)
	}
	if gotEvidence[0].Detail["fact_kind"] != "reducer_service_catalog_correlation" {
		t.Fatalf("detail = %#v", gotEvidence[0].Detail)
	}
}

func TestAdmissionDecisionStoreRequiresBoundedListFilter(t *testing.T) {
	t.Parallel()

	store := NewAdmissionDecisionStore(newAdmissionDecisionTestDB())
	_, err := store.ListDecisions(context.Background(), AdmissionDecisionFilter{
		Domain:       "deployable_unit_correlation",
		ScopeID:      "scope:repo-a",
		GenerationID: "",
		Limit:        100,
	})
	if err == nil {
		t.Fatal("ListDecisions() error = nil, want bounded filter error")
	}
}

func TestAdmissionDecisionStoreClampsListLimit(t *testing.T) {
	t.Parallel()

	db := newAdmissionDecisionTestDB()
	store := NewAdmissionDecisionStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	for i := range 3 {
		decision := AdmissionDecision{
			DecisionID:       fmt.Sprintf("admission:%d", i),
			Domain:           "package_supply_chain",
			State:            AdmissionDecisionStateRejected,
			DomainState:      "rejected",
			ScopeID:          "scope:repo-b",
			GenerationID:     "gen-b",
			AnchorKind:       "package",
			AnchorID:         "package:library",
			CandidateKind:    "repository",
			CandidateID:      fmt.Sprintf("repository:%d", i),
			ConfidenceScore:  0.1,
			ConfidenceBucket: "low",
			ConfidenceBasis:  "no ownership evidence",
			FreshnessState:   "current",
			FreshnessCause:   "active_generation",
			RedactionState:   "safe",
			CanonicalWrite: AdmissionDecisionCanonicalWrite{
				Eligible:      false,
				Written:       false,
				SkippedReason: "rejected",
			},
			RecommendedAction: AdmissionDecisionNextAction{Action: "add_package_owner"},
			PayloadVersion:    "correlation.admission.v1",
			DecidedAt:         now.Add(time.Duration(i) * time.Minute),
			UpdatedAt:         now.Add(time.Duration(i) * time.Minute),
		}
		if err := store.UpsertDecision(ctx, decision); err != nil {
			t.Fatalf("UpsertDecision(%d): %v", i, err)
		}
	}

	rows, err := store.ListDecisions(ctx, AdmissionDecisionFilter{
		Domain:       "package_supply_chain",
		ScopeID:      "scope:repo-b",
		GenerationID: "gen-b",
		Limit:        0,
	})
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want default bounded page of 1", len(rows))
	}
	if rows[0].DecisionID != "admission:2" {
		t.Fatalf("first row = %q, want newest admission:2", rows[0].DecisionID)
	}
}
