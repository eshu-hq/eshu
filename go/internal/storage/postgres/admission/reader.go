// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admissionstore

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// AdmissionDecisionReader exposes only the bounded read operations for
// admission decisions and their evidence.
type AdmissionDecisionReader struct {
	database db.Queryer
}

// NewAdmissionDecisionReader constructs a decision reader without write access.
func NewAdmissionDecisionReader(database db.Queryer) *AdmissionDecisionReader {
	return &AdmissionDecisionReader{database: database}
}

// ListDecisions returns persisted admission decisions for one bounded domain,
// scope, and generation.
func (r *AdmissionDecisionReader) ListDecisions(ctx context.Context, f AdmissionDecisionFilter) ([]AdmissionDecision, error) {
	return listAdmissionDecisions(ctx, r.database, f)
}

// ListEvidence returns a bounded page of evidence for one admission decision.
func (r *AdmissionDecisionReader) ListEvidence(ctx context.Context, decisionID string, limit int) ([]AdmissionDecisionEvidence, error) {
	return listAdmissionDecisionEvidence(ctx, r.database, decisionID, limit)
}

func listAdmissionDecisions(ctx context.Context, database db.Queryer, f AdmissionDecisionFilter) ([]AdmissionDecision, error) {
	if err := validateAdmissionDecisionFilter(f); err != nil {
		return nil, err
	}
	state := ""
	if f.State != nil {
		state = string(*f.State)
	}
	rows, err := database.QueryContext(ctx, listAdmissionDecisionsSQL,
		f.Domain, f.ScopeID, f.GenerationID, state, f.AnchorKind, f.AnchorID, admissionDecisionLimit(f.Limit))
	if err != nil {
		return nil, fmt.Errorf("query admission decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanAdmissionDecisionRows(rows)
}

func listAdmissionDecisionEvidence(ctx context.Context, database db.Queryer, decisionID string, limit int) ([]AdmissionDecisionEvidence, error) {
	rows, err := database.QueryContext(ctx, listAdmissionDecisionEvidenceSQL, decisionID, admissionDecisionLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("query admission decision evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanAdmissionDecisionEvidenceRows(rows)
}
