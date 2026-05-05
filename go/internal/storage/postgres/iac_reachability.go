package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	iacReachabilityBatchSize = 500
	iacReachabilityColumns   = 13
)

const iacReachabilitySchemaSQL = `
CREATE TABLE IF NOT EXISTS iac_reachability_rows (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    repo_id TEXT NOT NULL,
    family TEXT NOT NULL,
    artifact_path TEXT NOT NULL,
    artifact_name TEXT NOT NULL,
    reachability TEXT NOT NULL,
    finding TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL,
    evidence JSONB NOT NULL,
    limitations JSONB NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, repo_id, family, artifact_path)
);

CREATE INDEX IF NOT EXISTS iac_reachability_cleanup_idx
    ON iac_reachability_rows (scope_id, generation_id, reachability, family, repo_id);

CREATE INDEX IF NOT EXISTS iac_reachability_artifact_idx
    ON iac_reachability_rows (repo_id, family, artifact_path);
`

const upsertIaCReachabilityBatchPrefix = `
INSERT INTO iac_reachability_rows (
    scope_id, generation_id, repo_id, family, artifact_path,
    artifact_name, reachability, finding, confidence,
    evidence, limitations, observed_at, updated_at
) VALUES `

const upsertIaCReachabilityBatchSuffix = `
ON CONFLICT (scope_id, generation_id, repo_id, family, artifact_path) DO UPDATE
SET artifact_name = EXCLUDED.artifact_name,
    reachability = EXCLUDED.reachability,
    finding = EXCLUDED.finding,
    confidence = EXCLUDED.confidence,
    evidence = EXCLUDED.evidence,
    limitations = EXCLUDED.limitations,
    observed_at = EXCLUDED.observed_at,
    updated_at = EXCLUDED.updated_at
`

const listIaCCleanupFindingsSQL = `
SELECT scope_id, generation_id, repo_id, family, artifact_path,
       artifact_name, reachability, finding, confidence,
       evidence, limitations, observed_at, updated_at
FROM iac_reachability_rows
WHERE scope_id = $1
  AND generation_id = $2
  AND (
      reachability = 'unused'
      OR ($3 = true AND reachability = 'ambiguous')
  )
ORDER BY family, artifact_path
LIMIT $4
OFFSET $5
`

// IaCReachability identifies the materialized reachability class for one IaC
// artifact.
type IaCReachability string

const (
	// IaCReachabilityUsed means the artifact has a modeled reference.
	IaCReachabilityUsed IaCReachability = "used"
	// IaCReachabilityUnused means no modeled reference reaches the artifact.
	IaCReachabilityUnused IaCReachability = "unused"
	// IaCReachabilityAmbiguous means dynamic evidence prevents a dead/not-dead
	// conclusion.
	IaCReachabilityAmbiguous IaCReachability = "ambiguous"
)

// IaCFinding classifies the operator-facing cleanup finding.
type IaCFinding string

const (
	// IaCFindingInUse marks artifacts that must not be cleanup candidates.
	IaCFindingInUse IaCFinding = "in_use"
	// IaCFindingCandidateDead marks unreferenced artifacts as cleanup
	// candidates.
	IaCFindingCandidateDead IaCFinding = "candidate_dead_iac"
	// IaCFindingAmbiguousDynamic marks dynamic references that need stronger
	// renderer or runtime evidence.
	IaCFindingAmbiguousDynamic IaCFinding = "ambiguous_dynamic_reference"
)

// IaCReachabilityRow is one durable IaC usage/finding row materialized by the
// reducer for query/API consumption.
type IaCReachabilityRow struct {
	ScopeID      string
	GenerationID string
	RepoID       string
	Family       string
	ArtifactPath string
	ArtifactName string
	Reachability IaCReachability
	Finding      IaCFinding
	Confidence   float64
	Evidence     []string
	Limitations  []string
	ObservedAt   time.Time
	UpdatedAt    time.Time
}

// IaCReachabilityStore persists reducer-materialized IaC reachability rows.
type IaCReachabilityStore struct {
	db ExecQueryer
}

// NewIaCReachabilityStore creates a Postgres-backed IaC reachability store.
func NewIaCReachabilityStore(db ExecQueryer) *IaCReachabilityStore {
	return &IaCReachabilityStore{db: db}
}

// IaCReachabilitySchemaSQL returns the DDL for IaC reachability rows.
func IaCReachabilitySchemaSQL() string {
	return iacReachabilitySchemaSQL
}

// EnsureSchema applies the IaC reachability DDL.
func (s *IaCReachabilityStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, iacReachabilitySchemaSQL)
	return err
}

