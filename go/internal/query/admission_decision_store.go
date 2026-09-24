// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/admission"
)

// PostgresAdmissionDecisionReadStore adapts the reducer admission decision
// store to the query read surface.
type PostgresAdmissionDecisionReadStore struct {
	store *admissionstore.AdmissionDecisionStore
}

// NewPostgresAdmissionDecisionReadStore creates a Postgres-backed admission
// decision read store.
func NewPostgresAdmissionDecisionReadStore(database db.ExecQueryer) PostgresAdmissionDecisionReadStore {
	return PostgresAdmissionDecisionReadStore{
		store: admissionstore.NewAdmissionDecisionStore(database),
	}
}

// ListAdmissionDecisions returns a bounded page of persisted admission
// decisions.
func (s PostgresAdmissionDecisionReadStore) ListAdmissionDecisions(
	ctx context.Context,
	filter AdmissionDecisionReadFilter,
) ([]AdmissionDecisionReadRow, error) {
	if s.store == nil {
		return nil, fmt.Errorf("admission decision store is required")
	}
	if !admissionDecisionReadFilterAllowed(filter) {
		return []AdmissionDecisionReadRow{}, nil
	}
	state, err := postgresAdmissionDecisionState(filter.State)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListDecisions(ctx, admissionstore.AdmissionDecisionFilter{
		Domain:       filter.Domain,
		ScopeID:      filter.ScopeID,
		GenerationID: filter.GenerationID,
		State:        state,
		AnchorKind:   filter.AnchorKind,
		AnchorID:     filter.AnchorID,
		Limit:        filter.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AdmissionDecisionReadRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, admissionDecisionReadRowFromPostgres(row))
	}
	return out, nil
}

// ListAdmissionDecisionEvidence returns persisted evidence for one admission
// decision.
func (s PostgresAdmissionDecisionReadStore) ListAdmissionDecisionEvidence(
	ctx context.Context,
	decisionID string,
	limit int,
) ([]AdmissionDecisionEvidenceRow, error) {
	if s.store == nil {
		return nil, fmt.Errorf("admission decision store is required")
	}
	rows, err := s.store.ListEvidence(ctx, decisionID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]AdmissionDecisionEvidenceRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, AdmissionDecisionEvidenceRow{
			EvidenceID:   row.EvidenceID,
			DecisionID:   row.DecisionID,
			SourceHandle: row.SourceHandle,
			EvidenceKind: row.EvidenceKind,
			Detail:       row.Detail,
			CreatedAt:    row.CreatedAt,
		})
	}
	return out, nil
}

func postgresAdmissionDecisionState(state *string) (*admissionstore.AdmissionDecisionState, error) {
	if state == nil {
		return nil, nil
	}
	if !validAdmissionDecisionState(*state) {
		return nil, fmt.Errorf("unsupported admission decision state %q", *state)
	}
	converted := admissionstore.AdmissionDecisionState(*state)
	return &converted, nil
}

func admissionDecisionReadRowFromPostgres(row admissionstore.AdmissionDecision) AdmissionDecisionReadRow {
	return AdmissionDecisionReadRow{
		DecisionID:          row.DecisionID,
		Domain:              row.Domain,
		State:               string(row.State),
		DomainState:         row.DomainState,
		ScopeID:             row.ScopeID,
		GenerationID:        row.GenerationID,
		AnchorKind:          row.AnchorKind,
		AnchorID:            row.AnchorID,
		CandidateKind:       row.CandidateKind,
		CandidateID:         row.CandidateID,
		ConfidenceScore:     row.ConfidenceScore,
		ConfidenceBucket:    row.ConfidenceBucket,
		ConfidenceBasis:     row.ConfidenceBasis,
		FreshnessState:      row.FreshnessState,
		FreshnessObservedAt: row.FreshnessObservedAt,
		FreshnessCause:      row.FreshnessCause,
		SourceHandles:       admissionDecisionSourceHandlesFromPostgres(row.SourceHandles),
		RedactionState:      row.RedactionState,
		RedactionReason:     row.RedactionReason,
		CanonicalWrite: AdmissionDecisionCanonicalWrite{
			Eligible:      row.CanonicalWrite.Eligible,
			Written:       row.CanonicalWrite.Written,
			TargetKind:    row.CanonicalWrite.TargetKind,
			TargetID:      row.CanonicalWrite.TargetID,
			SkippedReason: row.CanonicalWrite.SkippedReason,
		},
		RecommendedAction: AdmissionDecisionNextAction{
			Action: row.RecommendedAction.Action,
			Reason: row.RecommendedAction.Reason,
			Owner:  row.RecommendedAction.Owner,
		},
		PayloadVersion: row.PayloadVersion,
		DecidedAt:      row.DecidedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func admissionDecisionSourceHandlesFromPostgres(
	handles []admissionstore.AdmissionDecisionSourceHandle,
) []AdmissionDecisionSourceHandle {
	out := make([]AdmissionDecisionSourceHandle, 0, len(handles))
	for _, handle := range handles {
		out = append(out, AdmissionDecisionSourceHandle{
			Kind:    handle.Kind,
			ID:      handle.ID,
			ScopeID: handle.ScopeID,
		})
	}
	return out
}
