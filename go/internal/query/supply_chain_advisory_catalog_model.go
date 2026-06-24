// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

const (
	// advisoryCatalogCapability gates the browsable CVE-intelligence catalog
	// list over the Postgres vulnerability source fact read model.
	advisoryCatalogCapability = "supply_chain.advisory_catalog.list"
	// advisoryCatalogMaxLimit bounds one catalog page so an unscoped browse of
	// the whole intelligence catalog stays cheap and cancellable.
	advisoryCatalogMaxLimit = 200
)

// AdvisoryCatalogStore reads a browsable, summary-only page of canonical
// vulnerability advisories from active vulnerability source facts. Unlike
// AdvisoryEvidenceStore, it does not require an advisory, package, repository,
// service, or workload anchor: it lists the known CVE-intelligence catalog so
// the console can browse advisories that are not yet reachable in any service.
type AdvisoryCatalogStore interface {
	ListAdvisoryCatalog(context.Context, AdvisoryCatalogFilter) (AdvisoryCatalogPage, error)
}

// AdvisoryCatalogFilter bounds a catalog browse. All filters are optional; an
// empty filter lists the full catalog ordered by descending CVSS then advisory
// key. Limit is the page size plus one used to detect truncation.
type AdvisoryCatalogFilter struct {
	// Severity matches the canonical severity label (case-insensitive), e.g.
	// CRITICAL, HIGH, MEDIUM, LOW.
	Severity string
	// Ecosystem matches an affected-package ecosystem (case-insensitive), e.g.
	// npm, pypi, maven.
	Ecosystem string
	// Query prefix-matches a canonical advisory id, CVE id, GHSA id, or
	// affected package id / PURL.
	Query string
	// KEVOnly limits the page to advisories present in the CISA KEV catalog.
	KEVOnly bool
	// AfterCVSS and AfterAdvisoryKey form the keyset cursor for the
	// (cvss desc, advisory_key asc) ordering. AfterAdvisoryKey is empty for the
	// first page.
	AfterCVSS        float64
	AfterAdvisoryKey string
	// Limit is the page size plus one so the store can report truncation.
	Limit int
}

// AdvisoryCatalogPage is one bounded page of catalog rows.
type AdvisoryCatalogPage struct {
	Rows []AdvisoryCatalogRow
}

// AdvisoryCatalogRow is one canonical advisory summarized for catalog browsing.
// It is source intelligence only: it does not imply repository, image,
// workload, or deployment impact. Drill into the existing advisory detail
// surface for full source evidence.
type AdvisoryCatalogRow struct {
	AdvisoryKey   string   `json:"advisory_key"`
	CanonicalID   string   `json:"canonical_id"`
	CVEID         string   `json:"cve_id,omitempty"`
	GHSAID        string   `json:"ghsa_id,omitempty"`
	SeverityLabel string   `json:"severity_label,omitempty"`
	CVSSScore     float64  `json:"cvss_score,omitempty"`
	KEV           bool     `json:"kev"`
	Ecosystems    []string `json:"ecosystems,omitempty"`
	PackageIDs    []string `json:"package_ids,omitempty"`
	PublishedAt   string   `json:"published_at,omitempty"`
	Sources       []string `json:"sources,omitempty"`
}