// Upsert writes IaC reachability rows in bounded batches.
func (s *IaCReachabilityStore) Upsert(ctx context.Context, rows []IaCReachabilityRow) error {
	if len(rows) == 0 {
		return nil
	}
	for i := 0; i < len(rows); i += iacReachabilityBatchSize {
		end := i + iacReachabilityBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := upsertIaCReachabilityBatch(ctx, s.db, rows[i:end]); err != nil {
			return err
		}
	}
	return nil
}

// ListCleanupFindings returns materialized unused rows and, when requested,
// ambiguous rows. Used rows are intentionally excluded from cleanup findings.
func (s *IaCReachabilityStore) ListCleanupFindings(
	ctx context.Context,
	scopeID string,
	generationID string,
	includeAmbiguous bool,
	limit int,
	offset int,
) ([]IaCReachabilityRow, error) {
	limit, offset = normalizeIaCReachabilityPaging(limit, offset)
	rows, err := s.db.QueryContext(ctx, listIaCCleanupFindingsSQL, scopeID, generationID, includeAmbiguous, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query IaC cleanup findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]IaCReachabilityRow, 0)
	for rows.Next() {
		row, err := scanIaCReachabilityRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// ListLatestCleanupFindings returns cleanup findings for active generations of
// the requested repositories.
func (s *IaCReachabilityStore) ListLatestCleanupFindings(
	ctx context.Context,
	repoIDs []string,
	families []string,
	includeAmbiguous bool,
	limit int,
	offset int,
) ([]IaCReachabilityRow, error) {
	if len(repoIDs) == 0 {
		return nil, nil
	}
	limit, offset = normalizeIaCReachabilityPaging(limit, offset)
	query, args := buildListLatestIaCCleanupFindingsQuery(repoIDs, families, includeAmbiguous, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query latest IaC cleanup findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]IaCReachabilityRow, 0)
	for rows.Next() {
		row, err := scanIaCReachabilityRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// CountLatestCleanupFindings returns the unpaged count for active-generation
// cleanup findings matching the requested repository and family filters.
func (s *IaCReachabilityStore) CountLatestCleanupFindings(
	ctx context.Context,
	repoIDs []string,
	families []string,
	includeAmbiguous bool,
) (int, error) {
	if len(repoIDs) == 0 {
		return 0, nil
	}
	query, args := buildCountLatestIaCCleanupFindingsQuery(repoIDs, families, includeAmbiguous)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("count latest IaC cleanup findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, rows.Err()
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan latest IaC cleanup count: %w", err)
	}
	return count, rows.Err()
}

// HasLatestRows reports whether active-generation reachability rows exist for
// the requested repositories and optional families.
func (s *IaCReachabilityStore) HasLatestRows(
	ctx context.Context,
	repoIDs []string,
	families []string,
) (bool, error) {
	if len(repoIDs) == 0 {
		return false, nil
	}
	query, args := buildHasLatestIaCReachabilityRowsQuery(repoIDs, families)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("query latest IaC reachability row existence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		return true, rows.Err()
	}
	return false, rows.Err()
}

func buildListLatestIaCCleanupFindingsQuery(
	repoIDs []string,
	families []string,
	includeAmbiguous bool,
	limit int,
	offset int,
) (string, []any) {
	args := make([]any, 0, len(repoIDs)+2)
	repoPlaceholders := appendPlaceholders(&args, repoIDs)
	familyClause := buildFamilyFilterClause(&args, families)
	includeIdx := len(args) + 1
	limitIdx := len(args) + 2
	offsetIdx := len(args) + 3
	args = append(args, includeAmbiguous, limit, offset)

	query := fmt.Sprintf(`
SELECT row.scope_id, row.generation_id, row.repo_id, row.family, row.artifact_path,
       row.artifact_name, row.reachability, row.finding, row.confidence,
       row.evidence, row.limitations, row.observed_at, row.updated_at
FROM iac_reachability_rows AS row
JOIN scope_generations AS generation
  ON generation.scope_id = row.scope_id
 AND generation.generation_id = row.generation_id
WHERE generation.status = 'active'
  AND row.repo_id IN (%s)
  %s
  AND (
      row.reachability = 'unused'
      OR ($%d = true AND row.reachability = 'ambiguous')
  )
ORDER BY row.family, row.artifact_path
LIMIT $%d
OFFSET $%d
`, repoPlaceholders, familyClause, includeIdx, limitIdx, offsetIdx)
	return query, args
}

func buildCountLatestIaCCleanupFindingsQuery(
	repoIDs []string,
	families []string,
	includeAmbiguous bool,
) (string, []any) {
	args := make([]any, 0, len(repoIDs)+len(families)+1)
	repoPlaceholders := appendPlaceholders(&args, repoIDs)
	familyClause := buildFamilyFilterClause(&args, families)
	includeIdx := len(args) + 1
	args = append(args, includeAmbiguous)
	query := fmt.Sprintf(`
SELECT COUNT(*)
FROM iac_reachability_rows AS row
JOIN scope_generations AS generation
  ON generation.scope_id = row.scope_id
 AND generation.generation_id = row.generation_id
WHERE generation.status = 'active'
  AND row.repo_id IN (%s)
  %s
  AND (
      row.reachability = 'unused'
      OR ($%d = true AND row.reachability = 'ambiguous')
  )
`, repoPlaceholders, familyClause, includeIdx)
	return query, args
}

func normalizeIaCReachabilityPaging(limit int, offset int) (int, int) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func buildHasLatestIaCReachabilityRowsQuery(repoIDs []string, families []string) (string, []any) {
	args := make([]any, 0, len(repoIDs)+len(families))
	repoPlaceholders := appendPlaceholders(&args, repoIDs)
	familyClause := buildFamilyFilterClause(&args, families)
	query := fmt.Sprintf(`
SELECT 1
FROM iac_reachability_rows AS row
JOIN scope_generations AS generation
  ON generation.scope_id = row.scope_id
 AND generation.generation_id = row.generation_id
WHERE generation.status = 'active'
  AND row.repo_id IN (%s)
  %s
LIMIT 1
`, repoPlaceholders, familyClause)
	return query, args
}

func appendPlaceholders(args *[]any, values []string) string {
	var placeholders strings.Builder
	for _, value := range values {
		if placeholders.Len() > 0 {
			placeholders.WriteString(", ")
		}
		*args = append(*args, value)
		fmt.Fprintf(&placeholders, "$%d", len(*args))
	}
	return placeholders.String()
}

func buildFamilyFilterClause(args *[]any, families []string) string {
	if len(families) == 0 {
		return ""
	}
	placeholders := appendPlaceholders(args, families)
	return fmt.Sprintf("AND row.family IN (%s)", placeholders)
}

func upsertIaCReachabilityBatch(ctx context.Context, db ExecQueryer, batch []IaCReachabilityRow) error {
	args := make([]any, 0, len(batch)*iacReachabilityColumns)
	var values strings.Builder
	for i, row := range batch {
		evidence, err := json.Marshal(row.Evidence)
		if err != nil {
			return fmt.Errorf("marshal IaC evidence: %w", err)
		}
		limitations, err := json.Marshal(row.Limitations)
		if err != nil {
			return fmt.Errorf("marshal IaC limitations: %w", err)
		}
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * iacReachabilityColumns
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6,
			offset+7, offset+8, offset+9, offset+10, offset+11, offset+12, offset+13,
		)
		args = append(args,
			row.ScopeID,
			row.GenerationID,
			row.RepoID,
			row.Family,
			row.ArtifactPath,
			row.ArtifactName,
			string(row.Reachability),
			string(row.Finding),
			row.Confidence,
			evidence,
			limitations,
			row.ObservedAt,
			row.UpdatedAt,
		)
	}
	query := upsertIaCReachabilityBatchPrefix + values.String() + upsertIaCReachabilityBatchSuffix
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert IaC reachability batch (%d rows): %w", len(batch), err)
	}
	return nil
}

