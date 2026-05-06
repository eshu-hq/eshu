package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

const decisionSchemaSQL = `
CREATE TABLE IF NOT EXISTS projection_decisions (
    decision_id TEXT PRIMARY KEY,
    decision_type TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    work_item_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    confidence_score DOUBLE PRECISION NOT NULL,
    confidence_reason TEXT NOT NULL,
    provenance_summary JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS projection_decisions_repo_run_idx
    ON projection_decisions (repository_id, source_run_id, created_at);

CREATE TABLE IF NOT EXISTS projection_decision_evidence (
    evidence_id TEXT PRIMARY KEY,
    decision_id TEXT NOT NULL,
    fact_id TEXT NULL,
    evidence_kind TEXT NOT NULL,
    detail JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS projection_decision_evidence_decision_idx
    ON projection_decision_evidence (decision_id, created_at);
`

// DecisionSchemaSQL returns the DDL for projection decision tables.
func DecisionSchemaSQL() string {
	return decisionSchemaSQL
}

const upsertDecisionSQL = `
INSERT INTO projection_decisions (
    decision_id, decision_type, repository_id, source_run_id,
    work_item_id, subject, confidence_score, confidence_reason,
    provenance_summary, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (decision_id) DO UPDATE
SET decision_type = EXCLUDED.decision_type,
    repository_id = EXCLUDED.repository_id,
    source_run_id = EXCLUDED.source_run_id,
    work_item_id = EXCLUDED.work_item_id,
    subject = EXCLUDED.subject,
    confidence_score = EXCLUDED.confidence_score,
    confidence_reason = EXCLUDED.confidence_reason,
    provenance_summary = EXCLUDED.provenance_summary,
    created_at = EXCLUDED.created_at
`

const upsertEvidenceSQL = `
INSERT INTO projection_decision_evidence (
    evidence_id, decision_id, fact_id, evidence_kind, detail, created_at
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (evidence_id) DO UPDATE
SET decision_id = EXCLUDED.decision_id,
    fact_id = EXCLUDED.fact_id,
    evidence_kind = EXCLUDED.evidence_kind,
    detail = EXCLUDED.detail,
    created_at = EXCLUDED.created_at
`

const listDecisionsSQL = `
SELECT decision_id, decision_type, repository_id, source_run_id,
       work_item_id, subject, confidence_score, confidence_reason,
       provenance_summary, created_at
FROM projection_decisions
WHERE repository_id = $1
  AND source_run_id = $2
  AND ($3 = '' OR decision_type = $3)
ORDER BY created_at ASC, decision_id ASC
LIMIT $4
`

const listEvidenceSQL = `
SELECT evidence_id, decision_id, fact_id, evidence_kind, detail, created_at
FROM projection_decision_evidence
WHERE decision_id = $1
ORDER BY created_at ASC, evidence_id ASC
`

// DecisionFilter specifies query parameters for listing projection decisions.
type DecisionFilter struct {
	RepositoryID string
	SourceRunID  string
	DecisionType *string
	Limit        int
}

// DecisionStore persists projection decisions and evidence in PostgreSQL.
type DecisionStore struct {
	db ExecQueryer
}

// NewDecisionStore creates a decision store backed by the given database.
func NewDecisionStore(db ExecQueryer) *DecisionStore {
	return &DecisionStore{db: db}
}

// EnsureSchema applies the projection decision DDL. This is a no-op in test
// mocks and runs the real DDL against Postgres in production.
func (s *DecisionStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, decisionSchemaSQL)
	return err
}

// UpsertDecision inserts or updates one projection decision.
func (s *DecisionStore) UpsertDecision(ctx context.Context, d projector.ProjectionDecisionRow) error {
	provBytes, err := json.Marshal(d.ProvenanceSummary)
	if err != nil {
		return fmt.Errorf("marshal provenance: %w", err)
	}

	_, err = s.db.ExecContext(ctx, upsertDecisionSQL,
		d.DecisionID,
		d.DecisionType,
		d.RepositoryID,
		d.SourceRunID,
		d.WorkItemID,
		d.Subject,
		d.ConfidenceScore,
		d.ConfidenceReason,
		provBytes,
		d.CreatedAt,
	)
	return err
}

