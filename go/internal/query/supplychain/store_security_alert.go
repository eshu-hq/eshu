// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
)

// SecurityAlertReconciliationStore reads reducer-owned provider alert
// reconciliation rows.
type SecurityAlertReconciliationStore interface {
	ListSecurityAlertReconciliations(
		context.Context,
		SecurityAlertReconciliationFilter,
	) ([]SecurityAlertReconciliationRow, error)
}

// SecurityAlertReconciliationFilter bounds provider alert reconciliation reads
// to a repository, provider, package, or advisory id anchor. Provider state and
// reconciliation status narrow anchored pages but are not standalone scopes.
type SecurityAlertReconciliationFilter struct {
	RepositoryID          string
	RepositoryScopeIDs    []string
	Provider              string
	PackageID             string
	CVEID                 string
	GHSAID                string
	ProviderState         string
	ReconciliationStatus  string
	AfterReconciliationID string
	Limit                 int
	// AllowedSourceRepositoryIDs carries the scoped-token grant set (union of
	// granted repository and ingestion-scope ids). When populated, reconciliation
	// facts are intersected with the grant set (matching repository_id,
	// provider_repository_id, or scope_id) before deduplication, ordering,
	// limits, cursors, and count metadata. Empty means unrestricted
	// (shared-token, all-scope admin, or local dev).
	AllowedSourceRepositoryIDs []string
}

// ProviderSecurityAlertRow is the provider-reported alert state preserved by
// the reconciliation read model.
type ProviderSecurityAlertRow struct {
	Provider                    string              `json:"provider,omitempty"`
	ProviderAlertID             string              `json:"provider_alert_id,omitempty"`
	ProviderAlertNumber         int64               `json:"provider_alert_number,omitempty"`
	ProviderState               string              `json:"provider_state,omitempty"`
	RepositoryID                string              `json:"repository_id,omitempty"`
	PackageID                   string              `json:"package_id,omitempty"`
	Ecosystem                   string              `json:"ecosystem,omitempty"`
	PackageName                 string              `json:"package_name,omitempty"`
	ManifestPath                string              `json:"manifest_path,omitempty"`
	DependencyScope             string              `json:"dependency_scope,omitempty"`
	Relationship                string              `json:"relationship,omitempty"`
	GHSAIDs                     []string            `json:"ghsa_ids,omitempty"`
	CVEIDs                      []string            `json:"cve_ids,omitempty"`
	VulnerableRange             string              `json:"vulnerable_range,omitempty"`
	PatchedVersion              string              `json:"patched_version,omitempty"`
	Severity                    string              `json:"severity,omitempty"`
	CVSS                        map[string]any      `json:"cvss,omitempty"`
	EPSS                        map[string]string   `json:"epss,omitempty"`
	CWEs                        []map[string]string `json:"cwes,omitempty"`
	Summary                     string              `json:"summary,omitempty"`
	SourceURL                   string              `json:"source_url,omitempty"`
	CreatedAt                   string              `json:"created_at,omitempty"`
	UpdatedAt                   string              `json:"updated_at,omitempty"`
	FixedAt                     string              `json:"fixed_at,omitempty"`
	DismissedAt                 string              `json:"dismissed_at,omitempty"`
	CollectionCoverageState     string              `json:"collection_coverage_state,omitempty"`
	CollectionTruncated         bool                `json:"collection_truncated,omitempty"`
	CollectionPagesFetched      int64               `json:"collection_pages_fetched,omitempty"`
	CollectionStateFilter       string              `json:"collection_state_filter,omitempty"`
	CollectionIncompleteReasons []string            `json:"collection_incomplete_reasons,omitempty"`
}

// SecurityAlertEshuImpactRow carries Eshu-owned impact state matched to a
// provider alert. Empty fields mean no Eshu impact finding was admitted.
type SecurityAlertEshuImpactRow struct {
	ImpactStatus string `json:"impact_status,omitempty"`
	FindingID    string `json:"finding_id,omitempty"`
}

// SecurityAlertEshuPackageRow carries Eshu-owned dependency evidence matched to
// a provider alert. ObservedVersion is never copied from provider alert fields.
type SecurityAlertEshuPackageRow struct {
	ObservedVersion        string   `json:"observed_version,omitempty"`
	RequestedRange         string   `json:"requested_range,omitempty"`
	DependencyRange        string   `json:"dependency_range,omitempty"`
	DependencyEvidenceID   string   `json:"dependency_evidence_id,omitempty"`
	DependencyEvidenceKind string   `json:"dependency_evidence_kind,omitempty"`
	MissingEvidence        []string `json:"missing_evidence,omitempty"`
}

// SecurityAlertReconciliationRow is one reducer-owned comparison row.
type SecurityAlertReconciliationRow struct {
	ReconciliationID     string                         `json:"reconciliation_id"`
	ProviderAlert        ProviderSecurityAlertRow       `json:"provider_alert"`
	EshuPackage          SecurityAlertEshuPackageRow    `json:"eshu_package"`
	EshuImpact           SecurityAlertEshuImpactRow     `json:"eshu_impact"`
	ReconciliationStatus string                         `json:"reconciliation_status"`
	Reason               string                         `json:"reason,omitempty"`
	ReasonCode           string                         `json:"reason_code,omitempty"`
	MissingEvidence      []SecurityAlertMissingEvidence `json:"missing_evidence,omitempty"`
	EvidenceFactIDs      []string                       `json:"evidence_fact_ids,omitempty"`
	SourceFreshness      string                         `json:"source_freshness,omitempty"`
	SourceConfidence     string                         `json:"source_confidence,omitempty"`
}

