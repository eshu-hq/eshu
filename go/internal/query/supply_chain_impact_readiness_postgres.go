package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lib/pq"
)

const supplyChainImpactReadinessFreshnessWindow = 14 * 24 * time.Hour

type supplyChainImpactReadinessQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresSupplyChainImpactReadinessStore reads bounded source-fact counts and
// freshness per evidence family from the active fact-record snapshot. It never
// invents findings or duplicates reducer matching: it only reports counts and
// observed-at timestamps the API handler classifies into a readiness state.
type PostgresSupplyChainImpactReadinessStore struct {
	DB              supplyChainImpactReadinessQueryer
	FreshnessWindow time.Duration
}

// NewPostgresSupplyChainImpactReadinessStore creates a Postgres-backed
// readiness store with the default 14-day freshness window.
func NewPostgresSupplyChainImpactReadinessStore(
	db supplyChainImpactReadinessQueryer,
) PostgresSupplyChainImpactReadinessStore {
	return PostgresSupplyChainImpactReadinessStore{
		DB:              db,
		FreshnessWindow: supplyChainImpactReadinessFreshnessWindow,
	}
}

// ReadSupplyChainImpactReadiness returns one snapshot of evidence-family
// counts, latest observation, and freshness for the bounded readiness query.
//
// The store requires a fact-anchored scope (CVE, package, repository, or
// subject digest). An impact_status-only scope returns an empty snapshot
// without scanning fact_records, because impact_status is a reducer-finding
// attribute that does not exist on source facts; running an unanchored scan
// across the active fact set would be expensive without producing
// scope-relevant coverage.
func (s PostgresSupplyChainImpactReadinessStore) ReadSupplyChainImpactReadiness(
	ctx context.Context,
	query SupplyChainImpactReadinessQuery,
) (SupplyChainImpactReadinessSnapshot, error) {
	if s.DB == nil {
		return SupplyChainImpactReadinessSnapshot{}, fmt.Errorf("supply chain impact readiness database is required")
	}
	if !query.hasFactAnchor() {
		return SupplyChainImpactReadinessSnapshot{}, nil
	}
	window := s.FreshnessWindow
	if window <= 0 {
		window = supplyChainImpactReadinessFreshnessWindow
	}
	freshnessCutoff := time.Now().UTC().Add(-window)

	rows, err := s.DB.QueryContext(
		ctx,
		listSupplyChainImpactReadinessQuery,
		pq.Array(vulnerabilityAdvisoryFactKinds),
		pq.Array(vulnerabilityExploitabilityFactKinds),
		pq.Array(packageConsumptionCorrelationFactKinds),
		pq.Array(packageRegistryFactKinds),
		pq.Array(sbomComponentFactKinds),
		pq.Array(sbomAttestationFactKinds),
		pq.Array(containerImageIdentityFactKinds),
		pq.Array(vulnerabilitySourceSnapshotFactKinds),
		query.CVEID,
		query.PackageID,
		query.RepositoryID,
		query.SubjectDigest,
	)
	if err != nil {
		return SupplyChainImpactReadinessSnapshot{}, fmt.Errorf("read supply chain impact readiness: %w", err)
	}
	defer func() { _ = rows.Close() }()

	families := make(map[string]SupplyChainImpactEvidenceFamily, len(supplyChainImpactReadinessFamilies))
	var targetIncomplete bool
	var incompleteReasons []string
	var sourceSnapshots []SupplyChainImpactSourceSnapshot

	for rows.Next() {
		var family string
		var factCount int
		var latest sql.NullTime
		var incompleteFlag sql.NullBool
		var reasons pq.StringArray
		var sourceSnapshotsJSON sql.NullString
		if err := rows.Scan(&family, &factCount, &latest, &incompleteFlag, &reasons, &sourceSnapshotsJSON); err != nil {
			return SupplyChainImpactReadinessSnapshot{}, fmt.Errorf("scan supply chain impact readiness row: %w", err)
		}
		if family == sourceSnapshotFamilyMarker {
			if incompleteFlag.Valid && incompleteFlag.Bool {
				targetIncomplete = true
				incompleteReasons = append(incompleteReasons, reasons...)
			}
			decodedSnapshots, err := decodeSourceSnapshots(sourceSnapshotsJSON)
			if err != nil {
				return SupplyChainImpactReadinessSnapshot{}, err
			}
			sourceSnapshots = append(sourceSnapshots, decodedSnapshots...)
			continue
		}
		existing, ok := families[family]
		if !ok {
			existing = SupplyChainImpactEvidenceFamily{Family: family}
		}
		existing.FactCount += factCount
		if latest.Valid {
			latestUTC := latest.Time.UTC()
			if existing.LatestObservedAt == "" || latestUTC.Format(time.RFC3339) > existing.LatestObservedAt {
				existing.LatestObservedAt = latestUTC.Format(time.RFC3339)
				if latestUTC.After(freshnessCutoff) {
					existing.Freshness = FreshnessLabelFresh
				} else {
					existing.Freshness = FreshnessLabelStale
				}
			}
		}
		families[family] = existing
	}
	if err := rows.Err(); err != nil {
		return SupplyChainImpactReadinessSnapshot{}, fmt.Errorf("read supply chain impact readiness: %w", err)
	}

	sources := make([]SupplyChainImpactEvidenceFamily, 0, len(families))
	for _, family := range supplyChainImpactReadinessFamilies {
		entry, ok := families[family]
		if !ok {
			continue
		}
		if entry.FactCount == 0 {
			continue
		}
		if entry.Freshness == "" {
			entry.Freshness = FreshnessLabelUnknown
		}
		sources = append(sources, entry)
	}

	return SupplyChainImpactReadinessSnapshot{
		EvidenceSources:   sources,
		SourceSnapshots:   sourceSnapshots,
		TargetIncomplete:  targetIncomplete,
		IncompleteReasons: uniqueSortedReadinessStrings(incompleteReasons),
	}, nil
}

