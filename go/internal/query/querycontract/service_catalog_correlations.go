// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "context"

// ServiceCatalogCorrelationMaxLimit bounds service-catalog correlation
// reads: a single repository can have more than one catalog
// provider/entity correlated to it, and resolvers must see every row, not
// just the first page. It lives here (hoisted from root package query for
// #6060 lane A) so the codeowners family can bound its
// manifest-precedence lookup without importing root, which it cannot do
// without an import cycle through root's compatibility aliases.
const ServiceCatalogCorrelationMaxLimit = 200

// ServiceCatalogCorrelationStore reads reducer-owned service catalog correlations.
//
// It lives here (promoted from root package query for #6060 lane A) because
// two query families need it without importing root: the staying
// service-catalog handler (plus catalog enrichment, freshness, and
// incident-context reads) and the moved codeowners handler's
// manifest-precedence resolver. Root keeps type aliases so every staying
// caller compiles unchanged; the Postgres implementation stays in root.
// See AGENTS.md: a shared wire or port type belongs here exactly when
// multiple families need it, and a copied type instead of an alias would
// break source identity across the storage and handler adapters.
type ServiceCatalogCorrelationStore interface {
	ListServiceCatalogCorrelations(context.Context, ServiceCatalogCorrelationFilter) ([]ServiceCatalogCorrelationRow, error)
}

// ServiceCatalogCorrelationFilter bounds catalog reads to a concrete catalog
// entity, repository, service, workload, owner, or ingestion scope.
type ServiceCatalogCorrelationFilter struct {
	ScopeID              string
	Provider             string
	EntityRef            string
	RepositoryID         string
	ServiceID            string
	WorkloadID           string
	OwnerRef             string
	Outcome              string
	DriftStatus          string
	AfterCorrelationID   string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
	// OutsideGrant inverts the grant clause: the read returns the rows the
	// caller's grant does NOT admit, rather than the rows it does. It answers
	// "does anything outside my grant also claim this selector", which is what
	// a caller needs before it may act on a shared identifier whose downstream
	// tables carry no scope column of their own. The two grant arrays are
	// required in this mode -- see the staying store's
	// errServiceCatalogOutsideGrantNeedsAGrant.
	OutsideGrant bool
	Limit        int
}

// HasScope reports whether the filter carries any read anchor: a concrete
// catalog entity, repository, service, workload, owner, or ingestion scope,
// or a grant allow-list (so the catalog enrichment path can issue a
// scope-bounded lookup without a required single-id field).
//
// It is exported because the staying Postgres implementation and the
// staying service-catalog handler call it across the package boundary
// (supplychain precedent: the hub filter HasScope methods).
func (f ServiceCatalogCorrelationFilter) HasScope() bool {
	return f.ScopeID != "" ||
		f.EntityRef != "" ||
		f.RepositoryID != "" ||
		f.ServiceID != "" ||
		f.WorkloadID != "" ||
		f.OwnerRef != "" ||
		len(f.AllowedRepositoryIDs) > 0
}

// ServiceCatalogCorrelationRow is one durable service-catalog correlation fact.
type ServiceCatalogCorrelationRow struct {
	CorrelationID          string
	Provider               string
	EntityRef              string
	EntityType             string
	DisplayName            string
	RepositoryID           string
	ServiceID              string
	WorkloadID             string
	OwnerRef               string
	Lifecycle              string
	Tier                   string
	Outcome                string
	Reason                 string
	ProvenanceOnly         bool
	DriftKind              string
	DriftStatus            string
	CandidateRepositoryIDs []string
	EvidenceFactIDs        []string
	RequiredAnchorKeys     []string
}
