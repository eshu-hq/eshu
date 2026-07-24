// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
//
// The store requires a fact-anchored scope (CVE, package, repository, subject
// digest, or image reference). Advisory narrows source-advisory rows only when
// one of those anchors is present. Derived scanner filters such as workload,
// service, environment, ecosystem, severity, or impact_status are echoed in the
// target scope but do not open a source-fact scan by themselves, because source
// facts do not carry those reducer-owned attributes.
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
		query.AdvisoryID,
		query.ImageRef,
		pq.Array(vulnerabilityOSPackageFactKinds),
		pq.Array(scannerWorkerAnalysisFactKinds),
	)
	if err != nil {
		return SupplyChainImpactReadinessSnapshot{}, fmt.Errorf("read supply chain impact readiness: %w", err)
	}
	defer func() { _ = rows.Close() }()

	families := make(map[string]SupplyChainImpactEvidenceFamily, len(supplyChainImpactReadinessFamilies))
	var targetIncomplete bool
	var incompleteReasons []string
	var sourceSnapshots []SupplyChainImpactSourceSnapshot
	var sourceStates []SupplyChainImpactSourceState
	var unsupportedTargets []SupplyChainImpactUnsupportedTarget

	for rows.Next() {
		var family string
		var factCount int
		var latest sql.NullTime
		var incompleteFlag sql.NullBool
		var reasons pq.StringArray
		var sourceSnapshotsJSON sql.NullString
		var sourceStatesJSON sql.NullString
		var unsupportedTargetsJSON sql.NullString
		if err := rows.Scan(&family, &factCount, &latest, &incompleteFlag, &reasons, &sourceSnapshotsJSON, &sourceStatesJSON, &unsupportedTargetsJSON); err != nil {
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
		if family == sourceStateFamilyMarker {
			if incompleteFlag.Valid && incompleteFlag.Bool {
				targetIncomplete = true
				incompleteReasons = append(incompleteReasons, reasons...)
			}
			decodedStates, err := decodeSourceStates(sourceStatesJSON)
			if err != nil {
				return SupplyChainImpactReadinessSnapshot{}, err
			}
			sourceStates = append(sourceStates, decodedStates...)
			continue
		}
		if family == unsupportedTargetFamilyMarker {
			decodedTargets, err := decodeUnsupportedTargets(unsupportedTargetsJSON)
			if err != nil {
				return SupplyChainImpactReadinessSnapshot{}, err
			}
			unsupportedTargets = append(unsupportedTargets, decodedTargets...)
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
		EvidenceSources:    sources,
		SourceSnapshots:    sourceSnapshots,
		SourceStates:       sourceStates,
		UnsupportedTargets: unsupportedTargets,
		TargetIncomplete:   targetIncomplete,
		IncompleteReasons:  uniqueSortedReadinessStrings(incompleteReasons),
	}, nil
}