func decodeSourceSnapshots(raw sql.NullString) ([]SupplyChainImpactSourceSnapshot, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var snapshots []SupplyChainImpactSourceSnapshot
	if err := json.Unmarshal([]byte(raw.String), &snapshots); err != nil {
		return nil, fmt.Errorf("decode vulnerability source snapshot metadata: %w", err)
	}
	return snapshots, nil
}

const sourceSnapshotFamilyMarker = "vulnerability.source_snapshot"

// supplyChainImpactReadinessFamilies orders the evidence-family identifiers
// emitted by the readiness store. Iteration order is fixed so JSON output and
// regression tests stay deterministic regardless of map walk order.
var supplyChainImpactReadinessFamilies = []string{
	EvidenceFamilyContainerImageIdentity,
	EvidenceFamilyPackageConsumption,
	EvidenceFamilyPackageRegistry,
	EvidenceFamilySBOMAttestation,
	EvidenceFamilySBOMComponent,
	EvidenceFamilyVulnerabilityAdvisory,
	EvidenceFamilyVulnerabilityExploitability,
}

var (
	vulnerabilityAdvisoryFactKinds = []string{
		"vulnerability.cve",
		"vulnerability.affected_package",
		"vulnerability.affected_product",
	}
	vulnerabilityExploitabilityFactKinds = []string{
		"vulnerability.epss_score",
		"vulnerability.known_exploited",
	}
	packageConsumptionCorrelationFactKinds = []string{
		"reducer_package_consumption_correlation",
	}
	packageRegistryFactKinds = []string{
		"package_registry.package",
		"package_registry.package_version",
	}
	sbomComponentFactKinds               = []string{"sbom.component"}
	sbomAttestationFactKinds             = []string{"reducer_sbom_attestation_attachment"}
	containerImageIdentityFactKinds      = []string{"reducer_container_image_identity"}
	vulnerabilitySourceSnapshotFactKinds = []string{"vulnerability.source_snapshot"}
)