func scanIaCReachabilityRow(rows Rows) (IaCReachabilityRow, error) {
	var row IaCReachabilityRow
	var reachability, finding string
	var evidence, limitations []byte
	if err := rows.Scan(
		&row.ScopeID,
		&row.GenerationID,
		&row.RepoID,
		&row.Family,
		&row.ArtifactPath,
		&row.ArtifactName,
		&reachability,
		&finding,
		&row.Confidence,
		&evidence,
		&limitations,
		&row.ObservedAt,
		&row.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return IaCReachabilityRow{}, err
		}
		return IaCReachabilityRow{}, fmt.Errorf("scan IaC reachability row: %w", err)
	}
	row.Reachability = IaCReachability(reachability)
	row.Finding = IaCFinding(finding)
	if err := json.Unmarshal(evidence, &row.Evidence); err != nil {
		return IaCReachabilityRow{}, fmt.Errorf("unmarshal IaC evidence: %w", err)
	}
	if err := json.Unmarshal(limitations, &row.Limitations); err != nil {
		return IaCReachabilityRow{}, fmt.Errorf("unmarshal IaC limitations: %w", err)
	}
	return row, nil
}

func iacReachabilityBootstrapDefinition() Definition {
	return Definition{
		Name: "iac_reachability",
		Path: "schema/data-plane/postgres/016_iac_reachability.sql",
		SQL:  iacReachabilitySchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, iacReachabilityBootstrapDefinition())
}
