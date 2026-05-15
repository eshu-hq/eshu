package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AWSCloudRuntimeDriftFindingFactKind is the durable reducer fact emitted for
// AWS runtime drift findings.
const AWSCloudRuntimeDriftFindingFactKind = "reducer_aws_cloud_runtime_drift_finding"

const (
	awsCloudRuntimeDriftFindingDefaultLimit = 100
	awsCloudRuntimeDriftFindingMaxLimit     = 500
)

// AWSCloudRuntimeDriftFindingFilter bounds active AWS drift finding reads. The
// store rejects filters without ScopeID or AccountID and caps list pages.
type AWSCloudRuntimeDriftFindingFilter struct {
	ScopeID      string
	AccountID    string
	Region       string
	FindingKinds []string
	Limit        int
	Offset       int
}

// AWSCloudRuntimeDriftFindingRow is one active reducer finding loaded from
// fact_records.
type AWSCloudRuntimeDriftFindingRow struct {
	FactID        string
	ScopeID       string
	GenerationID  string
	SourceSystem  string
	ObservedAt    time.Time
	CanonicalID   string
	CandidateID   string
	CandidateKind string
	ARN           string
	FindingKind   string
	Confidence    float64
	Evidence      []AWSCloudRuntimeDriftEvidenceRow
}

// AWSCloudRuntimeDriftEvidenceRow preserves the reducer evidence atoms used to
// explain an AWS management finding.
type AWSCloudRuntimeDriftEvidenceRow struct {
	ID           string  `json:"id"`
	SourceSystem string  `json:"source_system"`
	EvidenceType string  `json:"evidence_type"`
	ScopeID      string  `json:"scope_id"`
	Key          string  `json:"key"`
	Value        string  `json:"value"`
	Confidence   float64 `json:"confidence"`
}

// AWSCloudRuntimeDriftFindingStore reads active AWS runtime drift reducer facts.
type AWSCloudRuntimeDriftFindingStore struct {
	db ExecQueryer
}

// NewAWSCloudRuntimeDriftFindingStore constructs an AWS runtime drift finding
// reader over the provided database adapter.
func NewAWSCloudRuntimeDriftFindingStore(db ExecQueryer) AWSCloudRuntimeDriftFindingStore {
	return AWSCloudRuntimeDriftFindingStore{db: db}
}

// ListActiveFindings returns one page of active AWS runtime drift findings for
// the caller's bounded scope.
func (s AWSCloudRuntimeDriftFindingStore) ListActiveFindings(
	ctx context.Context,
	filter AWSCloudRuntimeDriftFindingFilter,
) ([]AWSCloudRuntimeDriftFindingRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("aws cloud runtime drift finding store database is required")
	}
	filter = normalizeAWSCloudRuntimeDriftFindingFilter(filter)
	if err := validateAWSCloudRuntimeDriftFindingFilter(filter); err != nil {
		return nil, err
	}
	query, args := buildAWSCloudRuntimeDriftFindingQuery(false, filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active AWS runtime drift findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var findings []AWSCloudRuntimeDriftFindingRow
	for rows.Next() {
		var row AWSCloudRuntimeDriftFindingRow
		var payload []byte
		if err := rows.Scan(
			&row.FactID,
			&row.ScopeID,
			&row.GenerationID,
			&row.SourceSystem,
			&row.ObservedAt,
			&payload,
		); err != nil {
			return nil, fmt.Errorf("scan active AWS runtime drift finding: %w", err)
		}
		if err := decodeAWSCloudRuntimeDriftFindingPayload(payload, &row); err != nil {
			return nil, err
		}
		findings = append(findings, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active AWS runtime drift findings: %w", err)
	}
	return findings, nil
}

// CountActiveFindings returns the total active finding count for the same
// bounded filters used by ListActiveFindings.
func (s AWSCloudRuntimeDriftFindingStore) CountActiveFindings(
	ctx context.Context,
	filter AWSCloudRuntimeDriftFindingFilter,
) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("aws cloud runtime drift finding store database is required")
	}
	filter = normalizeAWSCloudRuntimeDriftFindingFilter(filter)
	if err := validateAWSCloudRuntimeDriftFindingFilter(filter); err != nil {
		return 0, err
	}
	query, args := buildAWSCloudRuntimeDriftFindingQuery(true, filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("count active AWS runtime drift findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, nil
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan active AWS runtime drift finding count: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate active AWS runtime drift finding count: %w", err)
	}
	return count, nil
}

