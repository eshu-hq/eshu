package query

import (
	"context"
	"database/sql"
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
func (s PostgresSupplyChainImpactReadinessStore) ReadSupplyChainImpactReadiness(
	ctx context.Context,
	query SupplyChainImpactReadinessQuery,
) (SupplyChainImpactReadinessSnapshot, error) {
	if s.DB == nil {
		return SupplyChainImpactReadinessSnapshot{}, fmt.Errorf("supply chain impact readiness database is required")
	}
	window := s.FreshnessWindow
	if window <= 0 {
		window = supplyChainImpactReadinessFreshnessWindow
	}
	freshnessCutoff := time.Now().Add(-window)

	rows, err := s.DB.QueryContext(
		ctx,
		listSupplyChainImpactReadinessQuery,
		pq.Array(vulnerabilityAdvisoryFactKinds),
		pq.Array(vulnerabilityExploitabilityFactKinds),
		pq.Array(packageConsumptionFactKinds),
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

	sources := make([]SupplyChainImpactEvidenceFamily, 0, 7)
	var targetIncomplete bool
	var incompleteReason string

	for rows.Next() {
		var family string
		var factCount int
		var latest sql.NullTime
		var incompleteFlag sql.NullBool
		var reason sql.NullString
		if err := rows.Scan(&family, &factCount, &latest, &incompleteFlag, &reason); err != nil {
			return SupplyChainImpactReadinessSnapshot{}, fmt.Errorf("scan supply chain impact readiness row: %w", err)
		}
		if family == "vulnerability.source_snapshot" {
			if incompleteFlag.Valid && incompleteFlag.Bool {
				targetIncomplete = true
				if reason.Valid {
					incompleteReason = reason.String
				}
			}
			continue
		}
		entry := SupplyChainImpactEvidenceFamily{
			Family:    family,
			FactCount: factCount,
		}
		if latest.Valid {
			entry.LatestObservedAt = latest.Time.UTC().Format(time.RFC3339)
			if latest.Time.After(freshnessCutoff) {
				entry.Freshness = FreshnessLabelFresh
			} else {
				entry.Freshness = FreshnessLabelStale
			}
		} else {
			entry.Freshness = FreshnessLabelUnknown
		}
		sources = append(sources, entry)
	}
	if err := rows.Err(); err != nil {
		return SupplyChainImpactReadinessSnapshot{}, fmt.Errorf("read supply chain impact readiness: %w", err)
	}

	return SupplyChainImpactReadinessSnapshot{
		EvidenceSources:  sources,
		TargetIncomplete: targetIncomplete,
		IncompleteReason: incompleteReason,
	}, nil
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
	packageConsumptionFactKinds = []string{
		"package_manifest_dependency",
		"reducer_package_consumption_correlation",
	}
	packageRegistryFactKinds = []string{
		"package_registry.package",
		"package_registry.package_version",
	}
	sbomComponentFactKinds            = []string{"sbom.component"}
	sbomAttestationFactKinds          = []string{"reducer_sbom_attestation_attachment"}
	containerImageIdentityFactKinds   = []string{"reducer_container_image_identity"}
	vulnerabilitySourceSnapshotFactKinds = []string{"vulnerability.source_snapshot"}
)

const listSupplyChainImpactReadinessQuery = `
WITH active_facts AS (
    SELECT
        fact.fact_kind,
        fact.payload,
        fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
vulnerability_advisory AS (
    SELECT
        'vulnerability.advisory' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        FALSE AS target_incomplete,
        ''::text AS incomplete_reason
    FROM active_facts
    WHERE fact_kind = ANY($1::text[])
      AND ($9 = '' OR payload->>'cve_id' = $9)
),
vulnerability_exploitability AS (
    SELECT
        'vulnerability.exploitability' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        FALSE AS target_incomplete,
        ''::text AS incomplete_reason
    FROM active_facts
    WHERE fact_kind = ANY($2::text[])
      AND ($9 = '' OR payload->>'cve_id' = $9)
),
package_consumption AS (
    SELECT
        'package.consumption' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        FALSE AS target_incomplete,
        ''::text AS incomplete_reason
    FROM active_facts
    WHERE fact_kind = ANY($3::text[])
      AND ($11 = '' OR payload->>'repository_id' = $11)
      AND ($10 = '' OR payload->>'package_id' = $10)
),
package_registry AS (
    SELECT
        'package.registry' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        FALSE AS target_incomplete,
        ''::text AS incomplete_reason
    FROM active_facts
    WHERE fact_kind = ANY($4::text[])
      AND ($10 = '' OR payload->>'package_id' = $10)
),
sbom_component AS (
    SELECT
        'sbom.component' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        FALSE AS target_incomplete,
        ''::text AS incomplete_reason
    FROM active_facts
    WHERE fact_kind = ANY($5::text[])
      AND ($12 = '' OR payload->>'subject_digest' = $12)
),
sbom_attestation AS (
    SELECT
        'sbom.attestation' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        FALSE AS target_incomplete,
        ''::text AS incomplete_reason
    FROM active_facts
    WHERE fact_kind = ANY($6::text[])
      AND ($12 = '' OR payload->>'subject_digest' = $12)
),
container_image_identity AS (
    SELECT
        'container_image.identity' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        FALSE AS target_incomplete,
        ''::text AS incomplete_reason
    FROM active_facts
    WHERE fact_kind = ANY($7::text[])
      AND ($12 = '' OR payload->>'digest' = $12)
),
vulnerability_source_snapshot AS (
    SELECT
        'vulnerability.source_snapshot' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        BOOL_OR(
            LOWER(COALESCE(payload->>'completion_state', '')) IN ('in_progress', 'partial', 'incomplete')
            OR COALESCE((payload->>'is_complete')::boolean, TRUE) = FALSE
        ) AS target_incomplete,
        COALESCE(
            MAX(payload->>'incomplete_reason') FILTER (WHERE LOWER(COALESCE(payload->>'completion_state', '')) IN ('in_progress', 'partial', 'incomplete')),
            ''
        ) AS incomplete_reason
    FROM active_facts
    WHERE fact_kind = ANY($8::text[])
)
SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reason FROM vulnerability_advisory
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reason FROM vulnerability_exploitability
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reason FROM package_consumption
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reason FROM package_registry
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reason FROM sbom_component
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reason FROM sbom_attestation
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reason FROM container_image_identity
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reason FROM vulnerability_source_snapshot
`
