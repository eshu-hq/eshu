// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type postgresAdmissionDecisionWriter struct {
	store *postgres.AdmissionDecisionStore
}

func newAdmissionDecisionWriter(db postgres.ExecQueryer) postgresAdmissionDecisionWriter {
	return postgresAdmissionDecisionWriter{
		store: postgres.NewAdmissionDecisionStore(db),
	}
}

func (w postgresAdmissionDecisionWriter) WriteAdmissionDecisions(
	ctx context.Context,
	writes []reducer.AdmissionDecisionWrite,
) error {
	if w.store == nil {
		return fmt.Errorf("admission decision store is required")
	}
	for _, write := range writes {
		if err := w.store.UpsertDecision(ctx, postgresAdmissionDecision(write.Decision)); err != nil {
			return err
		}
		if len(write.Evidence) == 0 {
			continue
		}
		if err := w.store.InsertEvidence(ctx, postgresAdmissionEvidence(write.Evidence)); err != nil {
			return err
		}
	}
	return nil
}

func postgresAdmissionDecision(decision reducer.AdmissionDecision) postgres.AdmissionDecision {
	return postgres.AdmissionDecision{
		DecisionID:          decision.DecisionID,
		Domain:              decision.Domain,
		State:               postgres.AdmissionDecisionState(decision.State),
		DomainState:         decision.DomainState,
		ScopeID:             decision.ScopeID,
		GenerationID:        decision.GenerationID,
		AnchorKind:          decision.AnchorKind,
		AnchorID:            decision.AnchorID,
		CandidateKind:       decision.CandidateKind,
		CandidateID:         decision.CandidateID,
		ConfidenceScore:     decision.ConfidenceScore,
		ConfidenceBucket:    decision.ConfidenceBucket,
		ConfidenceBasis:     decision.ConfidenceBasis,
		FreshnessState:      decision.FreshnessState,
		FreshnessObservedAt: decision.FreshnessObservedAt,
		FreshnessCause:      decision.FreshnessCause,
		SourceHandles:       postgresAdmissionSourceHandles(decision.SourceHandles),
		RedactionState:      decision.RedactionState,
		RedactionReason:     decision.RedactionReason,
		CanonicalWrite: postgres.AdmissionDecisionCanonicalWrite{
			Eligible:      decision.CanonicalWrite.Eligible,
			Written:       decision.CanonicalWrite.Written,
			TargetKind:    decision.CanonicalWrite.TargetKind,
			TargetID:      decision.CanonicalWrite.TargetID,
			SkippedReason: decision.CanonicalWrite.SkippedReason,
		},
		RecommendedAction: postgres.AdmissionDecisionNextAction{
			Action: decision.RecommendedAction.Action,
			Reason: decision.RecommendedAction.Reason,
			Owner:  decision.RecommendedAction.Owner,
		},
		PayloadVersion: decision.PayloadVersion,
		DecidedAt:      decision.DecidedAt,
		UpdatedAt:      decision.UpdatedAt,
	}
}

func postgresAdmissionSourceHandles(
	handles []reducer.AdmissionDecisionSourceHandle,
) []postgres.AdmissionDecisionSourceHandle {
	out := make([]postgres.AdmissionDecisionSourceHandle, 0, len(handles))
	for _, handle := range handles {
		out = append(out, postgres.AdmissionDecisionSourceHandle{
			Kind:    handle.Kind,
			ID:      handle.ID,
			ScopeID: handle.ScopeID,
		})
	}
	return out
}

func postgresAdmissionEvidence(
	rows []reducer.AdmissionDecisionEvidence,
) []postgres.AdmissionDecisionEvidence {
	out := make([]postgres.AdmissionDecisionEvidence, 0, len(rows))
	for _, row := range rows {
		out = append(out, postgres.AdmissionDecisionEvidence{
			EvidenceID:   row.EvidenceID,
			DecisionID:   row.DecisionID,
			SourceHandle: row.SourceHandle,
			EvidenceKind: row.EvidenceKind,
			Detail:       row.Detail,
			CreatedAt:    row.CreatedAt,
		})
	}
	return out
}
