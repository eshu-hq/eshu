// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// PortContentStore's read-model methods live here, split from
// portcontentstore.go to keep both files under the repo's 500-line cap.
//
// None of these are part of querycontract.ContentStore. Package query reaches
// each through a narrow optional port it type-asserts the store against, so a
// double that stops satisfying one of these signatures does not fail to
// compile -- the assertion just goes false and the handler quietly takes its
// fallback path. Keep the signatures matching their port declarations in
// package query.

// RepositoryReadModelSummary returns the fixture summary.
func (f PortContentStore) RepositoryReadModelSummary(
	context.Context,
	string,
) (querycontract.RepositoryReadModelSummary, error) {
	return f.Summary, nil
}

// RepositoryRelationshipReadModel returns the fixture relationship read model.
func (f PortContentStore) RepositoryRelationshipReadModel(
	context.Context,
	string,
) (querycontract.RepositoryRelationshipReadModel, error) {
	return f.RelationshipReadModel, nil
}

// RepositoryEntryPoints returns the fixture entry points.
func (f PortContentStore) RepositoryEntryPoints(
	context.Context,
	string,
) (querycontract.RepositoryEntryPointReadModel, error) {
	return f.EntryPoints, nil
}

// RepositoryDeploymentEvidence returns the fixture deployment evidence, or the
// installed error with a zero read model so a caller cannot mistake a failed
// read for an empty one.
func (f PortContentStore) RepositoryDeploymentEvidence(
	context.Context,
	string,
) (querycontract.RepositoryDeploymentEvidenceReadModel, error) {
	if f.DeploymentEvidenceErr != nil {
		return querycontract.RepositoryDeploymentEvidenceReadModel{}, f.DeploymentEvidenceErr
	}
	return f.DeploymentEvidence, nil
}

// RelationshipEvidenceByResolvedID returns the fixture relationship evidence.
func (f PortContentStore) RelationshipEvidenceByResolvedID(
	context.Context,
	string,
) (querycontract.RelationshipEvidenceReadModel, error) {
	return f.RelationshipEvidence, nil
}

// DocumentationFindings captures the filter it was called with when a capture
// slot is installed, then answers from the fixture.
func (f PortContentStore) DocumentationFindings(
	_ context.Context,
	filter querycontract.DocumentationFindingFilter,
) (querycontract.DocumentationFindingListReadModel, error) {
	if f.DocumentationFindingsFilter != nil {
		*f.DocumentationFindingsFilter = filter
	}
	if f.DocumentationFindingsErr != nil {
		return querycontract.DocumentationFindingListReadModel{}, f.DocumentationFindingsErr
	}
	return f.DocumentationFindingsModel, nil
}

// DocumentationFacts captures the filter it was called with when a capture
// slot is installed, then answers from the fixture.
func (f PortContentStore) DocumentationFacts(
	_ context.Context,
	filter querycontract.DocumentationFactFilter,
) (querycontract.DocumentationFactListReadModel, error) {
	if f.DocumentationFactsFilter != nil {
		*f.DocumentationFactsFilter = filter
	}
	if f.DocumentationFactsErr != nil {
		return querycontract.DocumentationFactListReadModel{}, f.DocumentationFactsErr
	}
	return f.DocumentationFactsModel, nil
}

// DocumentationEvidencePacket answers from the fixture. It takes no filter, so
// it captures nothing.
func (f PortContentStore) DocumentationEvidencePacket(
	context.Context,
	string,
) (querycontract.DocumentationEvidencePacketReadModel, error) {
	if f.DocumentationPacketErr != nil {
		return querycontract.DocumentationEvidencePacketReadModel{}, f.DocumentationPacketErr
	}
	return f.DocumentationPacketModel, nil
}

// DocumentationEvidencePacketWithFilter captures the authorization filter when
// a capture slot is installed, then answers from the same fixture the
// unfiltered read uses.
func (f PortContentStore) DocumentationEvidencePacketWithFilter(
	_ context.Context,
	filter querycontract.DocumentationEvidencePacketFilter,
) (querycontract.DocumentationEvidencePacketReadModel, error) {
	if f.DocumentationPacketFilter != nil {
		*f.DocumentationPacketFilter = filter
	}
	if f.DocumentationPacketErr != nil {
		return querycontract.DocumentationEvidencePacketReadModel{}, f.DocumentationPacketErr
	}
	return f.DocumentationPacketModel, nil
}

// DocumentationEvidencePacketFreshness answers from the fixture.
func (f PortContentStore) DocumentationEvidencePacketFreshness(
	context.Context,
	string,
	string,
) (querycontract.DocumentationEvidencePacketFreshnessReadModel, error) {
	if f.DocumentationFreshnessErr != nil {
		return querycontract.DocumentationEvidencePacketFreshnessReadModel{}, f.DocumentationFreshnessErr
	}
	return f.DocumentationFreshnessModel, nil
}

// DocumentationEvidencePacketFreshnessWithFilter captures the authorization
// filter when a capture slot is installed, then answers from the same fixture
// the unfiltered read uses.
func (f PortContentStore) DocumentationEvidencePacketFreshnessWithFilter(
	_ context.Context,
	filter querycontract.DocumentationEvidencePacketFreshnessFilter,
) (querycontract.DocumentationEvidencePacketFreshnessReadModel, error) {
	if f.DocumentationFreshnessFilter != nil {
		*f.DocumentationFreshnessFilter = filter
	}
	if f.DocumentationFreshnessErr != nil {
		return querycontract.DocumentationEvidencePacketFreshnessReadModel{}, f.DocumentationFreshnessErr
	}
	return f.DocumentationFreshnessModel, nil
}

// ServiceStoryTargetSupportEvidence returns the fixture support block and
// error exactly as installed, so a test can cover a partial answer that
// carries both.
func (f PortContentStore) ServiceStoryTargetSupportEvidence(
	context.Context,
	querycontract.ServiceStoryTargetSupportFilter,
) (querycontract.ServiceStoryTargetSupportReadModel, error) {
	return f.TargetSupportModel, f.TargetSupportErr
}
