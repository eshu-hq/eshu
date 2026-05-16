package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const (
	packageOwnershipCorrelationFactKind   = "reducer_package_ownership_correlation"
	packageConsumptionCorrelationFactKind = "reducer_package_consumption_correlation"
	packagePublicationCorrelationFactKind = "reducer_package_publication_correlation"
)

// PackageRegistryCorrelationStore reads reducer-owned package correlations.
type PackageRegistryCorrelationStore interface {
	ListPackageRegistryCorrelations(
		context.Context,
		PackageRegistryCorrelationFilter,
	) ([]PackageRegistryCorrelationRow, error)
}

// PackageRegistryCorrelationFilter bounds package correlation reads to one
// package or repository, with optional relationship-kind and cursor filters.
type PackageRegistryCorrelationFilter struct {
	PackageID          string
	RepositoryID       string
	RelationshipKind   string
	AfterCorrelationID string
	Limit              int
}

// PackageRegistryCorrelationRow is one durable package ownership, publication,
// or consumption row decoded from reducer fact payloads.
type PackageRegistryCorrelationRow struct {
	CorrelationID          string
	RelationshipKind       string
	PackageID              string
	VersionID              string
	Version                string
	PublishedAt            string
	Ecosystem              string
	PackageName            string
	RepositoryID           string
	RepositoryName         string
	SourceURL              string
	CandidateRepositoryIDs []string
	RelativePath           string
	ManifestSection        string
	DependencyRange        string
	Outcome                string
	Reason                 string
	ProvenanceOnly         bool
	CanonicalWrites        int
	EvidenceFactIDs        []string
}

type packageRegistryCorrelationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresPackageRegistryCorrelationStore reads reducer package correlation
// facts from Postgres with package/repository scoped filters.
type PostgresPackageRegistryCorrelationStore struct {
	DB packageRegistryCorrelationQueryer
}

// NewPostgresPackageRegistryCorrelationStore creates the Postgres-backed
// package correlation read model.
func NewPostgresPackageRegistryCorrelationStore(db packageRegistryCorrelationQueryer) PostgresPackageRegistryCorrelationStore {
	return PostgresPackageRegistryCorrelationStore{DB: db}
}

// ListPackageRegistryCorrelations returns a bounded page of active reducer
// package ownership, publication, or consumption correlation facts.
func (s PostgresPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	ctx context.Context,
	filter PackageRegistryCorrelationFilter,
) ([]PackageRegistryCorrelationRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("package registry correlation database is required")
	}
	if filter.PackageID == "" && filter.RepositoryID == "" {
		return nil, fmt.Errorf("package_id or repository_id is required")
	}
	if filter.Limit <= 0 || filter.Limit > packageRegistryMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", packageRegistryMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listPackageRegistryCorrelationsQuery,
		packageRegistryCorrelationFactKinds(),
		filter.PackageID,
		filter.RepositoryID,
		filter.RelationshipKind,
		filter.AfterCorrelationID,
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list package registry correlations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]PackageRegistryCorrelationRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list package registry correlations: %w", err)
		}
		row, err := decodePackageRegistryCorrelationRow(factID, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list package registry correlations: %w", err)
	}
	return out, nil
}

const listPackageRegistryCorrelationsQuery = `
SELECT fact.fact_id, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = ANY($1::text[])
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($2 = '' OR fact.payload->>'package_id' = $2)
  AND ($3 = '' OR fact.payload->>'repository_id' = $3)
  AND ($4 = '' OR fact.payload->>'relationship_kind' = $4)
  AND ($5 = '' OR fact.fact_id > $5)
ORDER BY fact.fact_id ASC
LIMIT $6
`

func packageRegistryCorrelationFactKinds() []string {
	return []string{
		packageOwnershipCorrelationFactKind,
		packageConsumptionCorrelationFactKind,
		packagePublicationCorrelationFactKind,
	}
}

func decodePackageRegistryCorrelationRow(
	factID string,
	payloadBytes []byte,
) (PackageRegistryCorrelationRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return PackageRegistryCorrelationRow{}, fmt.Errorf("decode package registry correlation: %w", err)
	}
	return PackageRegistryCorrelationRow{
		CorrelationID:          factID,
		RelationshipKind:       StringVal(payload, "relationship_kind"),
		PackageID:              StringVal(payload, "package_id"),
		VersionID:              StringVal(payload, "version_id"),
		Version:                StringVal(payload, "version"),
		PublishedAt:            StringVal(payload, "published_at"),
		Ecosystem:              StringVal(payload, "ecosystem"),
		PackageName:            StringVal(payload, "package_name"),
		RepositoryID:           StringVal(payload, "repository_id"),
		RepositoryName:         StringVal(payload, "repository_name"),
		SourceURL:              StringVal(payload, "source_url"),
		CandidateRepositoryIDs: StringSliceVal(payload, "candidate_repository_ids"),
		RelativePath:           StringVal(payload, "relative_path"),
		ManifestSection:        StringVal(payload, "manifest_section"),
		DependencyRange:        StringVal(payload, "dependency_range"),
		Outcome:                StringVal(payload, "outcome"),
		Reason:                 StringVal(payload, "reason"),
		ProvenanceOnly:         BoolVal(payload, "provenance_only"),
		CanonicalWrites:        IntVal(payload, "canonical_writes"),
		EvidenceFactIDs:        StringSliceVal(payload, "evidence_fact_ids"),
	}, nil
}