const listSupplyChainImpactReadinessQuery = `
WITH advisory_active AS (
    SELECT fact.payload, fact.observed_at
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
),
exploitability_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($2::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
package_consumption_correlation_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($3::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
package_manifest_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'content_entity'
      AND fact.source_system = 'git'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.payload->>'entity_type' = 'Variable'
      AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
),
package_registry_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($4::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
sbom_component_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($5::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
sbom_attestation_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($6::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
container_image_identity_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($7::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
vulnerability_source_snapshot_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($8::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
vulnerability_advisory AS (
    SELECT
        'vulnerability.advisory' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json
    FROM advisory_active
    WHERE ($9 = '' OR payload->>'cve_id' = $9)
),
vulnerability_exploitability AS (
    SELECT
        'vulnerability.exploitability' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json
    FROM exploitability_active
    WHERE ($9 = '' OR payload->>'cve_id' = $9)
),
package_consumption_correlation AS (
    SELECT
        'package.consumption' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json
    FROM package_consumption_correlation_active
    WHERE ($11 = '' OR payload->>'repository_id' = $11)
      AND ($10 = '' OR payload->>'package_id' = $10)
),
package_manifest_dependency AS (
    SELECT
        'package.consumption' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json
    FROM package_manifest_active
    WHERE ($11 = '' OR payload->>'repo_id' = $11)
),
package_registry AS (
    SELECT
        'package.registry' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json
    FROM package_registry_active
    -- package_registry is global metadata; only count it when the caller
    -- asked about a specific package_id so a repo-only scope does not get
    -- a global count that suppresses missing owned-package signals.
    WHERE $10 <> '' AND payload->>'package_id' = $10
),
sbom_component AS (
    SELECT
        'sbom.component' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json
    FROM sbom_component_active
    WHERE $12 <> '' AND payload->>'subject_digest' = $12
),
sbom_attestation AS (
    SELECT
        'sbom.attestation' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json
    FROM sbom_attestation_active
    WHERE $12 <> '' AND payload->>'subject_digest' = $12
),
container_image_identity AS (
    SELECT
        'container_image.identity' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json
    FROM container_image_identity_active
    WHERE $12 <> '' AND payload->>'digest' = $12
),
vulnerability_source_snapshot AS (
    SELECT
        'vulnerability.source_snapshot' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        BOOL_OR(payload @> '{"complete": false}'::jsonb) AS target_incomplete,
        ARRAY_REMOVE(
            ARRAY_AGG(DISTINCT NULLIF(TRIM(payload->>'warning_message'), ''))
                FILTER (WHERE payload @> '{"complete": false}'::jsonb),
            NULL
        ) AS incomplete_reasons,
        COALESCE(
            JSONB_AGG(DISTINCT JSONB_STRIP_NULLS(JSONB_BUILD_OBJECT(
                'source', payload->>'source',
                'ecosystem', payload->>'ecosystem',
                'cache_artifact_version', payload->>'cache_artifact_version',
                'snapshot_digest', payload->>'cache_snapshot_digest',
                'last_updated_at', payload->>'cache_updated_at',
                'freshness', payload->>'cache_freshness',
                'complete', payload @> '{"complete": true}'::jsonb,
                'warning_code', payload->>'warning_code',
                'warning_message', payload->>'warning_message'
            ))) FILTER (WHERE payload IS NOT NULL),
            '[]'::jsonb
        )::text AS source_snapshots_json
    FROM vulnerability_source_snapshot_active
)
SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json FROM vulnerability_advisory
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json FROM vulnerability_exploitability
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json FROM package_consumption_correlation
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json FROM package_manifest_dependency
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json FROM package_registry
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json FROM sbom_component
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json FROM sbom_attestation
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json FROM container_image_identity
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json FROM vulnerability_source_snapshot
`
