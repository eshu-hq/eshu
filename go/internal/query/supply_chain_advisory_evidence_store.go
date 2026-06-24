// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

type advisoryEvidenceQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type advisoryEvidenceFactRow struct {
	FactID           string
	FactKind         string
	SourceConfidence string
	ObservedAt       string
	Payload          map[string]any
}

// PostgresAdvisoryEvidenceStore reads active vulnerability source facts and
// groups them into canonical advisory evidence rows.
type PostgresAdvisoryEvidenceStore struct {
	DB advisoryEvidenceQueryer
}

// NewPostgresAdvisoryEvidenceStore creates the Postgres-backed advisory
// evidence read model.
func NewPostgresAdvisoryEvidenceStore(db advisoryEvidenceQueryer) PostgresAdvisoryEvidenceStore {
	return PostgresAdvisoryEvidenceStore{DB: db}
}

// ListAdvisoryEvidence returns one bounded page of source-only advisory
// evidence.
func (s PostgresAdvisoryEvidenceStore) ListAdvisoryEvidence(
	ctx context.Context,
	filter AdvisoryEvidenceFilter,
) ([]AdvisoryEvidenceRow, error) {
	filter = normalizeAdvisoryEvidenceFilter(filter)
	if s.DB == nil {
		return nil, fmt.Errorf("advisory evidence database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("cve_id, advisory_id, package_id, repository_id, service_id, or workload_id is required")
	}
	if filter.Limit <= 0 || filter.Limit > advisoryEvidenceMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", advisoryEvidenceMaxLimit+1)
	}
	rows, err := s.DB.QueryContext(
		ctx,
		listAdvisoryEvidenceQuery,
		pq.Array(advisoryEvidenceFactKinds),
		pq.Array(advisoryEvidenceLookupIDs(filter)),
		pq.Array(advisoryEvidencePackageIDs(filter)),
		filter.Source,
		advisoryEvidenceMaxFactRows,
		filter.RepositoryID,
		filter.ServiceID,
		filter.WorkloadID,
		pq.Array(filter.AllowedSourceRepositoryIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list advisory evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	facts := make([]advisoryEvidenceFactRow, 0, advisoryEvidenceFactCapacity())
	for rows.Next() {
		var factID string
		var factKind string
		var sourceConfidence string
		var observedAt sql.NullTime
		var payloadBytes []byte
		if err := rows.Scan(&factID, &factKind, &sourceConfidence, &observedAt, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list advisory evidence: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil, fmt.Errorf("decode advisory evidence payload: %w", err)
		}
		facts = append(facts, advisoryEvidenceFactRow{
			FactID:           factID,
			FactKind:         factKind,
			SourceConfidence: sourceConfidence,
			ObservedAt:       formatNullTime(observedAt),
			Payload:          payload,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list advisory evidence: %w", err)
	}
	return pageAdvisoryEvidenceRows(buildAdvisoryEvidenceRows(facts), filter), nil
}

func (f AdvisoryEvidenceFilter) hasScope() bool {
	return f.CVEID != "" || f.AdvisoryID != "" || f.PackageID != "" ||
		f.RepositoryID != "" || f.ServiceID != "" || f.WorkloadID != ""
}

func (f AdvisoryEvidenceFilter) hasImpactScope() bool {
	return f.RepositoryID != "" || f.ServiceID != "" || f.WorkloadID != ""
}

func normalizeAdvisoryEvidenceFilter(filter AdvisoryEvidenceFilter) AdvisoryEvidenceFilter {
	filter.CVEID = normalizeAdvisoryLookupID(filter.CVEID)
	filter.AdvisoryID = normalizeAdvisoryLookupID(filter.AdvisoryID)
	filter.PackageID = strings.TrimSpace(filter.PackageID)
	filter.RepositoryID = strings.TrimSpace(filter.RepositoryID)
	filter.ServiceID = strings.TrimSpace(filter.ServiceID)
	filter.WorkloadID = strings.TrimSpace(filter.WorkloadID)
	filter.Source = strings.ToLower(strings.TrimSpace(filter.Source))
	filter.AfterAdvisoryKey = normalizeAdvisoryLookupID(filter.AfterAdvisoryKey)
	return filter
}

func normalizeAdvisoryLookupID(value string) string {
	return normalizeAdvisoryDisplayID(strings.TrimSpace(value))
}

func advisoryEvidenceLookupIDs(filter AdvisoryEvidenceFilter) []string {
	filter = normalizeAdvisoryEvidenceFilter(filter)
	seen := map[string]struct{}{}
	for _, value := range []string{filter.CVEID, filter.AdvisoryID} {
		addSet(seen, value)
	}
	return setToSortedSlice(seen)
}

func advisoryEvidencePackageIDs(filter AdvisoryEvidenceFilter) []string {
	filter = normalizeAdvisoryEvidenceFilter(filter)
	seen := map[string]struct{}{}
	addSet(seen, filter.PackageID)
	return setToSortedSlice(seen)
}

func advisoryEvidenceFactCapacity() int {
	return advisoryEvidenceMaxFactRows
}

func formatNullTime(value sql.NullTime) string {
	if !value.Valid {
		return ""
	}
	return value.Time.UTC().Format(time.RFC3339)
}

func pageAdvisoryEvidenceRows(rows []AdvisoryEvidenceRow, filter AdvisoryEvidenceFilter) []AdvisoryEvidenceRow {
	rows = filterAdvisoryEvidenceRows(rows, filter)
	start := 0
	if after := normalizeAdvisoryLookupID(filter.AfterAdvisoryKey); after != "" {
		for idx, row := range rows {
			if advisoryEvidenceKeyEqual(row.AdvisoryKey, after) {
				start = idx + 1
				break
			}
		}
	}
	if start >= len(rows) {
		return nil
	}
	end := start + filter.Limit
	if end > len(rows) {
		end = len(rows)
	}
	return append([]AdvisoryEvidenceRow(nil), rows[start:end]...)
}

func filterAdvisoryEvidenceRows(rows []AdvisoryEvidenceRow, filter AdvisoryEvidenceFilter) []AdvisoryEvidenceRow {
	if filter.CVEID == "" && filter.AdvisoryID == "" && filter.PackageID == "" {
		return rows
	}
	filter = normalizeAdvisoryEvidenceFilter(filter)
	if filter.hasImpactScope() {
		filter.CVEID = ""
		filter.AdvisoryID = ""
		filter.PackageID = ""
	}
	if filter.CVEID == "" && filter.AdvisoryID == "" && filter.PackageID == "" {
		return rows
	}
	out := make([]AdvisoryEvidenceRow, 0, len(rows))
	for _, row := range rows {
		if advisoryEvidenceRowMatchesFilter(row, filter) {
			out = append(out, row)
		}
	}
	return out
}

func advisoryEvidenceRowMatchesFilter(row AdvisoryEvidenceRow, filter AdvisoryEvidenceFilter) bool {
	if filter.CVEID != "" && !advisoryEvidenceRowMatchesCVE(row, filter.CVEID) {
		return false
	}
	if filter.AdvisoryID != "" && !advisoryEvidenceRowMatchesAdvisory(row, filter.AdvisoryID) {
		return false
	}
	if filter.PackageID != "" && !advisoryEvidenceRowMatchesPackage(row, filter.PackageID) {
		return false
	}
	return true
}

func advisoryEvidenceRowMatchesCVE(row AdvisoryEvidenceRow, cveID string) bool {
	target := normalizeCVEID(cveID)
	if strings.EqualFold(normalizeCVEID(row.CanonicalID), target) ||
		strings.EqualFold(normalizeCVEID(row.AdvisoryKey), target) {
		return true
	}
	for _, value := range row.CVEIDs {
		if strings.EqualFold(normalizeCVEID(value), target) {
			return true
		}
	}
	return false
}

func advisoryEvidenceRowMatchesAdvisory(row AdvisoryEvidenceRow, advisoryID string) bool {
	target := normalizeAdvisoryLookupID(advisoryID)
	if advisoryEvidenceKeyEqual(row.AdvisoryKey, target) ||
		advisoryEvidenceKeyEqual(row.CanonicalID, target) ||
		advisoryEvidenceStringSliceMatches(row.CVEIDs, target) ||
		advisoryEvidenceStringSliceMatches(row.GHSAIDs, target) ||
		advisoryEvidenceStringSliceMatches(row.OSVIDs, target) ||
		advisoryEvidenceStringSliceMatches(row.SourceIDs, target) {
		return true
	}
	for _, source := range row.Sources {
		if advisoryEvidenceAnyIDMatches(target, source.AdvisoryID, source.CVEID, source.GHSAID) ||
			advisoryEvidenceStringSliceMatches(source.Aliases, target) {
			return true
		}
	}
	for _, pkg := range row.AffectedPackages {
		if advisoryEvidenceAnyIDMatches(target, pkg.AdvisoryID, pkg.CVEID, pkg.GHSAID) {
			return true
		}
	}
	for _, product := range row.AffectedProducts {
		if advisoryEvidenceAnyIDMatches(target, product.CVEID) {
			return true
		}
	}
	for _, epss := range row.EPSS {
		if advisoryEvidenceAnyIDMatches(target, epss.CVEID) {
			return true
		}
	}
	for _, kev := range row.KEV {
		if advisoryEvidenceAnyIDMatches(target, kev.CVEID) {
			return true
		}
	}
	for _, ref := range row.References {
		if advisoryEvidenceAnyIDMatches(target, ref.AdvisoryID, ref.CVEID) {
			return true
		}
	}
	return false
}

func advisoryEvidenceRowMatchesPackage(row AdvisoryEvidenceRow, packageID string) bool {
	target := strings.TrimSpace(packageID)
	for _, pkg := range row.AffectedPackages {
		if strings.TrimSpace(pkg.PackageID) == target || strings.TrimSpace(pkg.PURL) == target {
			return true
		}
	}
	return false
}

func advisoryEvidenceAnyIDMatches(target string, values ...string) bool {
	for _, value := range values {
		if advisoryEvidenceKeyEqual(value, target) {
			return true
		}
	}
	return false
}

func advisoryEvidenceStringSliceMatches(values []string, target string) bool {
	for _, value := range values {
		if advisoryEvidenceKeyEqual(value, target) {
			return true
		}
	}
	return false
}

func advisoryEvidenceKeyEqual(left string, right string) bool {
	return strings.EqualFold(normalizeAdvisoryLookupID(left), normalizeAdvisoryLookupID(right))
}