// InsertEvidence inserts or updates evidence rows for projection decisions.
func (s *DecisionStore) InsertEvidence(ctx context.Context, rows []projector.ProjectionDecisionEvidenceRow) error {
	if len(rows) == 0 {
		return nil
	}

	for _, e := range rows {
		detailBytes, err := json.Marshal(e.Detail)
		if err != nil {
			return fmt.Errorf("marshal detail: %w", err)
		}

		var factID any
		if e.FactID != nil {
			factID = *e.FactID
		}

		_, err = s.db.ExecContext(ctx, upsertEvidenceSQL,
			e.EvidenceID,
			e.DecisionID,
			factID,
			e.EvidenceKind,
			detailBytes,
			e.CreatedAt,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// ListDecisions returns persisted decisions for one repository/run pair.
func (s *DecisionStore) ListDecisions(ctx context.Context, f DecisionFilter) ([]projector.ProjectionDecisionRow, error) {
	limit := max(f.Limit, 1)

	decisionType := ""
	if f.DecisionType != nil {
		decisionType = *f.DecisionType
	}

	sqlRows, err := s.db.QueryContext(ctx, listDecisionsSQL,
		f.RepositoryID,
		f.SourceRunID,
		decisionType,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()

	return scanDecisionRows(sqlRows)
}

// ListEvidence returns persisted evidence for one decision.
func (s *DecisionStore) ListEvidence(ctx context.Context, decisionID string) ([]projector.ProjectionDecisionEvidenceRow, error) {
	sqlRows, err := s.db.QueryContext(ctx, listEvidenceSQL, decisionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()

	return scanEvidenceRows(sqlRows)
}

func scanDecisionRows(rows Rows) ([]projector.ProjectionDecisionRow, error) {
	var result []projector.ProjectionDecisionRow
	for rows.Next() {
		var d projector.ProjectionDecisionRow
		var provBytes []byte
		if err := rows.Scan(
			&d.DecisionID,
			&d.DecisionType,
			&d.RepositoryID,
			&d.SourceRunID,
			&d.WorkItemID,
			&d.Subject,
			&d.ConfidenceScore,
			&d.ConfidenceReason,
			&provBytes,
			&d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan decision: %w", err)
		}
		if len(provBytes) > 0 {
			if err := json.Unmarshal(provBytes, &d.ProvenanceSummary); err != nil {
				return nil, fmt.Errorf("unmarshal provenance: %w", err)
			}
		}
		result = append(result, d)
	}

	return result, rows.Err()
}

func scanEvidenceRows(rows Rows) ([]projector.ProjectionDecisionEvidenceRow, error) {
	var result []projector.ProjectionDecisionEvidenceRow
	for rows.Next() {
		var e projector.ProjectionDecisionEvidenceRow
		var factID sql.NullString
		var detailBytes []byte
		if err := rows.Scan(
			&e.EvidenceID,
			&e.DecisionID,
			&factID,
			&e.EvidenceKind,
			&detailBytes,
			&e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan evidence: %w", err)
		}
		if factID.Valid {
			e.FactID = &factID.String
		}
		if len(detailBytes) > 0 {
			if err := json.Unmarshal(detailBytes, &e.Detail); err != nil {
				return nil, fmt.Errorf("unmarshal detail: %w", err)
			}
		}
		result = append(result, e)
	}

	return result, rows.Err()
}

// decisionBootstrapDefinition returns the schema definition for the bootstrap
// registry.
func decisionBootstrapDefinition() Definition {
	return Definition{
		Name: "projection_decisions",
		Path: "schema/data-plane/postgres/007_projection_decisions.sql",
		SQL:  decisionSchemaSQL,
	}
}

// init registers the decision schema in the bootstrap definitions.
func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, decisionBootstrapDefinition())
}
