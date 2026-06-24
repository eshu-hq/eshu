// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// PostgresAdvisoryCatalogStore reads a bounded, browsable page of canonical
// vulnerability advisories from active vulnerability source facts. It reuses the
// advisory evidence queryer seam so the catalog and detail read models share one
// Postgres connection contract.
type PostgresAdvisoryCatalogStore struct {
	DB advisoryEvidenceQueryer
}

// NewPostgresAdvisoryCatalogStore creates the Postgres-backed catalog read
// model.
func NewPostgresAdvisoryCatalogStore(db advisoryEvidenceQueryer) PostgresAdvisoryCatalogStore {
	return PostgresAdvisoryCatalogStore{DB: db}
}

// ListAdvisoryCatalog returns one bounded page of catalog rows ordered by
// descending CVSS then ascending advisory key. The query is read-only, bounded
// by the page limit, and cancellable through the request context.
func (s PostgresAdvisoryCatalogStore) ListAdvisoryCatalog(
	ctx context.Context,
	filter AdvisoryCatalogFilter,
) (AdvisoryCatalogPage, error) {
	filter = normalizeAdvisoryCatalogFilter(filter)
	if s.DB == nil {
		return AdvisoryCatalogPage{}, fmt.Errorf("advisory catalog database is required")
	}
	if filter.Limit <= 0 || filter.Limit > advisoryCatalogMaxLimit+1 {
		return AdvisoryCatalogPage{}, fmt.Errorf("limit must be between 1 and %d for internal pagination", advisoryCatalogMaxLimit+1)
	}
	rows, err := s.DB.QueryContext(
		ctx,
		listAdvisoryCatalogQuery,
		filter.Severity,
		filter.Ecosystem,
		filter.Query,
		filter.KEVOnly,
		filter.AfterCVSS,
		filter.AfterAdvisoryKey,
		filter.Limit,
	)
	if err != nil {
		return AdvisoryCatalogPage{}, fmt.Errorf("list advisory catalog: %w", err)
	}
	defer func() { _ = rows.Close() }()

	page := AdvisoryCatalogPage{Rows: make([]AdvisoryCatalogRow, 0, filter.Limit)}
	for rows.Next() {
		var (
			advisoryKey   string
			cvssScore     float64
			severityLabel sql.NullString
			cveID         sql.NullString
			ghsaID        sql.NullString
			publishedAt   sql.NullString
			sources       pq.StringArray
			ecosystems    pq.StringArray
			packageIDs    pq.StringArray
			kev           bool
		)
		if err := rows.Scan(
			&advisoryKey,
			&cvssScore,
			&severityLabel,
			&cveID,
			&ghsaID,
			&publishedAt,
			&sources,
			&ecosystems,
			&packageIDs,
			&kev,
		); err != nil {
			return AdvisoryCatalogPage{}, fmt.Errorf("list advisory catalog: %w", err)
		}
		page.Rows = append(page.Rows, AdvisoryCatalogRow{
			AdvisoryKey:   advisoryKey,
			CanonicalID:   advisoryKey,
			CVEID:         strings.TrimSpace(cveID.String),
			GHSAID:        strings.TrimSpace(ghsaID.String),
			SeverityLabel: strings.TrimSpace(severityLabel.String),
			CVSSScore:     cvssScore,
			KEV:           kev,
			Ecosystems:    trimmedStrings(ecosystems),
			PackageIDs:    trimmedStrings(packageIDs),
			PublishedAt:   strings.TrimSpace(publishedAt.String),
			Sources:       trimmedStrings(sources),
		})
	}
	if err := rows.Err(); err != nil {
		return AdvisoryCatalogPage{}, fmt.Errorf("list advisory catalog: %w", err)
	}
	return page, nil
}

// normalizeAdvisoryCatalogFilter trims and canonicalizes catalog filter inputs.
// Severity, ecosystem, and query stay as supplied beyond trimming so the SQL
// owns case folding; the advisory key cursor is upper-cased to match the
// canonical key projection.
func normalizeAdvisoryCatalogFilter(filter AdvisoryCatalogFilter) AdvisoryCatalogFilter {
	filter.Severity = strings.TrimSpace(filter.Severity)
	filter.Ecosystem = strings.TrimSpace(filter.Ecosystem)
	filter.Query = strings.TrimSpace(filter.Query)
	filter.AfterAdvisoryKey = strings.ToUpper(strings.TrimSpace(filter.AfterAdvisoryKey))
	return filter
}

// trimmedStrings copies a Postgres text array into a trimmed Go slice, dropping
// blank entries. It returns nil for an empty result so JSON omitempty fields
// stay absent.
func trimmedStrings(values pq.StringArray) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
