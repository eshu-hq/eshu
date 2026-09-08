// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RecordingServiceCatalogCorrelationStore is a canned
// querycontract.ServiceCatalogCorrelationStore double that records the last
// filter it was called with. It moved here for #6060 lane B B4 because the
// service-catalog handler tests moved to the service family package while
// the selector-routes and catalog-authz tests stay in root; a _test.go
// declaration in either package is unreachable from the other.
type RecordingServiceCatalogCorrelationStore struct {
	Rows                   []querycontract.ServiceCatalogCorrelationRow
	DescriptorRows         []querycontract.ServiceCatalogLocalDescriptorEvidenceRow
	LastFilter             querycontract.ServiceCatalogCorrelationFilter
	LastDescriptorRepoID   string
	LastDescriptorRowLimit int
}

// ListServiceCatalogCorrelations records filter and returns the canned rows.
func (s *RecordingServiceCatalogCorrelationStore) ListServiceCatalogCorrelations(
	_ context.Context,
	filter querycontract.ServiceCatalogCorrelationFilter,
) ([]querycontract.ServiceCatalogCorrelationRow, error) {
	s.LastFilter = filter
	return append([]querycontract.ServiceCatalogCorrelationRow(nil), s.Rows...), nil
}

// ListServiceCatalogLocalDescriptorEvidence records its arguments and
// returns the canned descriptor rows, capped at limit like the SQL read.
func (s *RecordingServiceCatalogCorrelationStore) ListServiceCatalogLocalDescriptorEvidence(
	_ context.Context,
	repositoryID string,
	limit int,
) ([]querycontract.ServiceCatalogLocalDescriptorEvidenceRow, error) {
	s.LastDescriptorRepoID = repositoryID
	s.LastDescriptorRowLimit = limit
	if limit > 0 && limit < len(s.DescriptorRows) {
		return append([]querycontract.ServiceCatalogLocalDescriptorEvidenceRow(nil), s.DescriptorRows[:limit]...), nil
	}
	return append([]querycontract.ServiceCatalogLocalDescriptorEvidenceRow(nil), s.DescriptorRows...), nil
}
