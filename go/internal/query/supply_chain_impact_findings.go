package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const supplyChainImpactFindingFactKind = "reducer_supply_chain_impact_finding"

// SupplyChainImpactFindingStore reads reducer-owned vulnerability impact
// findings.
type SupplyChainImpactFindingStore interface {
	ListSupplyChainImpactFindings(context.Context, SupplyChainImpactFindingFilter) ([]SupplyChainImpactFindingRow, error)
}

// SupplyChainImpactFindingFilter bounds impact reads to a concrete CVE,
// package, repository, image digest, or status.
type SupplyChainImpactFindingFilter struct {
	CVEID          string
	PackageID      string
	RepositoryID   string
	SubjectDigest  string
	ImpactStatus   string
	AfterFindingID string
	Limit          int
}

// SupplyChainImpactFindingRow is one durable impact finding decoded from
// reducer-owned facts.
type SupplyChainImpactFindingRow struct {
	FindingID           string
	CVEID               string
	AdvisoryID          string
	PackageID           string
	Ecosystem           string
	PackageName         string
	PURL                string
	ObservedVersion     string
	FixedVersion        string
	ImpactStatus        string
	Confidence          string
	CVSSScore           float64
	EPSSProbability     string
	EPSSPercentile      string
	KnownExploited      bool
	PriorityReason      string
	RuntimeReachability string
	RepositoryID        string
	SubjectDigest       string
	MissingEvidence     []string
	EvidencePath        []string
	EvidenceFactIDs     []string
	SourceFreshness     string
	SourceConfidence    string
}

type supplyChainImpactFindingQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresSupplyChainImpactFindingStore reads active impact finding facts from
// Postgres using scoped payload predicates.
type PostgresSupplyChainImpactFindingStore struct {
	DB supplyChainImpactFindingQueryer
}

// NewPostgresSupplyChainImpactFindingStore creates the Postgres-backed impact
// finding read model.
func NewPostgresSupplyChainImpactFindingStore(
	db supplyChainImpactFindingQueryer,
) PostgresSupplyChainImpactFindingStore {
	return PostgresSupplyChainImpactFindingStore{DB: db}
}

// ListSupplyChainImpactFindings returns one bounded page of active reducer
// impact findings.
func (s PostgresSupplyChainImpactFindingStore) ListSupplyChainImpactFindings(
	ctx context.Context,
	filter SupplyChainImpactFindingFilter,
) ([]SupplyChainImpactFindingRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("supply chain impact finding database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("cve_id, package_id, repository_id, subject_digest, or impact_status is required")
	}
	if filter.Limit <= 0 || filter.Limit > supplyChainImpactFindingMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", supplyChainImpactFindingMaxLimit+1)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listSupplyChainImpactFindingsQuery,
		supplyChainImpactFindingFactKind,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.ImpactStatus,
		filter.AfterFindingID,
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list supply chain impact findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]SupplyChainImpactFindingRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var sourceConfidence string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &sourceConfidence, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list supply chain impact findings: %w", err)
		}
		row, err := decodeSupplyChainImpactFindingRow(factID, sourceConfidence, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list supply chain impact findings: %w", err)
	}
	return out, nil
}

const listSupplyChainImpactFindingsQuery = `
SELECT fact.fact_id, fact.source_confidence, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($2 = '' OR fact.payload->>'cve_id' = $2)
  AND ($3 = '' OR fact.payload->>'package_id' = $3)
  AND ($4 = '' OR fact.payload->>'repository_id' = $4)
  AND ($5 = '' OR fact.payload->>'subject_digest' = $5)
  AND ($6 = '' OR fact.payload->>'impact_status' = $6)
  AND ($7 = '' OR fact.fact_id > $7)
ORDER BY fact.fact_id ASC
LIMIT $8
`

func (f SupplyChainImpactFindingFilter) hasScope() bool {
	return f.CVEID != "" || f.PackageID != "" || f.RepositoryID != "" ||
		f.SubjectDigest != "" || f.ImpactStatus != ""
}

func decodeSupplyChainImpactFindingRow(
	factID string,
	sourceConfidence string,
	payloadBytes []byte,
) (SupplyChainImpactFindingRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return SupplyChainImpactFindingRow{}, fmt.Errorf("decode supply chain impact finding: %w", err)
	}
	return SupplyChainImpactFindingRow{
		FindingID:           factID,
		CVEID:               StringVal(payload, "cve_id"),
		AdvisoryID:          StringVal(payload, "advisory_id"),
		PackageID:           StringVal(payload, "package_id"),
		Ecosystem:           StringVal(payload, "ecosystem"),
		PackageName:         StringVal(payload, "package_name"),
		PURL:                StringVal(payload, "purl"),
		ObservedVersion:     StringVal(payload, "observed_version"),
		FixedVersion:        StringVal(payload, "fixed_version"),
		ImpactStatus:        StringVal(payload, "impact_status"),
		Confidence:          StringVal(payload, "confidence"),
		CVSSScore:           floatVal(payload, "cvss_score"),
		EPSSProbability:     StringVal(payload, "epss_probability"),
		EPSSPercentile:      StringVal(payload, "epss_percentile"),
		KnownExploited:      BoolVal(payload, "known_exploited"),
		PriorityReason:      StringVal(payload, "priority_reason"),
		RuntimeReachability: StringVal(payload, "runtime_reachability"),
		RepositoryID:        StringVal(payload, "repository_id"),
		SubjectDigest:       StringVal(payload, "subject_digest"),
		MissingEvidence:     StringSliceVal(payload, "missing_evidence"),
		EvidencePath:        StringSliceVal(payload, "evidence_path"),
		EvidenceFactIDs:     StringSliceVal(payload, "evidence_fact_ids"),
		SourceFreshness:     "active",
		SourceConfidence:    sourceConfidence,
	}, nil
}
