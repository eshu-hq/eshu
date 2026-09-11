// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// AdvisoryEvidenceQueryer is the Postgres connection contract the advisory
// catalog and evidence read models share. Exported so the staying root
// evidence/catalog tests and the root compatibility forwarders can name the
// constructor parameter.
type AdvisoryEvidenceQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// AdvisoryEvidenceFactRow is one scanned source-fact row before grouping.
// Exported for the staying root evidence tests, which build fact rows
// directly.
type AdvisoryEvidenceFactRow struct {
	FactID           string
	FactKind         string
	SourceConfidence string
	ObservedAt       string
	// SchemaVersion is the fact row's persisted schema_version, threaded into
	// the typed factschema decode seam so a non-1.x (future/unsupported major)
	// source fact dead-letters instead of being decoded as v1. An empty value
	// normalizes to queryDefaultSchemaMajorVersion in supplyChainSchemaEnvelope,
	// matching the version-less legacy default.
	SchemaVersion string
	Payload       map[string]any
}

// PostgresAdvisoryEvidenceStore reads active vulnerability source facts and
// groups them into canonical advisory evidence rows.
type PostgresAdvisoryEvidenceStore struct {
	DB AdvisoryEvidenceQueryer
}

// NewPostgresAdvisoryEvidenceStore creates the Postgres-backed advisory
// evidence read model.
func NewPostgresAdvisoryEvidenceStore(db AdvisoryEvidenceQueryer) PostgresAdvisoryEvidenceStore {
	return PostgresAdvisoryEvidenceStore{DB: db}
}

// ListAdvisoryEvidence returns one bounded page of source-only advisory
// evidence.
func (s PostgresAdvisoryEvidenceStore) ListAdvisoryEvidence(
	ctx context.Context,
	filter AdvisoryEvidenceFilter,
) ([]AdvisoryEvidenceRow, error) {
	filter = NormalizeAdvisoryEvidenceFilter(filter)
	if s.DB == nil {
		return nil, fmt.Errorf("advisory evidence database is required")
	}
	if !filter.HasScope() {
		return nil, fmt.Errorf("cve_id, advisory_id, package_id, repository_id, service_id, or workload_id is required")
	}
	if filter.Limit <= 0 || filter.Limit > AdvisoryEvidenceMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", AdvisoryEvidenceMaxLimit+1)
	}
	rows, err := s.DB.QueryContext(
		ctx,
		ListAdvisoryEvidenceQuery,
		pgarray.Array(advisoryEvidenceFactKinds),
		pgarray.Array(AdvisoryEvidenceLookupIDs(filter)),
		pgarray.Array(advisoryEvidencePackageIDs(filter)),
		filter.Source,
		AdvisoryEvidenceMaxFactRows,
		filter.RepositoryID,
		filter.ServiceID,
		filter.WorkloadID,
		pgarray.Array(filter.AllowedSourceRepositoryIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list advisory evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	facts := make([]AdvisoryEvidenceFactRow, 0, AdvisoryEvidenceFactCapacity())
	for rows.Next() {
		var factID string
		var factKind string
		var sourceConfidence string
		var observedAt sql.NullTime
		var schemaVersion string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &factKind, &sourceConfidence, &observedAt, &schemaVersion, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list advisory evidence: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil, fmt.Errorf("decode advisory evidence payload: %w", err)
		}
		facts = append(facts, AdvisoryEvidenceFactRow{
			FactID:           factID,
			FactKind:         factKind,
			SourceConfidence: sourceConfidence,
			ObservedAt:       FormatNullTime(observedAt),
			SchemaVersion:    schemaVersion,
			Payload:          payload,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list advisory evidence: %w", err)
	}
	return PageAdvisoryEvidenceRows(BuildAdvisoryEvidenceRows(facts), filter), nil
}

// HasScope reports whether the filter carries any anchor the read model can
// scope to. Exported for the staying root evidence handler, which rejects
// anchorless reads before touching the store.
func (f AdvisoryEvidenceFilter) HasScope() bool {
	return f.CVEID != "" || f.AdvisoryID != "" || f.PackageID != "" ||
		f.RepositoryID != "" || f.ServiceID != "" || f.WorkloadID != ""
}

func (f AdvisoryEvidenceFilter) hasImpactScope() bool {
	return f.RepositoryID != "" || f.ServiceID != "" || f.WorkloadID != ""
}

// NormalizeAdvisoryEvidenceFilter trims and canonicalizes evidence filter
// inputs. Exported for the staying root evidence and vulnerability-detail
// handlers and the root evidence tests.
func NormalizeAdvisoryEvidenceFilter(filter AdvisoryEvidenceFilter) AdvisoryEvidenceFilter {
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

// AdvisoryEvidenceLookupIDs returns the normalized advisory lookup ids for
// one filter. Exported for the root evidence SQL tests.
func AdvisoryEvidenceLookupIDs(filter AdvisoryEvidenceFilter) []string {
	filter = NormalizeAdvisoryEvidenceFilter(filter)
	seen := map[string]struct{}{}
	for _, value := range []string{filter.CVEID, filter.AdvisoryID} {
		addSet(seen, value)
	}
	return SetToSortedSlice(seen)
}

func advisoryEvidencePackageIDs(filter AdvisoryEvidenceFilter) []string {
	filter = NormalizeAdvisoryEvidenceFilter(filter)
	seen := map[string]struct{}{}
	addSet(seen, filter.PackageID)
	return SetToSortedSlice(seen)
}

// AdvisoryEvidenceFactCapacity bounds the scanned fact rows behind one
// evidence page. Exported for the root evidence tests.
func AdvisoryEvidenceFactCapacity() int {
	return AdvisoryEvidenceMaxFactRows
}

// FormatNullTime renders a nullable Postgres timestamp as RFC3339, or "".
// Exported for the staying root work-item evidence store, which shares the
// rendering.
func FormatNullTime(value sql.NullTime) string {
	if !value.Valid {
		return ""
	}
	return value.Time.UTC().Format(time.RFC3339)
}

// PageAdvisoryEvidenceRows filters and keyset-pages grouped evidence rows.
// Exported for the staying root evidence tests, which pin paging semantics.
func PageAdvisoryEvidenceRows(rows []AdvisoryEvidenceRow, filter AdvisoryEvidenceFilter) []AdvisoryEvidenceRow {
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
	filter = NormalizeAdvisoryEvidenceFilter(filter)
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
