// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package decisionsstore

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// DecisionReader exposes only bounded projection decision and evidence reads.
type DecisionReader struct {
	database db.Queryer
}

// NewDecisionReader constructs a decision reader without write capability.
func NewDecisionReader(database db.Queryer) *DecisionReader {
	return &DecisionReader{database: database}
}

// ListDecisions returns persisted decisions for one repository/run pair.
func (r *DecisionReader) ListDecisions(ctx context.Context, f DecisionFilter) ([]projector.ProjectionDecisionRow, error) {
	return listDecisions(ctx, r.database, f)
}

// ListEvidence returns persisted evidence for one decision.
func (r *DecisionReader) ListEvidence(ctx context.Context, decisionID string) ([]projector.ProjectionDecisionEvidenceRow, error) {
	return listEvidence(ctx, r.database, decisionID)
}

func listDecisions(ctx context.Context, database db.Queryer, f DecisionFilter) ([]projector.ProjectionDecisionRow, error) {
	limit := max(f.Limit, 1)
	decisionType := ""
	if f.DecisionType != nil {
		decisionType = *f.DecisionType
	}
	sqlRows, err := database.QueryContext(ctx, listDecisionsSQL,
		f.RepositoryID, f.SourceRunID, decisionType, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()
	return scanDecisionRows(sqlRows)
}

func listEvidence(ctx context.Context, database db.Queryer, decisionID string) ([]projector.ProjectionDecisionEvidenceRow, error) {
	sqlRows, err := database.QueryContext(ctx, listEvidenceSQL, decisionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()
	return scanEvidenceRows(sqlRows)
}