type awsCloudRuntimeDriftFindingPayload struct {
	CanonicalID   string                            `json:"canonical_id"`
	CandidateID   string                            `json:"candidate_id"`
	CandidateKind string                            `json:"candidate_kind"`
	ARN           string                            `json:"arn"`
	FindingKind   string                            `json:"finding_kind"`
	Confidence    float64                           `json:"confidence"`
	Evidence      []AWSCloudRuntimeDriftEvidenceRow `json:"evidence"`
}

func decodeAWSCloudRuntimeDriftFindingPayload(
	payload []byte,
	row *AWSCloudRuntimeDriftFindingRow,
) error {
	var decoded awsCloudRuntimeDriftFindingPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("decode AWS runtime drift finding payload: %w", err)
	}
	row.CanonicalID = decoded.CanonicalID
	row.CandidateID = decoded.CandidateID
	row.CandidateKind = decoded.CandidateKind
	row.ARN = decoded.ARN
	row.FindingKind = decoded.FindingKind
	row.Confidence = decoded.Confidence
	row.Evidence = append([]AWSCloudRuntimeDriftEvidenceRow(nil), decoded.Evidence...)
	return nil
}

func buildAWSCloudRuntimeDriftFindingQuery(
	countOnly bool,
	filter AWSCloudRuntimeDriftFindingFilter,
) (string, []any) {
	selectClause := strings.Join([]string{
		"fact.fact_id",
		"fact.scope_id",
		"fact.generation_id",
		"fact.source_system",
		"fact.observed_at",
		"fact.payload",
	}, ",\n    ")
	if countOnly {
		selectClause = "COUNT(*)"
	}

	args := []any{AWSCloudRuntimeDriftFindingFactKind}
	conditions := []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = false",
	}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if filter.ScopeID != "" {
		conditions = append(conditions, "fact.scope_id = "+addArg(filter.ScopeID))
	} else if filter.AccountID != "" {
		conditions = append(conditions, "fact.scope_id LIKE "+addArg(awsScopePrefix(filter.AccountID, filter.Region)))
	}
	if len(filter.FindingKinds) > 0 {
		placeholders := make([]string, 0, len(filter.FindingKinds))
		for _, kind := range filter.FindingKinds {
			placeholders = append(placeholders, addArg(kind))
		}
		conditions = append(conditions, "fact.payload->>'finding_kind' IN ("+strings.Join(placeholders, ", ")+")")
	}

	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "SELECT\n    %s\n", selectClause)
	builder.WriteString("FROM fact_records AS fact\n")
	builder.WriteString("JOIN ingestion_scopes AS scope\n")
	builder.WriteString("  ON scope.scope_id = fact.scope_id\n")
	builder.WriteString(" AND scope.active_generation_id = fact.generation_id\n")
	builder.WriteString("WHERE ")
	builder.WriteString(strings.Join(conditions, "\n  AND "))
	builder.WriteString("\n")
	if !countOnly {
		limit := addArg(filter.Limit)
		offset := addArg(filter.Offset)
		builder.WriteString("ORDER BY fact.observed_at DESC, fact.fact_id ASC\n")
		_, _ = fmt.Fprintf(&builder, "LIMIT %s OFFSET %s\n", limit, offset)
	}
	return builder.String(), args
}

func normalizeAWSCloudRuntimeDriftFindingFilter(
	filter AWSCloudRuntimeDriftFindingFilter,
) AWSCloudRuntimeDriftFindingFilter {
	filter.ScopeID = strings.TrimSpace(filter.ScopeID)
	filter.AccountID = strings.TrimSpace(filter.AccountID)
	filter.Region = strings.TrimSpace(filter.Region)
	filter.FindingKinds = cleanStringSet(filter.FindingKinds)
	if filter.Limit <= 0 {
		filter.Limit = awsCloudRuntimeDriftFindingDefaultLimit
	}
	if filter.Limit > awsCloudRuntimeDriftFindingMaxLimit {
		filter.Limit = awsCloudRuntimeDriftFindingMaxLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func validateAWSCloudRuntimeDriftFindingFilter(
	filter AWSCloudRuntimeDriftFindingFilter,
) error {
	if filter.ScopeID == "" && filter.AccountID == "" {
		return fmt.Errorf("aws cloud runtime drift finding filter requires scope_id or account_id")
	}
	return nil
}

func awsScopePrefix(accountID string, region string) string {
	prefix := "aws:" + accountID + ":"
	if region != "" {
		prefix += region + ":"
	}
	return prefix + "%"
}

func cleanStringSet(values []string) []string {
	seen := map[string]struct{}{}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}