// SecurityAlertReconciliationResult is the public API row shape.
type SecurityAlertReconciliationResult = SecurityAlertReconciliationRow

func (f SecurityAlertReconciliationFilter) HasScope() bool {
	return f.RepositoryID != "" || len(f.RepositoryScopeIDs) > 0 ||
		f.Provider != "" || f.PackageID != "" ||
		f.CVEID != "" || f.GHSAID != ""
}

// SecurityAlertReconciliationAggregateStore reads cheap-summary aggregates
// over reducer-owned provider security alert reconciliations. It replaces the
// page-and-iterate caller workflow for ecosystem-level questions like "how
// many alerts are in `eshu_only` reconciliation status across all repos?".
type SecurityAlertReconciliationAggregateStore interface {
	CountSecurityAlertReconciliations(context.Context, SecurityAlertReconciliationAggregateFilter) (SecurityAlertReconciliationAggregateCount, error)
	SecurityAlertReconciliationInventory(
		context.Context,
		SecurityAlertReconciliationAggregateFilter,
		SecurityAlertReconciliationInventoryDimension,
		int,
		int,
	) ([]SecurityAlertReconciliationInventoryRow, error)
}

// SecurityAlertReconciliationInventoryDimension names the grouping dimension
// for the inventory aggregate.
type SecurityAlertReconciliationInventoryDimension string

const (
	// SecurityAlertReconciliationInventoryByStatus groups by reducer
	// reconciliation_status.
	SecurityAlertReconciliationInventoryByStatus SecurityAlertReconciliationInventoryDimension = "reconciliation_status"
	// SecurityAlertReconciliationInventoryByProvider groups by provider.
	SecurityAlertReconciliationInventoryByProvider SecurityAlertReconciliationInventoryDimension = "provider"
	// SecurityAlertReconciliationInventoryByProviderState groups by provider
	// state (open, fixed, dismissed, etc.).
	SecurityAlertReconciliationInventoryByProviderState SecurityAlertReconciliationInventoryDimension = "provider_state"
	// SecurityAlertReconciliationInventoryByRepository groups by repository_id.
	SecurityAlertReconciliationInventoryByRepository SecurityAlertReconciliationInventoryDimension = "repository_id"
	// SecurityAlertReconciliationInventoryByPackage groups by package_id.
	SecurityAlertReconciliationInventoryByPackage SecurityAlertReconciliationInventoryDimension = "package_id"
)

// SecurityAlertReconciliationAggregateMaxLimit caps inventory result pages.
const SecurityAlertReconciliationAggregateMaxLimit = 500

// SecurityAlertReconciliationAggregateFilter narrows aggregate reads. An
// aggregate without a scope is allowed because the totals question itself is
// the call shape we want to support — the dataset is already bounded by
// `fact_kind` and the active-generation predicate at index lookup time.
type SecurityAlertReconciliationAggregateFilter struct {
	RepositoryID         string
	RepositoryScopeIDs   []string
	Provider             string
	PackageID            string
	CVEID                string
	GHSAID               string
	ProviderState        string
	ReconciliationStatus string
	// AllowedSourceRepositoryIDs carries the scoped-token grant set (union of
	// granted repository and ingestion-scope ids). When populated, aggregate
	// totals and inventory buckets cover only reconciliation rows whose
	// repository_id, provider_repository_id, or scope_id is in the grant set.
	AllowedSourceRepositoryIDs []string
}

// SecurityAlertReconciliationAggregateCount is the cheap-summary totals
// envelope used by the count handler. ByReconciliationStatus and ByProvider
// are pre-aggregated rollups so callers can answer "alerts per provider" and
// "alerts per reconciliation status" without a second round trip.
type SecurityAlertReconciliationAggregateCount struct {
	TotalReconciliations   int
	ByReconciliationStatus map[string]int
	ByProvider             map[string]int
	ByProviderState        map[string]int
	BySourceFreshness      map[string]int
}

// SecurityAlertReconciliationInventoryRow is one grouped bucket returned by
// the inventory aggregate.
type SecurityAlertReconciliationInventoryRow struct {
	Dimension SecurityAlertReconciliationInventoryDimension `json:"dimension"`
	Value     string                                        `json:"value"`
	Count     int                                           `json:"count"`
}

// SecurityAlertMissingEvidence names a row-level reconciliation gap without
// embedding raw provider payloads or private source details.
type SecurityAlertMissingEvidence struct {
	Kind       string `json:"kind"`
	Reason     string `json:"reason"`
	EvidenceID string `json:"evidence_id,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// SecurityAlertReconciliationAnchorRequiredMessage is the anchorless-read
// rejection text both the handlers and the staying store report, so a scoped
// caller gets one message whichever layer rejects first. It lives in the hub
// with the handlers; the staying store
// (security_alert_reconciliation.go) reads it through root's forward.
const SecurityAlertReconciliationAnchorRequiredMessage = "repository_id, provider, package_id, cve_id, or ghsa_id is required; provider_state and reconciliation_status are filters only"
